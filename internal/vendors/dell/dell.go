package dell

import (
	"context"

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
	dstCfg    *config.S3Bucket
	dstFs     rcloneFs.Fs
	tmpFs     rcloneFs.Fs
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
}

// NewDUP returns a new DUP firmware syncer object
func NewDUP(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfg *config.Configuration, logger *logrus.Logger) (vendors.Vendor, error) {
	// init inventory
	i, err := inventory.New(ctx, cfg.ServerserviceOptions, cfg.ArtifactsURL, logger)
	if err != nil {
		return nil, err
	}

	// init rclone filesystems for tmp and dst files
	vendors.SetRcloneLogging(logger)

	dstFs, err := vendors.InitS3Fs(ctx, cfg.FirmwareRepository, "/")
	if err != nil {
		return nil, err
	}

	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	return &DUP{
		dstCfg:    cfg.FirmwareRepository,
		dstFs:     dstFs,
		tmpFs:     tmpFs,
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

func (d *DUP) Sync(ctx context.Context) error {
	for _, fw := range d.firmwares {
		dstPath := vendors.DstPath(fw)

		d.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": dstPath,
			},
		).Info("sync DUP")

		// In case the file already exists in dst, don't verify/copy it
		if exists, _ := rcloneFs.FileExists(ctx, d.dstFs, dstPath); exists {
			d.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		// init src rclone filesystem
		srcFs, err := d.initSrcFs(ctx, fw)
		if err != nil {
			return err
		}

		// Verify file checksum
		err = vendors.VerifyFile(ctx, d.tmpFs, srcFs, fw)
		if err != nil {
			return err
		}

		// Copy file to dst
		err = d.copyFile(ctx, fw)
		if err != nil {
			return err
		}

		err = d.inventory.Publish(ctx, fw)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *DUP) initSrcFs(ctx context.Context, fw *serverservice.ComponentFirmwareVersion) (srcFs rcloneFs.Fs, err error) {
	// init source to download files
	srcFs, err = vendors.InitHTTPFs(ctx, fw.UpstreamURL)
	if err != nil {
		return nil, err
	}

	return srcFs, err
}

func (d *DUP) copyFile(ctx context.Context, fw *serverservice.ComponentFirmwareVersion) error {
	var err error

	_, err = rcloneOperations.CopyURL(ctx, d.dstFs, vendors.DstPath(fw), fw.UpstreamURL, false, false, false)
	if err != nil {
		if errors.Is(err, rcloneFs.ErrorObjectNotFound) {
			return errors.Wrap(vendors.ErrCopy, err.Error()+" :"+fw.UpstreamURL)
		}

		return errors.Wrap(vendors.ErrCopy, err.Error())
	}

	return nil
}
