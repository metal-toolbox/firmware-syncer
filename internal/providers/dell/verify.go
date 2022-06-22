package dell

import (
	"context"

	"github.com/equinixmetal/firmware-syncer/internal/providers"

	"github.com/sirupsen/logrus"
)

// Verify validates the files are in sync, checksummed and accessible from the RepositoryURL endpoint
// returns nil if verify was successful
func (d *DellDUP) Verify(ctx context.Context) error {
	for _, fw := range d.firmwares {
		downloader, err := initDownloaderDUP(ctx, fw.UpstreamURL, d.filestoreCfg)
		if err != nil {
			return err
		}

		d.logger.WithFields(logrus.Fields{"file": downloader.DstURL()}).Trace("verifying Dell DUP file")

		err = providers.VerifyUpdateURL(
			ctx,
			d.config.RepositoryURL,
			fw.Filename,
			fw.FileCheckSum,
			d.filestoreCfg.TmpDir,
			downloader,
			d.signer,
			d.logger,
		)
		// collect metrics from downloader
		d.metrics.FromDownloader(downloader, d.config.Vendor, providers.ActionVerify)

		if err != nil {
			return err
		}
	}

	return nil
}

//// verifyDSURepo validates UpdateURL by downloading the repodata files and validating the file signature and checksums
////
//// returns nil if verify was successful
//func (d *Dell) verifyDSURepo(ctx context.Context) error {
//	if d.syncCtx.UpdateCfg.UpdateURL == "" {
//		return errors.Wrap(ErrNotInSync, "UpdateConfig.UpdateURL not set, assuming files not in sync")
//	}
//
//	for _, repoFilepath := range d.repoFiles {
//		releasePath, err := dstUpdateDir(repoFilepath, d.syncCtx)
//		if err != nil {
//			return err
//		}
//
//		// https://foo.baz/firmware/dell/DSU_21.10.00/os_dependent/RHEL8_64/repodata/primary.xml.gz.SHA256.sig
//		repoFileURL := d.syncCtx.UpdateStoreURL + releasePath + "/repodata/primary.xml.gz"
//
//		storeCfg, err := providers.FilestoreConfig("/", d.syncCtx.FilestoreCfg)
//		if err != nil {
//			return errors.Wrap(ErrDownloadVerify, err.Error())
//		}
//
//		downloader, err := providers.NewDownloader(ctx, d.syncCtx.UpdateCfg.UpdateURL, storeCfg)
//		if err != nil {
//			return errors.Wrap(ErrDownloadVerify, err.Error())
//		}
//
//		err = providers.VerifyUpdateURL(
//			ctx,
//			repoFileURL,
//			path.Base(repoFileURL), // primary.xml.gz
//			"",
//			d.syncCtx.FilestoreCfg.TmpDir,
//			downloader,
//			d.signer,
//			d.logger,
//		)
//
//		d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionVerify)
//
//		if err != nil {
//			return errors.Wrap(ErrDownloadVerify, err.Error())
//		}
//	}
//
//	return nil
//}
//
