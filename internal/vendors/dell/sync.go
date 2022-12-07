package dell

import (
	"context"

	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/sirupsen/logrus"
)

// Sync implements the Syncer interface to fetch, checksum and sign firmware
func (d *DUP) Sync(ctx context.Context) error {
	// sync files
	err := d.syncDUPFiles(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (d *DUP) syncDUPFiles(ctx context.Context) error {
	for _, fw := range d.firmwares {
		// dst path for DUP files - /firmware/dell/<model>/<component>/foo.bin
		downloader, err := vendors.NewDownloader(ctx, d.vendor.Name, fw.UpstreamURL, d.dstCfg, d.logger)
		if err != nil {
			return err
		}

		dstPath := downloader.DstPath(fw)
		dstURL := "s3://" + downloader.DstBucket() + dstPath

		d.logger.WithFields(
			logrus.Fields{
				"src": downloader.SrcURL(),
				"dst": dstURL,
			},
		).Info("sync DUP")

		err = downloader.CopyFile(ctx, fw)
		// collect metrics from downloader
		d.metrics.FromDownloader(downloader, d.vendor.Name, vendors.ActionSync)

		if err != nil {
			return err
		}

		err = d.inventory.Publish(d.vendor.Name, fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}