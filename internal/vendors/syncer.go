package vendors

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"

	"github.com/metal-toolbox/firmware-syncer/internal/inventory"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

type Syncer struct {
	dstFs      fs.Fs
	tmpFs      fs.Fs
	downloader Downloader
	firmwares  []*serverservice.ComponentFirmwareVersion
	logger     *logrus.Logger
	inventory  inventory.ServerService
}

// NewSyncer creates a new Syncer.
func NewSyncer(
	dstFs fs.Fs,
	tmpFs fs.Fs,
	downloader Downloader,
	inventoryClient inventory.ServerService,
	firmwares []*serverservice.ComponentFirmwareVersion,
	logger *logrus.Logger,
) Vendor {
	SetRcloneLogging(logger)

	return &Syncer{
		dstFs:      dstFs,
		tmpFs:      tmpFs,
		downloader: downloader,
		inventory:  inventoryClient,
		firmwares:  firmwares,
		logger:     logger,
	}
}

// Sync will synchronize the firmwares with the destination file system and inventory.
// Files that do not exist on the destination will be downloaded from their source and uploaded to the destination.
// Information about the firmware file will be updated using the inventory client.
func (s *Syncer) Sync(ctx context.Context) (err error) {
	for _, firmware := range s.firmwares {
		if err = s.syncFirmware(ctx, firmware); err != nil {
			// Log error without returning, to sync other firmwares
			s.logger.WithError(err).
				WithField("firmware", firmware.Filename).
				WithField("vendor", firmware.Vendor).
				WithField("version", firmware.Version).
				WithField("url", firmware.UpstreamURL).
				Error("Failed to sync firmware")
		}
	}

	return nil
}

// syncFirmware does the synchronization for the given firmware.
func (s *Syncer) syncFirmware(ctx context.Context, firmware *serverservice.ComponentFirmwareVersion) error {
	destPath := DstPath(firmware)

	logMsg := s.logger.WithField("firmware", firmware.Filename).
		WithField("vendor", firmware.Vendor).
		WithField("version", firmware.Version).
		WithField("url", firmware.UpstreamURL)

	logMsg.Info("Syncing Firmware")

	fileExists, err := fs.FileExists(ctx, s.dstFs, destPath)
	if err != nil {
		return errors.Wrap(err, "failure checking if firmware file exists")
	}

	if !fileExists {
		downloadDir, err := os.MkdirTemp(s.tmpFs.Root(), "firmware-download")
		if err != nil {
			return errors.Wrap(err, "failure creating download directory")
		}

		defer func() {
			if err = os.RemoveAll(downloadDir); err != nil {
				logMsg.WithError(err).Error("Failure to clean up download directory")
			}
		}()

		firmwareFilePath, err := s.downloader.Download(ctx, downloadDir, firmware)
		if err != nil {
			logMsg.WithError(err).Error("Failed to download firmware")
			return nil // Only logging the error, so we don't fail the whole process
		}

		if err = validateChecksum(firmwareFilePath, firmware.Checksum); err != nil {
			logMsg.WithError(err).Error("Checksum validation failure")
			return nil // Only logging the error, so we don't fail the whole process
		}

		if err = s.uploadFile(ctx, firmwareFilePath, destPath); err != nil {
			msg := fmt.Sprintf("failure to upload firmware %s", firmware.Filename)
			return errors.Wrap(err, msg)
		}
	}

	return s.inventory.Publish(ctx, firmware)
}

func (s *Syncer) uploadFile(ctx context.Context, firmwarePath, destPath string) error {
	// Remove root of tmpdir from filename since CopyFile doesn't use it
	firmwareRelativePath := strings.Replace(firmwarePath, s.tmpFs.Root(), "", 1)

	return operations.CopyFile(ctx, s.dstFs, s.tmpFs, destPath, firmwareRelativePath)
}

func validateChecksum(file, checksum string) error {
	if !ValidateChecksum(file, checksum) {
		msg := fmt.Sprintf("Checksum validation failed: %s, expected checksum: %s", file, checksum)
		return errors.Wrap(ErrChecksumValidate, msg)
	}

	return nil
}
