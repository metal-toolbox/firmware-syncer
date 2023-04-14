package intel

import (
	"context"
	"os"
	"strings"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

type Intel struct {
	syncer    *config.Syncer
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	dstCfg    *config.S3Bucket
	dstFs     fs.Fs
	tmpFs     fs.Fs
}

func New(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfgSyncer *config.Syncer, logger *logrus.Logger) (vendors.Vendor, error) {
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
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger)
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

	return &Intel{
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

func (i *Intel) Stats() *vendors.Metrics {
	return i.metrics
}

// Sync copies firmware files from Intel and publishes to ServerService
// Initially only supports network card firmware for a given NIC family (e.g. E810, X710)
// Each NIC family may have multiple firmware binaries that applies to specific models within the family.
// The NVM update utility is also provided in the tarball downloaded and extracted from the zip archive.
func (i *Intel) Sync(ctx context.Context) error {
	for _, fw := range i.firmwares {
		// In case the file already exists in dst, don't copy it
		if exists, _ := fs.FileExists(ctx, i.dstFs, vendors.DstPath(fw)); exists {
			i.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		// initialize a tmpDir so we can download and unpack the zip archive
		tmpDir, err := os.MkdirTemp(i.tmpFs.Root(), "firmware-archive")
		if err != nil {
			return err
		}

		i.logger.Debug("Downloading archive")

		archivePath, err := vendors.DownloadFirmwareArchive(ctx, tmpDir, fw.UpstreamURL, "")
		if err != nil {
			return err
		}

		i.logger.WithFields(
			logrus.Fields{
				"archivePath": archivePath,
			},
		).Debug("Archive downloaded.")

		i.logger.Debug("Extracting firmware from archive")

		fwFile, err := vendors.ExtractFromZipArchive(archivePath, fw.Filename, fw.Checksum)
		if err != nil {
			return err
		}

		i.logger.WithFields(
			logrus.Fields{
				"fwFile": fwFile.Name(),
			},
		).Debug("Firmware extracted.")

		i.logger.WithFields(
			logrus.Fields{
				"src": fwFile.Name(),
				"dst": vendors.DstPath(fw),
			},
		).Info("Sync Intel")

		// Remove root of tmpdir from filename since CopyFile doesn't use it
		tmpFwPath := strings.Replace(fwFile.Name(), i.tmpFs.Root(), "", 1)

		err = operations.CopyFile(ctx, i.dstFs, i.tmpFs, vendors.DstPath(fw), tmpFwPath)
		if err != nil {
			return err
		}

		// Clean up tmpDir after copying the extracted firmware to dst.
		os.RemoveAll(tmpDir)

		err = i.inventory.Publish(ctx, fw, vendors.DstPath(fw))
		if err != nil {
			return err
		}
	}

	return nil
}
