package supermicro

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmc-toolbox/common"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilSupermicro = "sum"
)

type Supermicro struct {
	syncer    *config.Syncer
	vendor    string
	firmwares []config.Firmware
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	dstCfg    *config.S3Bucket
}

func New(ctx context.Context, firmwares []config.Firmware, cfgSyncer *config.Syncer, logger *logrus.Logger) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	var smFirmwares []config.Firmware

	for _, fw := range firmwares {
		// UpstreamURL required
		if fw.UpstreamURL == "" {
			return nil, errors.Wrap(config.ErrProviderAttributes, "UpstreamURL not defined for: "+fw.Filename)
		}

		if fw.Utility == UpdateUtilSupermicro {
			smFirmwares = append(smFirmwares, fw)
		}
	}

	// parse S3 endpoint and bucket from cfgProvider.RepositoryURL
	s3DstEndpoint, s3DstBucket, err := config.ParseRepositoryURL(cfgSyncer.RepositoryURL)
	if err != nil {
		return nil, err
	}

	dstS3Config := &config.S3Bucket{
		Region:    cfgSyncer.RepositoryRegion,
		Endpoint:  s3DstEndpoint,
		Bucket:    s3DstBucket,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger)
	if err != nil {
		return nil, err
	}

	return &Supermicro{
		syncer:    cfgSyncer,
		vendor:    common.VendorSupermicro,
		firmwares: smFirmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
		dstCfg:    dstS3Config,
	}, nil
}

func (s *Supermicro) Stats() *vendors.Metrics {
	return s.metrics
}

func (s *Supermicro) Sync(ctx context.Context) error {
	for _, fw := range s.firmwares {
		fwID := strings.Split(fw.UpstreamURL, "=")[1]

		archiveURL, archiveChecksum, err := getArchiveURLAndChecksum(ctx, fwID)

		s.logger.WithFields(
			logrus.Fields{
				"archiveURL":      archiveURL,
				"archiveChecksum": archiveChecksum,
			},
		).Debug("found archive")

		if err != nil {
			s.logger.WithFields(
				logrus.Fields{
					"fwID": fwID,
				},
			).Debug("failed to get archiveURL and archiveChecksum")

			return err
		}

		downloader, err := vendors.NewDownloader(ctx, s.vendor, archiveURL, s.dstCfg, s.logger)
		if err != nil {
			return err
		}

		// In case the file already exists in dst, don't copy it
		if exists, _ := fs.FileExists(ctx, downloader.Dst(), downloader.DstPath(&fw)); exists {
			s.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		// initialize a tmpDir so we can download and unpack the zip archive
		tmpDir, err := os.MkdirTemp(downloader.Tmp().Root(), "firmware-archive")
		if err != nil {
			return err
		}

		s.logger.Debug("Downloading archive")

		archivePath, err := downloadFirmwareArchive(tmpDir, archiveURL, archiveChecksum)
		if err != nil {
			return err
		}

		s.logger.WithFields(
			logrus.Fields{
				"archivePath": archivePath,
			},
		).Debug("Archive downloaded.")

		s.logger.Debug("Extracting firmware from archive")

		fwFile, err := extractFirmware(archivePath, fw.Filename, fw.Checksum)
		if err != nil {
			return err
		}

		s.logger.WithFields(
			logrus.Fields{
				"fwFile": fwFile.Name(),
			},
		).Debug("Firmware extracted.")

		s.logger.WithFields(
			logrus.Fields{
				"src": fwFile.Name(),
				"dst": downloader.DstPath(&fw),
			},
		).Info("Sync Supermicro")

		// Remove root of tmpdir from filename since CopyFile doesn't use it
		tmpFwPath := strings.Replace(fwFile.Name(), downloader.Tmp().Root(), "", 1)

		err = operations.CopyFile(ctx, downloader.Dst(), downloader.Tmp(), downloader.DstPath(&fw), tmpFwPath)
		if err != nil {
			return err
		}

		// Clean up tmpDir after copying the extracted firmware to dst.
		os.RemoveAll(tmpDir)

		err = s.inventory.Publish(s.vendor, &fw, downloader.DstPath(&fw))
		if err != nil {
			return err
		}
	}

	return nil
}

func downloadFirmwareArchive(tmpDir, archiveURL, archiveChecksum string) (string, error) {
	zipArchivePath := path.Join(tmpDir, filepath.Base(archiveURL))

	out, err := os.Create(zipArchivePath)
	if err != nil {
		return "", err
	}

	err = operations.CopyURLToWriter(context.Background(), archiveURL, out)
	if err != nil {
		return "", err
	}

	if !vendors.ValidateMD5Checksum(zipArchivePath, archiveChecksum) {
		return "", errors.Wrap(vendors.ErrChecksumValidate, fmt.Sprintf("zipArchivePath: %s, expected checksum: %s", zipArchivePath, archiveChecksum))
	}

	return zipArchivePath, nil
}

// extractFirmware extracts the given firmareFilename from archivePath and checks if MD5 checksum matches.
// nolint:gocyclo // see Test_extractFirmware for examples of zip archives found in the wild.
func extractFirmware(archivePath, firmwareFilename, firmwareChecksum string) (*os.File, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var foundFile *zip.File

	fwFilenameNoExt := strings.Replace(firmwareFilename, filepath.Ext(firmwareFilename), "", 1)
	for _, f := range r.File {
		if filepath.Ext(f.Name) == ".zip" && strings.Contains(f.Name, fwFilenameNoExt) {
			foundFile = f
			// Skip checksum verification on the nested zip archive,
			// since we don't have a checksum for it.
			firmwareChecksum = ""

			break
		}

		if strings.HasSuffix(f.Name, firmwareFilename) {
			foundFile = f
			break
		}
	}

	if foundFile == nil {
		return nil, errors.New(fmt.Sprintf("couldn't find file: %s in archive: %s", firmwareFilename, archivePath))
	}

	zipContents, err := foundFile.Open()
	if err != nil {
		return nil, err
	}
	defer zipContents.Close()

	tmpDir := path.Dir(archivePath)
	tmpFilename := filepath.Base(foundFile.Name)

	out, err := os.Create(path.Join(tmpDir, tmpFilename))
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(out, zipContents)
	if err != nil {
		return nil, err
	}

	if filepath.Ext(out.Name()) == ".zip" {
		out, err = extractFirmware(out.Name(), firmwareFilename, firmwareChecksum)
		if err != nil {
			return nil, err
		}
	}

	if firmwareChecksum != "" && !vendors.ValidateMD5Checksum(out.Name(), firmwareChecksum) {
		return nil, errors.Wrap(vendors.ErrChecksumValidate, fmt.Sprintf("firmware: %s, expected checksum: %s", out.Name(), firmwareChecksum))
	}

	return out, nil
}

func getArchiveURLAndChecksum(ctx context.Context, id string) (url, checksum string, err error) {
	var httpClient = &http.Client{
		Timeout: time.Second * 15,
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		fmt.Sprintf("https://www.supermicro.com/Bios/softfiles/%s/checksum.txt", id),
		http.NoBody,
	)
	if err != nil {
		return "", "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	filename, checksum, err := parseFilenameAndChecksum(resp.Body)
	if err != nil {
		return "", "", err
	}

	archiveURL := fmt.Sprintf("https://www.supermicro.com/Bios/softfiles/%s/%s", id, filename)

	return archiveURL, checksum, nil
}

func parseFilenameAndChecksum(checksumFile io.Reader) (filename, checksum string, err error) {
	scanner := bufio.NewScanner(checksumFile)
	checksum = ""
	filename = ""

	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprintf("parsing failed: %s", r))
		}
	}()

	for i := 0; scanner.Scan() && i < 4; i++ {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "/softfiles"):
			if strings.Contains(line, "MD5") {
				filename = strings.Split(strings.Split(line, "/")[3], " ")[0]
				checksum = strings.TrimSpace(strings.Split(line, "=")[1])

				break
			} else {
				continue
			}
		case strings.HasPrefix(line, "softfiles"):
			filename = strings.Split(line, "/")[2]
		case strings.HasPrefix(line, "MD5 CheckSum:"):
			checksum = strings.TrimSpace(strings.Split(line, ":")[1])
		default:
			continue
		}

		if err := scanner.Err(); err != nil {
			return "", "", err
		}
	}

	return filename, checksum, nil
}
