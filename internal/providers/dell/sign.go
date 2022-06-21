package dell

import (
	"context"
	"path"

	"github.com/equinixmetal/firmware-syncer/internal/providers"
)

// signDUP downloads the DUP file from the filestore, checksums, signs and uploads the checksum and signature files
func (d *DellDUP) signDUPFile(ctx context.Context) error {
	for _, fw := range d.firmwares {
		downloader, err := initDownloaderDUP(ctx, fw.UpstreamURL, d.filestoreCfg)
		if err != nil {
			return err
		}

		// collect metrics on return
		defer d.metrics.FromDownloader(downloader, d.config.Vendor, providers.ActionSign)

		srcPath := path.Join(
			"/firmware",
			UpdateFilesPath(
				d.config.Vendor,
				fw.Model,
				fw.ComponentSlug,
				fw.Filename,
			),
		)

		err = providers.SignFilestoreFile(
			ctx,
			srcPath,
			fw.FileCheckSum,
			d.filestoreCfg.TmpDir,
			downloader,
			d.signer,
			d.logger,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

//// signDSURepo downloads repodata file from the filestore, checksums, signs and uploads the checksum and signature files
//func (d *Dell) signDSURepo(ctx context.Context) error {
//	// init downloaders to fetch dsu repo primary.xml.gz, checksum, signature files
//	downloaders, err := initDownloadersDSU(ctx, d.repoFiles, d.syncCtx)
//	if err != nil {
//		return err
//	}
//
//	for _, downloader := range downloaders {
//		err := providers.SignFilestoreFile(
//			ctx,
//			"/repodata/primary.xml.gz",
//			"",
//			d.syncCtx.FilestoreCfg.TmpDir,
//			downloader,
//			d.signer,
//			d.logger,
//		)
//
//		if err != nil {
//			return errors.Wrap(ErrDownloadSign, err.Error())
//		}
//
//		// collect metrics
//		// nolint:gocritic // defer by intent
//		defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSign)
//	}
//
//	return nil
//}
//
