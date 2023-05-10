package config

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

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
						Checksum:    fw.MD5Sum,
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
