package dell

import (
	"context"
	"path"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Sync implements the Syncer interface to fetch, checksum and sign firmware
func (d *Dell) Sync(ctx context.Context) error {
	if !d.syncCtx.Force {
		// verify files are in sync before proceeding
		// this is done here because we don't want to sync broken repository metadata
		// which is often the case with the upstream Dell repositories
		err := d.Verify(ctx)
		if err == nil {
			d.logger.WithFields(
				logrus.Fields{
					"vendor": d.syncCtx.HWVendor,
					"src":    d.syncCtx.UpdateCfg.UpstreamURL,
				},
			).Debug("file(s) in sync")

			return nil
		}

		d.logger.WithFields(
			logrus.Fields{
				"err":    err,
				"vendor": d.syncCtx.HWVendor,
				"src":    d.syncCtx.UpdateCfg.UpstreamURL,
			},
		).Debug("proceeding to sync files..")
	}

	// sync files
	err := d.sync(ctx)
	if err != nil {
		return err
	}

	// sign files
	err = d.sign(ctx)
	if err != nil {
		return err
	}

	return nil
}

// sync downloads updates
func (d *Dell) sync(ctx context.Context) error {
	switch d.updateUtility {
	case UpdateUtilDellDSU:
		return d.syncDSURepo(ctx)
	case UpdateUtilDellDUP:
		return d.syncDUPFile(ctx)
	default:
		return errors.Wrap(ErrUpdateUtil, d.updateUtility)
	}
}

// syncDSU repo syncs the DSU repositories
func (d *Dell) syncDSURepo(ctx context.Context) error {
	// init downloaders to fetch dsu repo primary.xml.gz, checksum, signature files
	downloaders, err := initDownloadersDSU(ctx, d.config)
	if err != nil {
		return err
	}

	for _, downloader := range downloaders {
		d.logger.WithFields(
			logrus.Fields{
				"src": downloader.SrcURL(),
				"dst": downloader.FilestoreURL() + downloader.FilestoreRootDir(),
			},
		).Trace("sync DSU repo")

		err = downloader.Sync(ctx)
		if err != nil {
			return err
		}

		// TODO: Fix metrics collection
		// collect metrics
		// nolint:gocritic // defer by intent
		//defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSync)
	}

	return nil
}

func (d *Dell) syncDUPFile(ctx context.Context) error {
	// dst path for DUP files - /firmware/dell/<model>/<component>/foo.bin
	downloader, err := initDownloaderDUP(ctx, d.config.UpstreamURL, d.config.FilestoreCfg)
	if err != nil {
		return err
	}

	// TODO: fix metrics collection
	// collect metrics on return
	//defer d.syncCtx.Metrics.FromDownloader(downloader, d.syncCtx.HWVendor, providers.ActionSync)

	downloadPath := path.Join(
		d.syncCtx.UpdateDirPrefix,
		UpdateFilesPath(
			d.syncCtx.HWVendor,
			d.syncCtx.HWModel,
			d.syncCtx.ComponentSlug,
			d.syncCtx.UpdateCfg.Filename,
		),
	)

	d.logger.WithFields(
		logrus.Fields{
			"src": downloader.SrcURL() + d.syncCtx.UpdateCfg.Filename,
			"dst": downloader.FilestoreURL() + downloadPath,
		},
	).Trace("sync DUP")

	err = downloader.CopyToFilestore(ctx, downloadPath, d.syncCtx.UpdateCfg.Filename)
	if err != nil {
		return err
	}

	return nil
}
