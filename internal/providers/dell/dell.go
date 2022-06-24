package dell

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/providers"
	ironlibm "github.com/metal-toolbox/ironlib/model"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilDellDUP = "dup" // Dell Update Package
)

// This provider syncs updates from the Dell firmware repository
//
// Dell provider can be of two types DSU or DUP.
//
// DSU repositories contain a metadata file - the primary.xml.gz file, which includes the SHA checksums for each RPM package,
// this file is checksum'd and signed as primary.xml.gz.sha256, primary.xml.gz.sha256.sig
//
// DUP files are retrieved into the filestore, checksummed and signed by themselves.
//
//
// Its often the case that the upstream is unavailable or has incorrect repodata
// and hence the sync is based on the checksum and sig files being present,
// instead of attempting to compare all the files each time.
//
//
// Updates files end up in the configured filestore under a directory structure determined by the
// hardware vendor, model, component slug and update filename (if any)

// DUP implements the Provider interface methods to retrieve dell DUP firmware files
type DUP struct {
	force        bool
	config       *config.Provider
	filestoreCfg *config.Filestore
	firmwares    []*config.Firmware
	signer       *providers.Signer
	logger       *logrus.Logger
	metrics      *providers.Metrics
}

// NewDUP returns a new DUP firmware syncer object
func NewDUP(ctx context.Context, cfgProvider *config.Provider, logger *logrus.Logger) (providers.Provider, error) {
	// RepositoryURL required
	if cfgProvider.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	var firmwares []*config.Firmware

	for _, fw := range cfgProvider.Firmwares {
		// UpstreamURL required
		if fw.UpstreamURL == "" {
			return nil, errors.Wrap(config.ErrProviderAttributes, "UpstreamURL not defined for: "+fw.Filename)
		}

		// Dell DUP files should be passed in with the file sha256 checksum
		if fw.Utility == UpdateUtilDellDUP && fw.FileCheckSum == "" {
			return nil, errors.Wrap(config.ErrNoFileChecksum, fw.UpstreamURL)
		}

		if fw.Utility == UpdateUtilDellDUP {
			firmwares = append(firmwares, fw)
		}
	}
	// parse S3 endpoint and bucket from cfgProvider.RepositoryURL
	s3Endpoint, s3Bucket, err := config.ParseRepositoryURL(cfgProvider.RepositoryURL)
	if err != nil {
		return nil, err
	}
	// initialize config.Filestore for the provider
	filestoreCfg := config.Filestore{
		Kind:     "s3",
		LocalDir: "",
		S3: &config.S3Bucket{
			Endpoint:  s3Endpoint,
			Bucket:    s3Bucket,
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
		},
		PublicKeyFile:  os.Getenv("SYNCER_PUBLIC_KEY_FILE"),
		PrivateKeyFile: os.Getenv("SYNCER_PRIVATE_KEY_FILE"),
		TmpDir:         "/tmp",
	}

	// init signer to sign and verify
	s, err := providers.NewSigner(filestoreCfg.PrivateKeyFile, filestoreCfg.PublicKeyFile)
	if err != nil {
		return nil, err
	}

	return &DUP{
		// TODO: fix force parameter to be configurable
		force:        true,
		config:       cfgProvider,
		filestoreCfg: &filestoreCfg,
		firmwares:    firmwares,
		signer:       s,
		logger:       logger,
		metrics:      providers.NewMetrics(),
	}, nil
}

// Stats implements the Syncer interface to return metrics collected on Object, byte transfer stats
func (d *DUP) Stats() *providers.Metrics {
	return d.metrics
}

// initDownloaderDUP initializes the dell DUP file downloader
// downloaders provides methods to Copy and Sync update file(s) from the source to the destination
//
// Dell updates installed as DUPs (updateConfig.Utility)
// these have a single downloader to retrieve the file from the UpstreamURL
func initDownloaderDUP(ctx context.Context, srcURL string, filestoreCfg *config.Filestore) (*providers.Downloader, error) {
	// Split out host and url part so the downloader can be invoked to copy with the source filename
	hostPart, urlPath, err := providers.SplitURLPath(srcURL)
	if err != nil {
		return nil, err
	}

	// upstream URL parent
	urlPathDir := filepath.Dir(urlPath)
	// srcURLPart includes the scheme + host + url path of the srcURL (excluding the base name)
	srcURLPart := hostPart + urlPathDir

	storeCfg, err := providers.FilestoreConfig("/", filestoreCfg)
	if err != nil {
		return nil, err
	}

	return providers.NewDownloader(ctx, srcURLPart, storeCfg)
}

// UpdateFilesPath returns the directory, file path destination for the update
// based on the device vendor, model, component slug attributes
//
// This filepath structure is used to store and retrieve firmware
func UpdateFilesPath(deviceVendor, deviceModel, slug, filename string) string {
	var p string
	// Update configuration for dells where a filename isn't specified indicates the updates are an entire repository
	if deviceVendor == ironlibm.VendorDell && filename == "" {
		p = "/" + deviceVendor + "/"
		return p
	}

	p = "/" + strings.Join([]string{
		deviceVendor,
		deviceModel,
		slug,
		filename,
	}, "/")

	return strings.Replace(p, "//", "/", -1)
}
