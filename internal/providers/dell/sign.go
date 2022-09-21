package dell

import (
	"context"
	"path"

	"github.com/metal-toolbox/firmware-syncer/internal/providers"
)

// signDUP downloads the DUP file from the filestore, checksums, signs and uploads the checksum and signature files
func (d *DUP) signDUPFile(ctx context.Context) error {
	for _, fw := range d.firmwares {
		downloader, err := initDownloaderDUP(ctx, fw.UpstreamURL, d.filestoreCfg)
		if err != nil {
			return err
		}

		srcPath := path.Join(
			"/firmware",
			providers.UpdateFilesPath(
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
		// collect metrics from downloader
		d.metrics.FromDownloader(downloader, d.config.Vendor, providers.ActionSign)

		if err != nil {
			return err
		}
	}

	return nil
}
