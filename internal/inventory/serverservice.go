package inventory

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/coreos/go-oidc"
	"github.com/google/uuid"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/clientcredentials"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

var (
	ErrServerServiceDuplicateFirmware = errors.New("duplicate firmware found")
	ErrServerServiceQuery             = errors.New("server service query failed")
)

type ServerService struct {
	client *serverservice.Client
	logger *logrus.Logger
}

func New(ctx context.Context, inventoryURL string, logger *logrus.Logger) (*ServerService, error) {
	clientSecret := os.Getenv("SERVERSERVICE_CLIENT_SECRET")

	if clientSecret == "" {
		return nil, errors.New("missing server service client secret")
	}

	clientID := os.Getenv("SERVERSERVICE_CLIENT_ID")

	if clientID == "" {
		return nil, errors.New("missing server service client id")
	}

	oidcProviderEndpoint := os.Getenv("SERVERSERVICE_OIDC_PROVIDER_ENDPOINT")

	if oidcProviderEndpoint == "" {
		return nil, errors.New("missing server service oidc provider endpoint")
	}

	provider, err := oidc.NewProvider(ctx, oidcProviderEndpoint)
	if err != nil {
		return nil, err
	}

	audience := os.Getenv("SERVERSERVICE_AUDIENCE_ENDPOINT")

	if audience == "" {
		return nil, errors.New("missing server service audience URL")
	}

	oauthConfig := clientcredentials.Config{
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		TokenURL:       provider.Endpoint().TokenURL,
		Scopes:         []string{"create:server", "read:server", "update:server"},
		EndpointParams: url.Values{"audience": {audience}},
	}

	c, err := serverservice.NewClient(inventoryURL, oauthConfig.Client(ctx))
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
		return errors.Wrap(ErrServerServiceQuery, "ListServerComponentFirmware: "+err.Error())
	}

	if len(firmwares) == 0 {
		var u *uuid.UUID

		u, _, err = s.client.CreateServerComponentFirmware(ctx, cfv)
		if err != nil {
			return errors.Wrap(ErrServerServiceQuery, "CreateServerComponentFirmware: "+err.Error())
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
				"uuid":    &firmwares[0].UUID,
				"vendor":  vendor,
				"model":   firmware.Model,
				"version": firmware.Version,
			},
		).Trace("firmware already published")

		return nil
	}

	// Assumption at this point is that there are duplicated firmwares returned by the ListServerComponentFirmware query.
	uuids := make([]string, len(firmwares))
	for i := range firmwares {
		uuids[i] = firmwares[i].UUID.String()
	}

	s.logger.WithFields(
		logrus.Fields{
			"uuids": strings.Join(uuids, ","),
		},
	).Trace("duplicate firmware IDs")

	return errors.Wrap(ErrServerServiceDuplicateFirmware, strings.Join(uuids, ","))
}
