package inventory

import (
	"context"
	"os"

	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/pkg/errors"
	"github.com/sanity-io/litter"
	"github.com/sirupsen/logrus"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

var ErrServerServiceMultipleFirmware = errors.New("multiple component firmware version found")

type ServerService struct {
	client *serverservice.Client
	logger *logrus.Logger
}

func New(inventoryURL string, logger *logrus.Logger) (*ServerService, error) {
	retryableClient := retryablehttp.NewClient()

	authToken := os.Getenv("SERVERSERVICE_AUTH_TOKEN")

	c, err := serverservice.NewClientWithToken(authToken, inventoryURL, retryableClient.StandardClient())
	if err != nil {
		return nil, err
	}

	return &ServerService{
		client: c,
		logger: logger,
	}, nil
}

// Publish adds firmware data to Hollow's ServerService
func (s *ServerService) Publish(vendor string, firmware *config.Firmware, dstURL string) error {
	cfv := serverservice.ComponentFirmwareVersion{
		Vendor:        vendor,
		Model:         firmware.Model,
		Filename:      firmware.Filename,
		Version:       firmware.Version,
		Checksum:      firmware.FileCheckSum,
		UpstreamURL:   firmware.UpstreamURL,
		RepositoryURL: dstURL,
		Component:     firmware.ComponentSlug,
	}

	ctx := context.TODO()

	params := serverservice.ComponentFirmwareVersionListParams{
		Vendor:  vendor,
		Model:   firmware.Model,
		Version: firmware.Version,
	}

	firmwares, _, err := s.client.ListServerComponentFirmware(ctx, &params)
	if err != nil {
		return err
	}

	if len(firmwares) == 0 {
		var u *uuid.UUID

		u, _, err = s.client.CreateServerComponentFirmware(ctx, cfv)
		if err != nil {
			return err
		}

		s.logger.WithFields(
			logrus.Fields{
				"uuid": u,
			},
		).Trace("published firmware")

		return nil
	}

	if len(firmwares) == 1 {
		s.logger.WithFields(
			logrus.Fields{
				"uuid": &firmwares[0].UUID,
			},
		).Trace("firmware already published")

		return nil
	}

	return errors.Wrap(ErrServerServiceMultipleFirmware, litter.Sdump(firmwares))
}
