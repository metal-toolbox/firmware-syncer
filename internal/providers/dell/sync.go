package dell

import (
	"context"
	"path"

	"github.com/metal-toolbox/firmware-syncer/internal/providers"
	"github.com/sirupsen/logrus"
)

// Sync implements the Syncer interface to fetch, checksum and sign firmware
func (d *DUP) Sync(ctx context.Context) error {
	if !d.force {
		// verify files are in sync before proceeding
		// this is done here because we don't want to sync broken repository metadata
		// which is often the case with the upstream Dell repositories
		err := d.Verify(ctx)
		if err == nil {
			d.logger.WithFields(
				logrus.Fields{
					"vendor": d.config.Vendor,
				},
			).Debug("file(s) in sync")

			return nil
		}

		d.logger.WithFields(
			logrus.Fields{
				"err":    err,
				"vendor": d.config.Vendor,
			},
		).Debug("proceeding to sync files..")
	}

	// sync files
	err := d.syncDUPFiles(ctx)
	if err != nil {
		return err
	}

	// sign files
	err = d.signDUPFile(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (d *DUP) syncDUPFiles(ctx context.Context) error {
	for _, fw := range d.firmwares {
		// dst path for DUP files - /firmware/dell/<model>/<component>/foo.bin
		downloader, err := initDownloaderDUP(ctx, fw.UpstreamURL, d.filestoreCfg)
		if err != nil {
			return err
		}

		downloadPath := path.Join(
			"/firmware",
			providers.UpdateFilesPath(
				d.config.Vendor,
				fw.Model,
				fw.ComponentSlug,
				fw.Filename,
			),
		)
		dstURL := downloader.FilestoreURL() + downloadPath

		d.logger.WithFields(
			logrus.Fields{
				"src": downloader.SrcURL(),
				"dst": dstURL,
			},
		).Trace("sync DUP")

		err = downloader.CopyToFilestore(ctx, downloadPath, fw.Filename)
		// collect metrics from downloader
		d.metrics.FromDownloader(downloader, d.config.Vendor, providers.ActionSync)

		if err != nil {
			return err
		}

		err = d.inventory.Publish(d.config.Vendor, fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}
