package dell

import (
	"context"
	"path"

	"github.com/packethost/fup/internal/syncer/providers"
	"github.com/packethost/fup/internal/utils"
	"github.com/packethost/fup/pkg/model"
	"github.com/pkg/errors"
)

func (d *Dell) sign(ctx context.Context) error {
	switch d.syncCtx.UpdateCfg.Utility {
	case model.UpdateUtilDellDSU:
		return d.signDSURepo(ctx)
	case model.UpdateUtilDellDUP:
		return d.signDUPFile(ctx)
	default:
		return errors.Wrap(ErrUpdateUtil, d.syncCtx.UpdateCfg.Utility)
	}
}

// signDUP downloads the DUP file from the filestore, checksums, signs and uploads the checksum and signature files
func (d *Dell) signDUPFile(ctx context.Context) error {
	downloader, err := initDownloaderDUP(ctx, d.syncCtx.UpdateCfg.UpstreamURL, d.syncCtx.FilestoreCfg)
	if err != nil {
		return err
	}

	// collect metrics on return
	defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSign)

	srcPath := path.Join(
		d.syncCtx.UpdateDirPrefix,
		utils.UpdateFilesPath(
			d.syncCtx.HWVendor,
			d.syncCtx.HWModel,
			d.syncCtx.ComponentSlug,
			d.syncCtx.UpdateCfg.Filename,
		),
	)

	return providers.SignFilestoreFile(
		ctx,
		srcPath,
		d.syncCtx.UpdateCfg.FileSHA256,
		d.syncCtx.FilestoreCfg.TmpDir,
		downloader,
		d.signer,
		d.logger,
	)
}

// signDSURepo downloads repodata file from the filestore, checksums, signs and uploads the checksum and signature files
func (d *Dell) signDSURepo(ctx context.Context) error {
	// init downloaders to fetch dsu repo primary.xml.gz, checksum, signature files
	downloaders, err := initDownloadersDSU(ctx, d.repoFiles, d.syncCtx)
	if err != nil {
		return err
	}

	for _, downloader := range downloaders {
		err := providers.SignFilestoreFile(
			ctx,
			"/repodata/primary.xml.gz",
			"",
			d.syncCtx.FilestoreCfg.TmpDir,
			downloader,
			d.signer,
			d.logger,
		)

		if err != nil {
			return errors.Wrap(ErrDownloadSign, err.Error())
		}

		// collect metrics
		// nolint:gocritic // defer by intent
		defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSign)
	}

	return nil
}
