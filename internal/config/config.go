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
	Providers        []*Provider
}

type Provider struct {
	Vendor        string `yaml:"vendor"`
	RepositoryURL string `yaml:"repositoryURL"`
	Firmwares     []*Firmware
}

type Firmware struct {
	Version       string `yaml:"version"`
	Model         string `yaml:"model"`
	ComponentSlug string `yaml:"componentslug"`
	Utility       string `yaml:"utility"`
	UpstreamURL   string `yaml:"upstreamURL"`
	Filename      string `yaml:"filename"`
	FileCheckSum  string `yaml:"checksum"`
}

// Filestore declares configuration for where firmware are stored
type Filestore struct {
	// Kind is one of s3, local (defaults to s3)
	Kind string `mapstructure:"kind"`
	// Local directory path - when Kind is set to 'local'
	LocalDir string `mapstructure:"local_dir"`
	// S3 bucket configuration - when Kind is set to 's3'
	S3 *S3Bucket `mapstructure:"s3_bucket"`
	// Public key file path - the file containing the public key part used to verify firmware
	PublicKeyFile string `mapstructure:"public_key_file"`
	// Private key file path - the file containing the private key part used to sign firmware
	PrivateKeyFile string `mapstructure:"private_key_file"`
	// TmpDir is a work directory to generate checksums and signatures
	TmpDir string `mapstructure:"tmp_dir"`
}

// S3Bucket holds configuration parameters to connect to an S3 compatible bucket
type S3Bucket struct {
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

	bucket = strings.TrimLeft(u.Path, "/")

	return u.Host, bucket, nil
}
