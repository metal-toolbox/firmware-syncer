package asrockrack

import (
	"context"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/spf13/viper"
	"github.com/pkg/errors"
	rcloneFs "github.com/rclone/rclone/fs"
	rcloneOperations "github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

// ASRockRack implements the Vendor interface methods to retrieve ASRockRack firmware files
type ASRockRack struct {
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	srcCfg    *config.S3Bucket
	dstCfg    *config.S3Bucket
	srcFs     rcloneFs.Fs
	dstFs     rcloneFs.Fs
	tmpFs     rcloneFs.Fs
}

func New(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfgSyncer *config.Syncer, logger *logrus.Logger, v* viper.Viper) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	srcS3Config := &config.S3Bucket{
		Region:    config.LoadEnvironmentVariable(v, logger, "asrr.s3.region"),
		Endpoint:  config.LoadEnvironmentVariable(v, logger, "asrr.s3.endpoint"),
		Bucket:    config.LoadEnvironmentVariable(v, logger, "asrr.s3.bucket"),
		AccessKey: config.LoadEnvironmentVariable(v, logger, "asrr.s3.access_key"),
		SecretKey: config.LoadEnvironmentVariable(v, logger, "asrr.s3.secret_key"),
	}

	// parse S3 endpoint and bucket from cfgSyncer.RepositoryURL
	s3DstEndpoint, s3DstBucket, err := config.ParseRepositoryURL(cfgSyncer.RepositoryURL)
	if err != nil {
		return nil, err
	}

	dstS3Config := &config.S3Bucket{
		Region:    cfgSyncer.RepositoryRegion,
		Endpoint:  s3DstEndpoint,
		Bucket:    s3DstBucket,
		AccessKey: config.LoadEnvironmentVariable(v, logger, "s3.access_key"),
		SecretKey: config.LoadEnvironmentVariable(v, logger, "s3.secret_key"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger, v)
	if err != nil {
		return nil, err
	}

	// init rclone filesystems for tmp, dst and src files
	vendors.SetRcloneLogging(logger)

	dstFs, err := vendors.InitS3Fs(ctx, dstS3Config, "/")
	if err != nil {
		return nil, err
	}

	srcFs, err := vendors.InitS3Fs(ctx, srcS3Config, "/")
	if err != nil {
		return nil, err
	}

	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	return &ASRockRack{
		firmwares: firmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
		srcCfg:    srcS3Config,
		dstCfg:    dstS3Config,
		srcFs:     srcFs,
		dstFs:     dstFs,
		tmpFs:     tmpFs,
	}, nil
}

func (a *ASRockRack) Stats() *vendors.Metrics {
	return a.metrics
}

func (a *ASRockRack) Sync(ctx context.Context) error {
	for _, fw := range a.firmwares {
		dstPath := vendors.DstPath(fw)

		dstURL := "s3://" + a.dstCfg.Bucket + "/" + dstPath

		a.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": dstURL,
			},
		).Info("sync ASRockRack")

		// In case the file already exists in dst, don't verify/copy it
		if exists, _ := rcloneFs.FileExists(ctx, a.dstFs, vendors.DstPath(fw)); exists {
			a.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		// verify file checksum
		err := vendors.VerifyFile(ctx, a.tmpFs, a.srcFs, fw)
		if err != nil {
			return err
		}

		// copy file to dst
		err = a.copyFile(ctx, fw)
		if err != nil {
			return err
		}

		err = a.inventory.Publish(ctx, fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}

func (a *ASRockRack) copyFile(ctx context.Context, fw *serverservice.ComponentFirmwareVersion) error {
	err := rcloneOperations.CopyFile(ctx, a.dstFs, a.srcFs, vendors.DstPath(fw), vendors.SrcPath(fw))
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(vendors.ErrCopy, err.Error()+" :"+fw.Filename)
		}

		return errors.Wrap(vendors.ErrCopy, err.Error())
	}

	return nil
}
