package dell

import (
	"context"

	"github.com/equinixmetal/firmware-syncer/internal/providers"

	"github.com/sirupsen/logrus"
)

// Verify validates the files are in sync, checksummed and accessible from the RepositoryURL endpoint
// returns nil if verify was successful
func (d *DUP) Verify(ctx context.Context) error {
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
