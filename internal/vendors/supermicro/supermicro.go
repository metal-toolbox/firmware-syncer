package supermicro

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

type SupermicroDownloader struct {
	logger *logrus.Logger
}

func NewSupermicroDownloader(logger *logrus.Logger) *SupermicroDownloader {
	return &SupermicroDownloader{logger: logger}
}

func (s *SupermicroDownloader) Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error) {
	urlSplit := strings.Split(firmware.UpstreamURL, "=")

	if len(urlSplit) < 2 {
		return "", fmt.Errorf("upstream URL is missing firmwareID: %s", firmware.UpstreamURL)
	}

	firmwareID := urlSplit[1]
	archiveURL, archiveChecksum, err := getArchiveURLAndChecksum(ctx, firmwareID)

	s.logger.WithField("archiveURL", archiveURL).
		WithField("archiveChecksum", archiveChecksum).
		Debug("found archive")

	if err != nil {
		s.logger.WithField("firmwareID", firmwareID).Debug("failed to get archiveURL and archiveChecksum")
		return "", err
	}

	s.logger.Debug("Downloading archive")

	archivePath, err := vendors.DownloadFirmwareArchive(ctx, downloadDir, archiveURL, archiveChecksum)
	if err != nil {
		return "", err
	}

	s.logger.WithField("archivePath", archivePath).Debug("Archive downloaded.")
	s.logger.Debug("Extracting firmware from archive")

	fwFile, err := vendors.ExtractFromZipArchive(archivePath, firmware.Filename, "")
	if err != nil {
		return "", err
	}

	return fwFile.Name(), nil
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
