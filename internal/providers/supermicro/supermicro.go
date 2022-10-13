package supermicro

import (
	"context"
	"os"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/providers"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilSupermicro = "sum"
)

type Supermicro struct {
	config    *config.Provider
	firmwares []*config.Firmware
	logger    *logrus.Logger
	metrics   *providers.Metrics
	inventory *inventory.ServerService
	dstCfg    *config.S3Bucket
}

func New(ctx context.Context, cfgProvider *config.Provider, inventoryURL string, logger *logrus.Logger) (providers.Provider, error) {
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

		if fw.Utility == UpdateUtilSupermicro {
			firmwares = append(firmwares, fw)
		}
	}

	// parse S3 endpoint and bucket from cfgProvider.RepositoryURL
	s3DstEndpoint, s3DstBucket, err := config.ParseRepositoryURL(cfgProvider.RepositoryURL)
	if err != nil {
		return nil, err
	}

	dstS3Config := &config.S3Bucket{
		Region:    cfgProvider.RepositoryRegion,
		Endpoint:  s3DstEndpoint,
		Bucket:    s3DstBucket,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	// init inventory
	i, err := inventory.New(ctx, inventoryURL, logger)
	if err != nil {
		return nil, err
	}

	return &Supermicro{
		config:    cfgProvider,
		firmwares: firmwares,
		logger:    logger,
		metrics:   providers.NewMetrics(),
		inventory: i,
		dstCfg:    dstS3Config,
	}, nil
}

func (s *Supermicro) Stats() *providers.Metrics {
	return s.metrics
}

func (s *Supermicro) Sync(ctx context.Context) error {
	for _, fw := range s.firmwares {
		downloader, err := providers.NewDownloader(ctx, s.config.Vendor, fw.UpstreamURL, s.dstCfg, s.logger)
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

		err = s.inventory.Publish(s.config.Vendor, fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}
