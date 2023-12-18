package app

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/bmc-toolbox/common"
	"github.com/jeremywohl/flatten"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/logging"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/github"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/supermicro"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
)

const (
	VendorEquinix = "equinix"
)

// App holds attributes for the firmware-syncer application
type App struct {
	// Viper loads configuration parameters.
	v *viper.Viper
	// firmware-syncer configuration.
	Config *config.Configuration
	// Logger is the app logger
	Logger  *logrus.Logger
	vendors []vendors.Vendor
}

// New returns a new instance of the firmware-syncer app
func New(ctx context.Context, inventoryKind types.InventoryKind, cfgFile, logLevel string) (*App, error) {
	app := &App{
		v:      viper.New(),
		Config: &config.Configuration{},
	}

	if err := app.LoadConfiguration(cfgFile, inventoryKind); err != nil {
		return nil, err
	}

	// CLI parameter takes precedence over config and env vars
	if logLevel != "" {
		app.Config.LogLevel = logLevel
	}

	app.Logger = logging.NewLogger(app.Config.LogLevel)

	// Load firmware manifest
	firmwaresByVendor, err := config.LoadFirmwareManifest(ctx, app.Config.FirmwareManifestURL)
	if err != nil {
		app.Logger.Error(err.Error())
		return nil, err
	}

	inventoryClient, err := inventory.New(ctx, app.Config.ServerserviceOptions, app.Config.ArtifactsURL, app.Logger)
	if err != nil {
		return nil, err
	}

	dstFs, err := vendors.InitS3Fs(ctx, app.Config.FirmwareRepository, "/")
	if err != nil {
		return nil, err
	}

	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: os.TempDir()})
	if err != nil {
		return nil, err
	}

	for vendor, firmwares := range firmwaresByVendor {
		var downloader vendors.Downloader

		switch vendor {
		case common.VendorDell:
			downloader = vendors.NewRcloneDownloader(app.Logger)
		case common.VendorAsrockrack:
			s3Fs, err := vendors.InitS3Fs(ctx, app.Config.AsRockRackRepository, "/")
			if err != nil {
				return nil, err
			}
			downloader = vendors.NewS3Downloader(app.Logger, s3Fs)
		case common.VendorSupermicro:
			downloader = supermicro.NewSupermicroDownloader(app.Logger)
		case common.VendorMellanox:
			downloader = vendors.NewArchiveDownloader(app.Logger)
		case common.VendorIntel:
			downloader = vendors.NewArchiveDownloader(app.Logger)
		case VendorEquinix:
			ghClient := github.NewGitHubClient(ctx, app.Config.GithubOpenBmcToken)
			downloader = github.NewGitHubDownloader(app.Logger, ghClient)
		default:
			app.Logger.Error("Vendor not supported: " + vendor)
			continue
		}

		syncer := vendors.NewSyncer(dstFs, tmpFs, downloader, inventoryClient, firmwares, app.Logger)
		app.vendors = append(app.vendors, syncer)
	}

	return app, nil
}

// SyncFirmwares syncs all firmware files from the configured providers
func (a *App) SyncFirmwares(ctx context.Context) error {
	for _, v := range a.vendors {
		err := v.Sync(ctx)
		if err != nil {
			a.Logger.WithError(err).Error("Failed to sync vendor")
		}
	}

	return nil
}

// nolint:gocyclo // config load is cyclomatic
// LoadConfiguration loads application configuration
//
// Reads in the cfgFile when available and overrides from environment variables.
func (a *App) LoadConfiguration(cfgFile string, inventoryKind types.InventoryKind) error {
	a.v.SetConfigType("yaml")
	a.v.SetEnvPrefix(types.AppName)
	a.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	a.v.AutomaticEnv()

	// these are initialized here so viper can read in configuration from env vars
	// once https://github.com/spf13/viper/pull/1429 is merged, this can go.
	a.Config.ServerserviceOptions = &config.ServerserviceOptions{}
	a.Config.FirmwareRepository = &config.S3Bucket{}
	a.Config.AsRockRackRepository = &config.S3Bucket{}

	if cfgFile != "" {
		fh, err := os.Open(cfgFile)
		if err != nil {
			return errors.Wrap(config.ErrConfig, err.Error())
		}

		if err = a.v.ReadConfig(fh); err != nil {
			return errors.Wrap(config.ErrConfig, "ReadConfig error: "+err.Error())
		}
	}

	if err := a.envBindVars(); err != nil {
		return errors.Wrap(config.ErrConfig, "env var bind error: "+err.Error())
	}

	if err := a.v.Unmarshal(a.Config); err != nil {
		return errors.Wrap(config.ErrConfig, "Unmarshal error: "+err.Error())
	}

	err := a.envVarAppOverrides()
	if err != nil {
		return errors.Wrap(config.ErrConfig, "app env overrides error: "+err.Error())
	}

	if inventoryKind == types.InventoryStoreServerservice {
		if err := a.envVarServerserviceOverrides(); err != nil {
			return errors.Wrap(config.ErrConfig, "serverservice env overrides error: "+err.Error())
		}
	}

	return nil
}

// nolint:gocyclo // env var load is cyclomatic
func (a *App) envVarAppOverrides() error {
	if a.v.GetString("log.level") != "" {
		a.Config.LogLevel = a.v.GetString("log.level")
	}

	if a.v.GetString("s3.endpoint") != "" {
		a.Config.FirmwareRepository.Endpoint = a.v.GetString("s3.endpoint")
	}

	if a.v.GetString("s3.bucket") != "" {
		a.Config.FirmwareRepository.Bucket = a.v.GetString("s3.bucket")
	}

	if a.v.GetString("s3.region") != "" {
		a.Config.FirmwareRepository.Region = a.v.GetString("s3.region")
	}

	if a.v.GetString("s3.access.key") != "" {
		a.Config.FirmwareRepository.AccessKey = a.v.GetString("s3.access.key")
	}

	if a.v.GetString("s3.secret.key") != "" {
		a.Config.FirmwareRepository.SecretKey = a.v.GetString("s3.secret.key")
	}

	if a.v.GetString("asrr.s3.region") != "" {
		a.Config.AsRockRackRepository.Region = a.v.GetString("asrr.s3.region")
	}

	if a.v.GetString("asrr.s3.endpoint") != "" {
		a.Config.AsRockRackRepository.Endpoint = a.v.GetString("asrr.s3.endpoint")
	}

	if a.v.GetString("asrr.s3.bucket") != "" {
		a.Config.AsRockRackRepository.Bucket = a.v.GetString("asrr.s3.bucket")
	}

	if a.v.GetString("asrr.s3.access.key") != "" {
		a.Config.AsRockRackRepository.AccessKey = a.v.GetString("asrr.s3.access.key")
	}

	if a.v.GetString("asrr.s3.secret.key") != "" {
		a.Config.AsRockRackRepository.SecretKey = a.v.GetString("asrr.s3.secret.key")
	}

	if a.v.GetString("github.openbmc.token") != "" {
		a.Config.GithubOpenBmcToken = a.v.GetString("github.openbmc.token")
	}

	return nil
}

// envBindVars binds environment variables to the struct
// without a configuration file being unmarshalled,
// this is a workaround for a viper bug,
//
// This can be replaced by the solution in https://github.com/spf13/viper/pull/1429
// once that PR is merged.
func (a *App) envBindVars() error {
	envKeysMap := map[string]interface{}{}
	if err := mapstructure.Decode(a.Config, &envKeysMap); err != nil {
		return err
	}

	// Flatten nested conf map
	flat, err := flatten.Flatten(envKeysMap, "", flatten.DotStyle)
	if err != nil {
		return errors.Wrap(err, "Unable to flatten config")
	}

	for k := range flat {
		if err := a.v.BindEnv(k); err != nil {
			return errors.Wrap(config.ErrConfig, "env var bind error: "+err.Error())
		}
	}

	return nil
}

// Server service configuration options

// nolint:gocyclo // parameter validation is cyclomatic
func (a *App) envVarServerserviceOverrides() error {
	if a.Config.ServerserviceOptions == nil {
		a.Config.ServerserviceOptions = &config.ServerserviceOptions{}
	}

	if a.v.GetString("serverservice.endpoint") != "" {
		a.Config.ServerserviceOptions.Endpoint = a.v.GetString("serverservice.endpoint")
	}

	endpointURL, err := url.Parse(a.Config.ServerserviceOptions.Endpoint)
	if err != nil {
		return errors.New("serverservice endpoint URL error: " + err.Error())
	}

	a.Config.ServerserviceOptions.EndpointURL = endpointURL

	if a.v.GetString("serverservice.disable.oauth") != "" {
		a.Config.ServerserviceOptions.DisableOAuth = a.v.GetBool("serverservice.disable.oauth")
	}

	if a.Config.ServerserviceOptions.DisableOAuth {
		return nil
	}

	if a.v.GetString("serverservice.oidc.issuer.endpoint") != "" {
		a.Config.ServerserviceOptions.OidcIssuerEndpoint = a.v.GetString("serverservice.oidc.issuer.endpoint")
	}

	if a.Config.ServerserviceOptions.OidcIssuerEndpoint == "" {
		return errors.New("serverservice oidc.issuer.endpoint not defined")
	}

	if a.v.GetString("serverservice.oidc.audience.endpoint") != "" {
		a.Config.ServerserviceOptions.OidcAudienceEndpoint = a.v.GetString("serverservice.oidc.audience.endpoint")
	}

	if a.Config.ServerserviceOptions.OidcAudienceEndpoint == "" {
		return errors.New("serverservice oidc.audience.endpoint not defined")
	}

	if a.v.GetString("serverservice.oidc.client.secret") != "" {
		a.Config.ServerserviceOptions.OidcClientSecret = a.v.GetString("serverservice.oidc.client.secret")
	}

	if a.Config.ServerserviceOptions.OidcClientSecret == "" {
		return errors.New("serverservice.oidc.client.secret not defined")
	}

	if a.v.GetString("serverservice.oidc.client.id") != "" {
		a.Config.ServerserviceOptions.OidcClientID = a.v.GetString("serverservice.oidc.client.id")
	}

	if a.Config.ServerserviceOptions.OidcClientID == "" {
		return errors.New("serverservice.oidc.client.id not defined")
	}

	if a.v.GetString("serverservice.oidc.client.scopes") != "" {
		a.Config.ServerserviceOptions.OidcClientScopes = a.v.GetStringSlice("serverservice.oidc.client.scopes")
	}

	if len(a.Config.ServerserviceOptions.OidcClientScopes) == 0 {
		return errors.New("serverservice oidc.client.scopes not defined")
	}

	return nil
}
