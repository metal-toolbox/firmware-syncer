package supermicro

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs/operations"
	"github.com/sirupsen/logrus"
)

const (
	UpdateUtilSupermicro = "sum"
)

type Supermicro struct {
	syncer    *config.Syncer
	vendor    *config.Vendor
	firmwares []*config.Firmware
	logger    *logrus.Logger
	metrics   *vendors.Metrics
	inventory *inventory.ServerService
	dstCfg    *config.S3Bucket
}

func New(ctx context.Context, cfgVendor *config.Vendor, cfgSyncer *config.Syncer, logger *logrus.Logger) (vendors.Vendor, error) {
	// RepositoryURL required
	if cfgSyncer.RepositoryURL == "" {
		return nil, errors.Wrap(config.ErrProviderAttributes, "RepositoryURL not defined")
	}

	var firmwares []*config.Firmware

	for _, fw := range cfgVendor.Firmwares {
		// UpstreamURL required
		if fw.UpstreamURL == "" {
			return nil, errors.Wrap(config.ErrProviderAttributes, "UpstreamURL not defined for: "+fw.Filename)
		}

		if fw.Utility == UpdateUtilSupermicro {
			firmwares = append(firmwares, fw)
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
		vendor:    cfgVendor,
		firmwares: firmwares,
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

		archiveChecksum, archiveName, err := getChecksumAndArchiveFilename(fwID)
		if err != nil {
			return err
		}

		err = downloadFirmwareArchive(fwID, archiveName, archiveChecksum, fw.Filename, fw.Checksum)
		if err != nil {
			return err
		}
	}

	fmt.Println("finished Supermicro sync")

	return nil
}

func downloadFirmwareArchive(id, archiveFilename, archiveChecksum, firmwareFilename, firmwareChecksum string) error {
	// use rclone here to copy from https src to tmp dir dst
	// TODO:
	// 1. create tmp directory for the zip download
	// 2. create io.writer for zip file
	// 3. use rclone to download file from url to io.writer
	// 4. verify downloaded archive matches the checksum
	// create local tmp directory
	tmpDir, err := os.MkdirTemp(os.TempDir(), "verify-zip")
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmpDir)

	zipArchivePath := path.Join(tmpDir, archiveFilename)

	out, err := os.Create(zipArchivePath)
	if err != nil {
		return err
	}

	zipArchiveURL := fmt.Sprintf("https://www.supermicro.com/Bios/softfiles/%s/%s", id, archiveFilename)

	err = operations.CopyURLToWriter(context.Background(), zipArchiveURL, out)
	if err != nil {
		return err
	}

	if !vendors.ValidateMD5Checksum(zipArchivePath, archiveChecksum) {
		// wrap some checksum validation error here.
		return err
	}

	// TODO: should use the value returned here to build fwPath
	_, err = unzipFirmwareBinary(zipArchivePath, firmwareFilename, firmwareChecksum)
	if err != nil {
		return err
	}

	fwPath := path.Dir(zipArchivePath) + firmwareFilename
	// copy the firmware file from the tmp dir where it was extracted to the s3 bucket.
	// operations.CopyFile(context.Background(), dstFilename, fwPath)
	fmt.Printf("Copying %s to destination S3 bucket\n", fwPath)

	return nil
}

func unzipFirmwareBinary(zipArchivePath, firmwareFilename, firmwareChecksum string) (*os.File, error) {
	r, err := zip.OpenReader(zipArchivePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var foundFile *zip.File

	for _, f := range r.File {
		if strings.HasSuffix(f.Name, firmwareFilename) {
			foundFile = f
			break
		}
	}

	if foundFile == nil {
		return nil, errors.New(fmt.Sprintf("couldn't find file: %s in archive: %s", firmwareFilename, zipArchivePath))
	}

	zipContents, err := foundFile.Open()
	if err != nil {
		return nil, err
	}
	defer zipContents.Close()

	tmpDir := path.Dir(zipArchivePath)
	tmpFilename := filepath.Base(foundFile.Name)

	out, err := os.Create(path.Join(tmpDir, tmpFilename))
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(out, zipContents)
	if err != nil {
		return nil, err
	}

	if !vendors.ValidateMD5Checksum(out.Name(), firmwareChecksum) {
		return nil, err
	}

	return out, nil
}

func getChecksumAndArchiveFilename(id string) (checksum, filename string, err error) {
	var httpClient = &http.Client{
		Timeout: time.Second * 15,
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		fmt.Sprintf("https://www.supermicro.com/Bios/softfiles/%s/checksum.txt", id),
		http.NoBody,
	)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	return parseChecksumAndFilename(resp.Body)
}

func parseChecksumAndFilename(checksumFile io.Reader) (checksum, filename string, err error) {
	scanner := bufio.NewScanner(checksumFile)
	checksum = ""
	filename = ""

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

	return checksum, filename, nil
}
