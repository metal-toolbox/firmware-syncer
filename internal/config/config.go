package config

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

var (
	ErrProviderAttributes   = errors.New("provider config missing required attribute(s)")
	ErrNoFileChecksum       = errors.New("file upstreamURL declared with no checksum (Provider.UtilityChecksum)")
	ErrProviderNotSupported = errors.New("provider not suppported")
)

type Syncer struct {
	ServerServiceURL string `yaml:"serverserviceURL"`
	RepositoryURL    string `yaml:"repositoryURL"`
	RepositoryRegion string `yaml:"repositoryRegion"`
	Providers        []*Provider
}

type Provider struct {
	Vendor    string `yaml:"vendor"`
	Firmwares []*Firmware
}

type Firmware struct {
	Version       string `yaml:"version"`
	Model         string `yaml:"model"`
	ComponentSlug string `yaml:"componentslug"`
	Utility       string `yaml:"utility"`
	UpstreamURL   string `yaml:"upstreamURL"`
	Filename      string `yaml:"filename"`
	Checksum      string `yaml:"checksum"`
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

func ParseRepositoryURL(repositoryURL string) (endpoint, bucket string, err error) {
	u, err := url.Parse(repositoryURL)
	if err != nil {
		return "", "", err
	}

	bucket = strings.Trim(u.Path, "/")

	return u.Host, bucket, nil
}
