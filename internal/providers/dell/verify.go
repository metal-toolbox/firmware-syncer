package dell

import (
	"context"
	"path"

	"github.com/equinixmetal/firmware-syncer/internal/providers"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Verify validates the files in sync, checksummed and accessible from the UpdateConfig.UpdateURL endpoint
func (d *Dell) Verify(ctx context.Context) error {
	switch d.updateUtility {
	case UpdateUtilDellDSU:
		return d.verifyDSURepo(ctx)
	case UpdateUtilDellDUP:
		return d.verifyDUPFile(ctx)
	default:
		return errors.Wrap(ErrUpdateUtil, d.updateUtility)
	}
}

// verifyDUPFile validates UpdateURL by downloading the files and validating the signature and checksums
//
// returns nil if verify was successful
func (d *Dell) verifyDUPFile(ctx context.Context) error {
	if d.syncCtx.UpdateCfg.UpdateURL == "" {
		return errors.Wrap(ErrNotInSync, "UpdateConfig.UpdateURL not set, assuming files not in sync")
	}

	downloader, err := initDownloaderDUP(ctx, d.syncCtx.UpdateCfg.UpdateURL, d.syncCtx.FilestoreCfg)
	if err != nil {
		return err
	}

	// collect metrics from downloader
	defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionVerify)

	d.logger.WithFields(logrus.Fields{"file": downloader.DstURL()}).Trace("verifying Dell DUP file")

	return providers.VerifyUpdateURL(
		ctx,
		d.syncCtx.UpdateCfg.UpdateURL,
		d.syncCtx.UpdateCfg.Filename,
		d.syncCtx.UpdateCfg.FileSHA256,
		d.syncCtx.FilestoreCfg.TmpDir,
		downloader,
		d.signer,
		d.logger,
	)
}

// verifyDSURepo validates UpdateURL by downloading the repodata files and validating the file signature and checksums
//
// returns nil if verify was successful
func (d *Dell) verifyDSURepo(ctx context.Context) error {
	if d.syncCtx.UpdateCfg.UpdateURL == "" {
		return errors.Wrap(ErrNotInSync, "UpdateConfig.UpdateURL not set, assuming files not in sync")
	}

	for _, repoFilepath := range d.repoFiles {
		releasePath, err := dstUpdateDir(repoFilepath, d.syncCtx)
		if err != nil {
			return err
		}

		// https://foo.baz/firmware/dell/DSU_21.10.00/os_dependent/RHEL8_64/repodata/primary.xml.gz.SHA256.sig
		repoFileURL := d.syncCtx.UpdateStoreURL + releasePath + "/repodata/primary.xml.gz"

		storeCfg, err := providers.FilestoreConfig("/", d.syncCtx.FilestoreCfg)
		if err != nil {
			return errors.Wrap(ErrDownloadVerify, err.Error())
		}

		downloader, err := providers.NewDownloader(ctx, d.syncCtx.UpdateCfg.UpdateURL, storeCfg)
		if err != nil {
			return errors.Wrap(ErrDownloadVerify, err.Error())
		}

		err = providers.VerifyUpdateURL(
			ctx,
			repoFileURL,
			path.Base(repoFileURL), // primary.xml.gz
			"",
			d.syncCtx.FilestoreCfg.TmpDir,
			downloader,
			d.signer,
			d.logger,
		)

		d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionVerify)

		if err != nil {
			return errors.Wrap(ErrDownloadVerify, err.Error())
		}
	}

	return nil
}
