package dell

import (
	"context"
	"os"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"

	"github.com/pkg/errors"
	rcloneFs "github.com/rclone/rclone/fs"
	rcloneOperations "github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

// DUP implements the Vendor interface methods to retrieve dell DUP firmware files
type DUP struct {
	syncer    *config.Syncer
	dstCfg    *config.S3Bucket
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
}

// NewDUP returns a new DUP firmware syncer object
func NewDUP(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfgSyncer *config.Syncer, logger *logrus.Logger) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
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
		dstCfg:    s3Cfg,
		firmwares: firmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
	}, nil
}

// Stats implements the Syncer interface to return metrics collected on Object, byte transfer stats
func (d *DUP) Stats() *vendors.Metrics {
	return d.metrics
}

func (d *DUP) initRcloneFs(ctx context.Context, fw *serverservice.ComponentFirmwareVersion, logger *logrus.Logger) (dstFs, tmpFs, srcFs rcloneFs.Fs, err error) {
	vendors.SetRcloneLogging(logger)

	dstFs, err = vendors.InitS3Fs(ctx, d.dstCfg, "/")
	if err != nil {
		return nil, nil, nil, err
	}

	tmpFs, err = vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, nil, nil, err
	}

	// init source to download files
	srcFs, err = vendors.InitHTTPFs(ctx, fw.UpstreamURL)
	if err != nil {
		return nil, nil, nil, err
	}

	return dstFs, tmpFs, srcFs, nil
}

func (d *DUP) Sync(ctx context.Context) error {
	for _, fw := range d.firmwares {
		dstPath := vendors.DstPath(fw)
		dstURL := "s3://" + d.dstCfg.Bucket + dstPath

		d.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": dstURL,
			},
		).Info("sync DUP")

		err := d.copyFile(ctx, fw)
		if err != nil {
			return err
		}

		err = d.inventory.Publish(fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *DUP) copyFile(ctx context.Context, fw *serverservice.ComponentFirmwareVersion) error {
	var err error

	dstFs, tmpFs, srcFs, err := d.initRcloneFs(ctx, fw, d.logger)
	if err != nil {
		return err
	}

	// In case the file already exists in dst, don't verify/copy it
	if exists, _ := rcloneFs.FileExists(ctx, dstFs, vendors.DstPath(fw)); exists {
		d.logger.WithFields(
			logrus.Fields{
				"filename": fw.Filename,
			},
		).Debug("firmware already exists at dst")

		return nil
	}

	err = vendors.VerifyFile(ctx, tmpFs, srcFs, fw)
	if err != nil {
		return err
	}

	_, err = rcloneOperations.CopyURL(ctx, dstFs, vendors.DstPath(fw), fw.UpstreamURL, false, false, false)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(vendors.ErrCopy, err.Error()+" :"+fw.UpstreamURL)
		}

		return errors.Wrap(vendors.ErrCopy, err.Error())
	}

	return nil
}
