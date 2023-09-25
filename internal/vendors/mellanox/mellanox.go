package mellanox

import (
	"context"
	"os"
	"strings"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/spf13/viper"

	"github.com/pkg/errors"
	rcloneFs "github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

type Mellanox struct {
	syncer    *config.Syncer
	dstCfg    *config.S3Bucket
	dstFs     rcloneFs.Fs
	tmpFs     rcloneFs.Fs
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
}

func New(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfgSyncer *config.Syncer, logger *logrus.Logger, v* viper.Viper) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
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
		AccessKey: config.LoadEnvironmentVariable(v, logger, "s3.access_key"),
		SecretKey: config.LoadEnvironmentVariable(v, logger, "s3.secret_key"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger, v)
	if err != nil {
		return nil, err
	}

	vendors.SetRcloneLogging(logger)

	dstFs, err := vendors.InitS3Fs(ctx, dstS3Config, "/")
	if err != nil {
		return nil, err
	}

	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	return &Mellanox{
		syncer:    cfgSyncer,
		firmwares: firmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
		dstCfg:    dstS3Config,
		dstFs:     dstFs,
		tmpFs:     tmpFs,
	}, nil
}

func (m *Mellanox) Stats() *vendors.Metrics {
	return m.metrics
}

func (m *Mellanox) Sync(ctx context.Context) error {
	for _, fw := range m.firmwares {
		// In case the file already exists in dst, don't copy it
		if exists, _ := rcloneFs.FileExists(ctx, m.dstFs, vendors.DstPath(fw)); exists {
			m.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		// initialize a tmpDir so we can download and unpack the zip archive
		tmpDir, err := os.MkdirTemp(m.tmpFs.Root(), "firmware-archive")
		if err != nil {
			return err
		}

		m.logger.Debug("Downloading archive")

		archivePath, err := vendors.DownloadFirmwareArchive(ctx, tmpDir, fw.UpstreamURL, "")
		if err != nil {
			return err
		}

		m.logger.WithFields(
			logrus.Fields{
				"archivePath": archivePath,
			},
		).Debug("Archive downloaded.")

		m.logger.Debug("Extracting firmware from archive")

		fwFile, err := vendors.ExtractFromZipArchive(archivePath, fw.Filename, fw.Checksum)
		if err != nil {
			return err
		}

		m.logger.WithFields(
			logrus.Fields{
				"fwFile": fwFile.Name(),
			},
		).Debug("Firmware extracted.")

		m.logger.WithFields(
			logrus.Fields{
				"src": fwFile.Name(),
				"dst": vendors.DstPath(fw),
			},
		).Info("Sync Mellanox")

		// Remove root of tmpdir from filename since CopyFile doesn't use it
		tmpFwPath := strings.Replace(fwFile.Name(), m.tmpFs.Root(), "", 1)

		err = operations.CopyFile(ctx, m.dstFs, m.tmpFs, vendors.DstPath(fw), tmpFwPath)
		if err != nil {
			return err
		}

		// Clean up tmpDir after copying the extracted firmware to dst.
		os.RemoveAll(tmpDir)

		err = m.inventory.Publish(ctx, fw, vendors.DstPath(fw))
		if err != nil {
			return err
		}
	}

	return nil
}
