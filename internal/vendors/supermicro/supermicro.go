package supermicro

import (
	"context"
	"os"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilSupermicro = "sum"
)

type Supermicro struct {
	syncer    *config.Syncer
	vendor    *config.Vendor
	firmwares []*config.Firmware
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	dstCfg    *config.S3Bucket
}

func New(ctx context.Context, cfgVendor *config.Vendor, cfgSyncer *config.Syncer, logger *logrus.Logger) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	var firmwares []*config.Firmware

	for _, fw := range cfgVendor.Firmwares {
		// UpstreamURL required
		if fw.UpstreamURL == "" {
			return nil, errors.Wrap(config.ErrProviderAttributes, "UpstreamURL not defined for: "+fw.Filename)
		}

		if fw.Utility == UpdateUtilSupermicro {
			firmwares = append(firmwares, fw)
		}
	}

	// parse S3 endpoint and bucket from cfgProvider.RepositoryURL
	s3DstEndpoint, s3DstBucket, err := config.ParseRepositoryURL(cfgSyncer.RepositoryURL)
	if err != nil {
		return nil, err
	}

	dstS3Config := &config.S3Bucket{
		Region:    cfgSyncer.RepositoryRegion,
		Endpoint:  s3DstEndpoint,
		Bucket:    s3DstBucket,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger)
	if err != nil {
		return nil, err
	}

	return &Supermicro{
		syncer:    cfgSyncer,
		vendor:    cfgVendor,
		firmwares: firmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
		dstCfg:    dstS3Config,
	}, nil
}

func (s *Supermicro) Stats() *vendors.Metrics {
	return s.metrics
}

func (s *Supermicro) Sync(ctx context.Context) error {
	for _, fw := range s.firmwares {
		downloader, err := vendors.NewDownloader(ctx, s.vendor.Name, fw.UpstreamURL, s.dstCfg, s.logger)
		if err != nil {
			return err
		}

		dstPath := downloader.DstPath(fw)

		dstURL := "s3://" + downloader.DstBucket() + dstPath

		s.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": dstURL,
			},
		).Info("sync Supermicro")

		err = downloader.CopyFile(ctx, fw)
		// collect metrics from downloader
		// s.metrics.FromDownloader(downloader, s.config.Vendor, providers.ActionSync)

		if err != nil {
			return err
		}

		err = s.inventory.Publish(s.vendor.Name, fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}
