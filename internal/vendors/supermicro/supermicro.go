package supermicro

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/inventory"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/pkg/errors"
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
		downloader, err := vendors.NewDownloader(ctx, s.vendor.Name, fw.UpstreamURL, s.dstCfg, s.logger)
		if err != nil {
			return err
		}

		dstPath := downloader.DstPath(fw)

		dstURL := "s3://" + downloader.DstBucket() + dstPath

		s.logger.WithFields(
			logrus.Fields{
				"src": fw.UpstreamURL,
				"dst": dstURL,
			},
		).Info("sync Supermicro")

		err = downloader.CopyFile(ctx, fw)
		// collect metrics from downloader
		// s.metrics.FromDownloader(downloader, s.config.Vendor, providers.ActionSync)

		if err != nil {
			return err
		}

		err = s.inventory.Publish(s.vendor.Name, fw, dstURL)
		if err != nil {
			return err
		}
	}

	return nil
}

func getChecksumFilename(id string) (checksum, filename string, err error) {
	var httpClient = &http.Client{
		Timeout: time.Second * 15,
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", fmt.Sprintf("https://www.supermicro.com/Bios/softfiles/%s/checksum.txt", id), http.NoBody)
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
