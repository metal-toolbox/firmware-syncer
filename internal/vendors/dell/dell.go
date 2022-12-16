package dell

import (
	"context"
	"os"

	"github.com/bmc-toolbox/common"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"

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

// DUP implements the Vendor interface methods to retrieve dell DUP firmware files
type DUP struct {
	syncer    *config.Syncer
	vendor    string
	dstCfg    *config.S3Bucket
	firmwares []config.Firmware
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
}

// NewDUP returns a new DUP firmware syncer object
func NewDUP(ctx context.Context, firmwares []config.Firmware, cfgSyncer *config.Syncer, logger *logrus.Logger) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	var dupFirmwares []config.Firmware

	for _, fw := range firmwares {
		// UpstreamURL required
		if fw.UpstreamURL == "" {
			return nil, errors.Wrap(config.ErrProviderAttributes, "UpstreamURL not defined for: "+fw.Filename)
		}

		// Dell DUP files should be passed in with the file sha256 checksum
		if fw.Utility == UpdateUtilDellDUP && fw.Checksum == "" {
			return nil, errors.Wrap(config.ErrNoFileChecksum, fw.UpstreamURL)
		}

		if fw.Utility == UpdateUtilDellDUP {
			dupFirmwares = append(dupFirmwares, fw)
		}
	}
	// parse S3 endpoint and bucket from cfgProvider.RepositoryURL
	s3Endpoint, s3Bucket, err := config.ParseRepositoryURL(cfgSyncer.RepositoryURL)
	if err != nil {
		return nil, err
	}

	s3Cfg := &config.S3Bucket{
		Region:    cfgSyncer.RepositoryRegion,
		Endpoint:  s3Endpoint,
		Bucket:    s3Bucket,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger)
	if err != nil {
		return nil, err
	}

	return &DUP{
		syncer:    cfgSyncer,
		vendor:    common.VendorDell,
		dstCfg:    s3Cfg,
		firmwares: dupFirmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
	}, nil
}

// Stats implements the Syncer interface to return metrics collected on Object, byte transfer stats
func (d *DUP) Stats() *vendors.Metrics {
	return d.metrics
}
