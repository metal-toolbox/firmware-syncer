package dell

import (
	"context"
	"path"

	"github.com/sirupsen/logrus"
)

// Sync implements the Syncer interface to fetch, checksum and sign firmware
func (d *DellDUP) Sync(ctx context.Context) error {
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

//// syncDSU repo syncs the DSU repositories
//func (d *DellDSU) syncDSURepo(ctx context.Context) error {
//	// init downloaders to fetch dsu repo primary.xml.gz, checksum, signature files
//	downloaders, err := initDownloadersDSU(ctx, d.config)
//	if err != nil {
//		return err
//	}
//
//	for _, downloader := range downloaders {
//		d.logger.WithFields(
//			logrus.Fields{
//				"src": downloader.SrcURL(),
//				"dst": downloader.FilestoreURL() + downloader.FilestoreRootDir(),
//			},
//		).Trace("sync DSU repo")
//
//		err = downloader.Sync(ctx)
//		if err != nil {
//			return err
//		}
//
//		// TODO: Fix metrics collection
//		// collect metrics
//		// nolint:gocritic // defer by intent
//		//defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSync)
//	}
//
//	return nil
//}

func (d *DellDUP) syncDUPFiles(ctx context.Context) error {
	for _, fw := range d.firmwares {
		// dst path for DUP files - /firmware/dell/<model>/<component>/foo.bin
		downloader, err := initDownloaderDUP(ctx, fw.UpstreamURL, d.filestoreCfg)
		if err != nil {
			return err
		}

		// TODO: fix metrics collection
		// collect metrics on return
		//defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSync)

		downloadPath := path.Join(
			"/firmware",
			UpdateFilesPath(
				d.config.Vendor,
				fw.Model,
				fw.ComponentSlug,
				fw.Filename,
			),
		)

		d.logger.WithFields(
			logrus.Fields{
				"src": downloader.SrcURL() + fw.Filename,
				"dst": downloader.FilestoreURL() + downloadPath,
			},
		).Trace("sync DUP")

		err = downloader.CopyToFilestore(ctx, downloadPath, fw.Filename)
		if err != nil {
			return err
		}
	}

	return nil
}
