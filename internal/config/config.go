package config

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"

	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"

	"github.com/metal-toolbox/firmware-syncer/pkg/types"
)

var (
	ErrConfig               = errors.New("configuration error")
	ErrProviderAttributes   = errors.New("provider config missing required attribute(s)")
	ErrNoFileChecksum       = errors.New("file upstreamURL declared with no checksum (Provider.UtilityChecksum)")
	ErrProviderNotSupported = errors.New("provider not suppported")
)

// Config holds application configuration read from a YAML or set by env variables.
type Configuration struct {
	// LogLevel is the app verbose logging level.
	// one of - info, debug, trace
	LogLevel string `mapstructure:"log_level"`

	InventoryKind types.InventoryKind `mapstructure:"inventory_kind"`

	// ServerserviceOptions defines the serverservice client configuration parameters
	//
	// This parameter is required when StoreKind is set to serverservice.
	ServerserviceOptions *ServerserviceOptions `mapstructure:"serverservice"`

	// FirmwareRepository defines configuration for the s3 bucket firmware will be synced to
	FirmwareRepository *S3Bucket `mapstructure:"s3bucket"`

	// AsRockRackRepository defines configuration for the asrockrack s3 source firmware bucket
	AsRockRackRepository *S3Bucket `mapstructure:"s3bucket"`

	// ArtifactsURL defines the artifacts URL used by all firmware
	ArtifactsURL string `mapstructure:"artifacts_url"`

	// FirmwareManifestURL defines the URL for modeldata.json
	FirmwareManifestURL string `mapstructure:"firmware_manifest_url"`

	// GithubOpenBmcToken defines the token used to access internal openbmc repository
	GithubOpenBmcToken string `mapstructure:"github_openbmc_token"`

	// DefaultDownloadURL defines where unsupported firmware will be downloaded from
	DefaultDownloadURL string `mapstructure:"default_download_url"`
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
	InstallInband   bool   `json:"install_inband"`
	Oem             bool   `json:"oem"`
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

func LoadFirmwareManifest(ctx context.Context, manifestURL string) (map[string][]*fleetdbapi.ComponentFirmwareVersion, error) {
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

	firmwaresByVendor := make(map[string][]*fleetdbapi.ComponentFirmwareVersion)

	for _, m := range models {
		for component, firmwareRecords := range m.Components {
			for _, fw := range firmwareRecords {
				cModels := []string{strings.ToLower(m.Model)}
				if fw.Model != "" {
					cModels = append(cModels, strings.ToLower(fw.Model))
				}

				tmpInstallInband := fw.InstallInband
				tmpOEM := fw.Oem
				firmwaresByVendor[m.Manufacturer] = append(firmwaresByVendor[m.Manufacturer],
					&fleetdbapi.ComponentFirmwareVersion{
						Vendor:      strings.ToLower(m.Manufacturer),
						Version:     fw.FirmwareVersion,
						Model:       cModels,
						Component:   strings.ToLower(component),
						UpstreamURL: fw.VendorURI,
						Filename:    fw.Filename,
						// publish checksum with hash hint
						Checksum:      "md5sum:" + fw.MD5Sum,
						InstallInband: &tmpInstallInband,
						OEM:           &tmpOEM,
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
