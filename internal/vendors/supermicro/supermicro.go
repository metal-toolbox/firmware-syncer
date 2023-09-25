package supermicro

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

type Supermicro struct {
	syncer    *config.Syncer
	firmwares []*serverservice.ComponentFirmwareVersion
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	dstCfg    *config.S3Bucket
	dstFs     fs.Fs
	tmpFs     fs.Fs
}

func New(ctx context.Context, firmwares []*serverservice.ComponentFirmwareVersion, cfgSyncer *config.Syncer, logger *logrus.Logger, v* viper.Viper) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
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
		AccessKey: config.LoadEnvironmentVariable(v, logger, "s3.access_key"),
		SecretKey: config.LoadEnvironmentVariable(v, logger, "s3.secret_key"),
	}

	// init inventory
	i, err := inventory.New(ctx, cfgSyncer.ServerServiceURL, cfgSyncer.ArtifactsURL, logger, v)
	if err != nil {
		return nil, err
	}

	vendors.SetRcloneLogging(logger)

	dstFs, err := vendors.InitS3Fs(ctx, dstS3Config, "/")
	if err != nil {
		return nil, err
	}

	tmpFs, err := vendors.InitLocalFs(ctx, &vendors.LocalFsConfig{Root: "/tmp"})
	if err != nil {
		return nil, err
	}

	return &Supermicro{
		syncer:    cfgSyncer,
		firmwares: firmwares,
		logger:    logger,
		metrics:   vendors.NewMetrics(),
		inventory: i,
		dstCfg:    dstS3Config,
		dstFs:     dstFs,
		tmpFs:     tmpFs,
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

		// In case the file already exists in dst, don't copy it
		if exists, _ := fs.FileExists(ctx, s.dstFs, vendors.DstPath(fw)); exists {
			s.logger.WithFields(
				logrus.Fields{
					"filename": fw.Filename,
				},
			).Debug("firmware already exists at dst")

			continue
		}

		// initialize a tmpDir so we can download and unpack the zip archive
		tmpDir, err := os.MkdirTemp(s.tmpFs.Root(), "firmware-archive")
		if err != nil {
			return err
		}

		s.logger.Debug("Downloading archive")

		archivePath, err := vendors.DownloadFirmwareArchive(ctx, tmpDir, archiveURL, archiveChecksum)
		if err != nil {
			return err
		}

		s.logger.WithFields(
			logrus.Fields{
				"archivePath": archivePath,
			},
		).Debug("Archive downloaded.")

		s.logger.Debug("Extracting firmware from archive")

		fwFile, err := vendors.ExtractFromZipArchive(archivePath, fw.Filename, fw.Checksum)
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
				"dst": vendors.DstPath(fw),
			},
		).Info("Sync Supermicro")

		// Remove root of tmpdir from filename since CopyFile doesn't use it
		tmpFwPath := strings.Replace(fwFile.Name(), s.tmpFs.Root(), "", 1)

		err = operations.CopyFile(ctx, s.dstFs, s.tmpFs, vendors.DstPath(fw), tmpFwPath)
		if err != nil {
			return err
		}

		// Clean up tmpDir after copying the extracted firmware to dst.
		os.RemoveAll(tmpDir)

		err = s.inventory.Publish(ctx, fw, vendors.DstPath(fw))
		if err != nil {
			return err
		}
	}

	return nil
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
