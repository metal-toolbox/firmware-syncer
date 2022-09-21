package asrockrack

import (
	"context"
	"net/url"
	"os"
	"path"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/providers"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilASRockRack = "asrrmgnttool"
)

// ASRockRack implements the Provider interface methods to retrieve ASRockRack firmware files
type ASRockRack struct {
	config    *config.Provider
	firmwares []*config.Firmware
	logger    *logrus.Logger
	metrics   *providers.Metrics
	inventory *inventory.ServerService
	srcCfg    *config.S3Bucket
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

		if fw.Utility == UpdateUtilASRockRack {
			firmwares = append(firmwares, fw)
		}
	}
	// TODO: For now set this configuration from env vars but ideally this should come from
	// somewhere else. Maybe a per provider config?
	srcS3Config := &config.S3Bucket{
		Region:    os.Getenv("ASRR_S3_REGION"),
		Endpoint:  os.Getenv("ASRR_S3_ENDPOINT"),
		Bucket:    os.Getenv("ASRR_S3_BUCKET"),
		AccessKey: os.Getenv("ASRR_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("ASRR_S3_SECRET_KEY"),
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

	return &ASRockRack{
		config:    cfgProvider,
		firmwares: firmwares,
		logger:    logger,
		metrics:   providers.NewMetrics(),
		inventory: i,
		srcCfg:    srcS3Config,
		dstCfg:    dstS3Config,
	}, nil
}

func (a *ASRockRack) Stats() *providers.Metrics {
	return a.metrics
}

func initDownloader(ctx context.Context, srcCfg, dstCfg *config.S3Bucket) (*providers.S3Downloader, error) {
	return providers.NewS3Downloader(ctx, srcCfg, dstCfg)
}

func (a *ASRockRack) Sync(ctx context.Context) error {
	for _, fw := range a.firmwares {
		downloader, err := initDownloader(ctx, a.srcCfg, a.dstCfg)
		if err != nil {
			return err
		}

		u, _ := url.Parse(fw.UpstreamURL)
		srcPath := u.Path

		dstPath := path.Join(
			"/firmware",
			providers.UpdateFilesPath(
				a.config.Vendor,
				fw.Model,
				fw.ComponentSlug,
				fw.Filename,
			),
		)

		a.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": "s3://" + downloader.DstBucket() + dstPath,
			},
		).Trace("sync ASRockRack")

		err = downloader.CopyFile(ctx, dstPath, srcPath)
		// collect metrics from downloader
		//a.metrics.FromDownloader(downloader, a.config.Vendor, providers.ActionSync)

		if err != nil {
			return err
		}

		//err = a.inventory.Publish(a.config.Vendor, fw, dstURL)
		//if err != nil {
		//	return err
		//}
	}

	return nil
}

// Verify validates files are in sync, checksummed and accessible from the RepositoryURL endpoint
// returns nil if verify was successful
func (a *ASRockRack) Verify(ctx context.Context) error {
	return nil
}
