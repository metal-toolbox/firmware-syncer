package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jeremywohl/flatten"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"go.hollow.sh/toolbox/events"
	"gopkg.in/yaml.v2"
)

const (
	SyncerConcurrency = 1
)

var (
	ErrConfig = errors.New("configuration error")
)

// Config holds application configuration read from a YAML or set by env variables.
type Configuration struct {
	// LogLevel is the app verbose logging level.
	// one of - info, debug, trace
	LogLevel string `mapstructure:"log_level"`

	// AppKind is the application kind - worker / client
	AppKind types.AppKind `mapstructure:"app_kind"`

	// Syncer configuration
	Concurrency int `mapstructure:"concurrency"`

	// FacilityCode limits this syncer to events in a facility.
	FacilityCode string `mapstructure:"facility_code"`

	// The inventory source - one of serverservice OR Yaml
	InventorySource string `mapstructure:"inventory_source"`

	StoreKind types.StoreKind `mapstructure:"store_kind"`

	// ServerserviceOptions defines the serverservice client configuration parameters
	//
	// This parameter is required when StoreKind is set to serverservice.
	ServerserviceOptions *ServerserviceOptions `mapstructure:"serverservice"`

	// EventsBrokerKind indicates the kind of event broker configuration to enable,
	//
	// Supported parameter value - nats
	EventsBrokerKind string `mapstructure:"events_broker_kind"`

	// NatsOptions defines the NATs events broker configuration parameters.
	//
	// This parameter is required when EventsBrokerKind is set to nats.
	NatsOptions *events.NatsOptions `mapstructure:"nats"`
}

// ServerserviceOptions defines configuration for the Serverservice client.
// https://github.com/metal-toolbox/hollow-serverservice
type ServerserviceOptions struct {
	EndpointURL          *url.URL
	Endpoint             string   `mapstructure:"endpoint"`
	OidcIssuerEndpoint   string   `mapstructure:"oidc_issuer_endpoint"`
	OidcAudienceEndpoint string   `mapstructure:"oidc_audience_endpoint"`
	OidcClientSecret     string   `mapstructure:"oidc_client_secret"`
	OidcClientID         string   `mapstructure:"oidc_client_id"`
	OidcClientScopes     []string `mapstructure:"oidc_client_scopes"`
	DisableOAuth         bool     `mapstructure:"disable_oauth"`
	ArtifactsURL         string   `mapstructure:"artifacts_url"`
}

// LoadConfiguration loads application configuration
//
// Reads in the cfgFile when available and overrides from environment variables.
func (a *App) LoadConfiguration(cfgFile string, storeKind types.StoreKind) error {
	a.v.SetConfigType("yaml")
	a.v.SetEnvPrefix(types.AppName)
	a.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	a.v.AutomaticEnv()

	// these are initialized here so viper can read in configuration from env vars
	// once https://github.com/spf13/viper/pull/1429 is merged, this can go.
	a.Config.ServerserviceOptions = &ServerserviceOptions{}
	a.Config.NatsOptions = &events.NatsOptions{
		Stream:   &events.NatsStreamOptions{},
		Consumer: &events.NatsConsumerOptions{},
	}

	if cfgFile != "" {
		fh, err := os.Open(cfgFile)
		if err != nil {
			return errors.Wrap(ErrConfig, err.Error())
		}

		if err = a.v.ReadConfig(fh); err != nil {
			return errors.Wrap(ErrConfig, "ReadConfig error:"+err.Error())
		}
	}

	a.v.SetDefault("log.level", "info")

	if err := a.envBindVars(); err != nil {
		return errors.Wrap(ErrConfig, "env var bind error:"+err.Error())
	}

	if err := a.v.Unmarshal(a.Config); err != nil {
		return errors.Wrap(ErrConfig, "Unmarshal error: "+err.Error())
	}

	a.envVarAppOverrides()

	if a.Config.EventsBrokerKind == "nats" {
		if err := a.envVarNatsOverrides(); err != nil {
			return errors.Wrap(ErrConfig, "nats env overrides error:"+err.Error())
		}
	}

	if storeKind == types.InventoryStoreServerservice {
		if err := a.envVarServerserviceOverrides(); err != nil {
			return errors.Wrap(ErrConfig, "serverservice env overrides error:"+err.Error())
		}
	}

	return nil
}

func (a *App) envVarAppOverrides() {
	if a.v.GetString("log.level") != "" {
		a.Config.LogLevel = a.v.GetString("log.level")
	}
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
			return errors.Wrap(ErrConfig, "env var bind error: "+err.Error())
		}
	}

	return nil
}

// NATs streaming configuration
var (
	defaultNatsConnectTimeout = 100 * time.Millisecond
)

// nolint:gocyclo // nats env config load is cyclomatic
func (a *App) envVarNatsOverrides() error {
	if a.Config.NatsOptions == nil {
		a.Config.NatsOptions = &events.NatsOptions{}
	}

	if a.v.GetString("nats.url") != "" {
		a.Config.NatsOptions.URL = a.v.GetString("nats.url")
	}

	if a.Config.NatsOptions.URL == "" {
		return errors.New("missing parameter: nats.url")
	}

	if a.v.GetString("nats.publisherSubjectPrefix") != "" {
		a.Config.NatsOptions.PublisherSubjectPrefix = a.v.GetString("nats.publisherSubjectPrefix")
	}

	if a.Config.NatsOptions.PublisherSubjectPrefix == "" {
		return errors.New("missing parameter: nats.publisherSubjectPrefix")
	}

	if a.v.GetString("nats.stream.user") != "" {
		a.Config.NatsOptions.StreamUser = a.v.GetString("nats.stream.user")
	}

	if a.v.GetString("nats.stream.pass") != "" {
		a.Config.NatsOptions.StreamPass = a.v.GetString("nats.stream.pass")
	}

	if a.v.GetString("nats.creds.file") != "" {
		a.Config.NatsOptions.CredsFile = a.v.GetString("nats.creds.file")
	}

	if a.v.GetString("nats.stream.name") != "" {
		if a.Config.NatsOptions.Stream == nil {
			a.Config.NatsOptions.Stream = &events.NatsStreamOptions{}
		}

		a.Config.NatsOptions.Stream.Name = a.v.GetString("nats.stream.name")
	}

	if a.Config.NatsOptions.Stream.Name == "" {
		return errors.New("A stream name is required")
	}

	if a.v.GetString("nats.consumer.name") != "" {
		if a.Config.NatsOptions.Consumer == nil {
			a.Config.NatsOptions.Consumer = &events.NatsConsumerOptions{}
		}

		a.Config.NatsOptions.Consumer.Name = a.v.GetString("nats.consumer.name")
	}

	if len(a.v.GetStringSlice("nats.consumer.subscribeSubjects")) != 0 {
		a.Config.NatsOptions.Consumer.SubscribeSubjects = a.v.GetStringSlice("nats.consumer.subscribeSubjects")
	}

	if len(a.Config.NatsOptions.Consumer.SubscribeSubjects) == 0 {
		return errors.New("missing parameter: nats.consumer.subscribeSubjects")
	}

	if a.v.GetString("nats.consumer.filterSubject") != "" {
		a.Config.NatsOptions.Consumer.FilterSubject = a.v.GetString("nats.consumer.filterSubject")
	}

	if a.Config.NatsOptions.Consumer.FilterSubject == "" {
		return errors.New("missing parameter: nats.consumer.filterSubject")
	}

	if a.v.GetDuration("nats.connect.timeout") != 0 {
		a.Config.NatsOptions.ConnectTimeout = a.v.GetDuration("nats.connect.timeout")
	}

	if a.Config.NatsOptions.ConnectTimeout == 0 {
		a.Config.NatsOptions.ConnectTimeout = defaultNatsConnectTimeout
	}

	return nil
}

// Server service configuration options

// nolint:gocyclo // parameter validation is cyclomatic
func (a *App) envVarServerserviceOverrides() error {
	if a.Config.ServerserviceOptions == nil {
		a.Config.ServerserviceOptions = &ServerserviceOptions{}
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

var (
	ErrProviderAttributes   = errors.New("provider config missing required attribute(s)")
	ErrNoFileChecksum       = errors.New("file upstreamURL declared with no checksum (Provider.UtilityChecksum)")
	ErrProviderNotSupported = errors.New("provider not suppported")
)

type Syncer struct {
	ServerServiceURL    string `yaml:"serverserviceURL"`
	RepositoryURL       string `yaml:"repositoryURL"`
	RepositoryRegion    string `yaml:"repositoryRegion"`
	ArtifactsURL        string `yaml:"artifactsURL"`
	FirmwareManifestURL string `yaml:"firmwareManifestURL"`
}

// FirmwareRecord from modeldata.json
type FirmwareRecord struct {
	BuildDate       string `json:"build_date"`
	Filename        string `json:"filename"`
	FirmwareVersion string `json:"firmware_version"`
	Latest          bool   `json:"latest"`
	MD5Sum          string `json:"md5sum"`
	VendorURI       string `json:"vendor_uri"`
	Model           string `json:"model,omitempty"`
	// intentionally ignoring preerequisite field in modeldata.json
	// because sometimes it's a bool (false) or a string with the prerequisite
}

// Model from modeldata.json
type Model struct {
	Model        string                      `json:"model"`
	Manufacturer string                      `json:"manufacturer"`
	Components   map[string][]FirmwareRecord `json:"firmware"`
}

// S3Bucket holds configuration parameters to connect to an S3 compatible bucket
type S3Bucket struct {
	Region    string `mapstructure:"region"`   // AWS region location for the s3 bucket
	Endpoint  string `mapstructure:"endpoint"` // s3.foobar.com
	Bucket    string `mapstructure:"bucket"`   // fup-data
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
}

func LoadSyncerConfig(configFile string) (*Syncer, error) {
	b, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config *Syncer

	err = yaml.Unmarshal(b, &config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func LoadFirmwareManifest(ctx context.Context, manifestURL string) (map[string][]*serverservice.ComponentFirmwareVersion, error) {
	var httpClient = &http.Client{
		Timeout: time.Second * 15,
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		manifestURL,
		http.NoBody,
	)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var models []Model

	err = json.Unmarshal(b, &models)
	if err != nil {
		return nil, err
	}

	firmwaresByVendor := make(map[string][]*serverservice.ComponentFirmwareVersion)

	for _, m := range models {
		for component, firmwareRecords := range m.Components {
			for _, fw := range firmwareRecords {
				cModels := []string{strings.ToLower(m.Model)}
				if fw.Model != "" {
					cModels = append(cModels, strings.ToLower(fw.Model))
				}

				firmwaresByVendor[m.Manufacturer] = append(firmwaresByVendor[m.Manufacturer],
					&serverservice.ComponentFirmwareVersion{
						Vendor:      strings.ToLower(m.Manufacturer),
						Version:     fw.FirmwareVersion,
						Model:       cModels,
						Component:   strings.ToLower(component),
						UpstreamURL: fw.VendorURI,
						Filename:    fw.Filename,
						// publish checksum with hash hint
						Checksum: "md5sum:" + fw.MD5Sum,
					})
			}
		}
	}

	return firmwaresByVendor, nil
}

func ParseRepositoryURL(repositoryURL string) (endpoint, bucket string, err error) {
	u, err := url.Parse(repositoryURL)
	if err != nil {
		return "", "", err
	}

	bucket = strings.Trim(u.Path, "/")

	return u.Host, bucket, nil
}
