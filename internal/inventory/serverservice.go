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
	artifactsURL string
	client       *serverservice.Client
	logger       *logrus.Logger
}

func New(ctx context.Context, serverServiceURL, artifactsURL string, logger *logrus.Logger) (*ServerService, error) {
	if artifactsURL == "" {
		return nil, errors.New("missing artifacts URL")
	}

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

	scopes := []string{
		"create:server-component-firmwares",
		"read:server-component-firmwares",
		"update:server-component-firmwares",
	}

	oauthConfig := clientcredentials.Config{
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		TokenURL:       provider.Endpoint().TokenURL,
		Scopes:         scopes,
		EndpointParams: url.Values{"audience": {audience}},
	}

	c, err := serverservice.NewClient(serverServiceURL, oauthConfig.Client(ctx))
	if err != nil {
		return nil, err
	}

	return &ServerService{
		artifactsURL: artifactsURL,
		client:       c,
		logger:       logger,
	}, nil
}

// getArtifactsURL returns the https artifactsURL for the given s3 dstURL
func (s *ServerService) getArtifactsURL(dstURL string) (string, error) {
	aURL, err := url.Parse(s.artifactsURL)
	if err != nil {
		return "", nil
	}

	dURL, err := url.Parse(dstURL)
	if err != nil {
		return "", nil
	}

	aURL.Path = dURL.Path

	return aURL.String(), nil
}

// Publish adds firmware data to Hollow's ServerService
func (s *ServerService) Publish(vendor string, firmware *config.Firmware, dstURL string) error {
	artifactsURL, err := s.getArtifactsURL(dstURL)
	if err != nil {
		return err
	}

	cfv := serverservice.ComponentFirmwareVersion{
		Vendor:        vendor,
		Model:         firmware.Model,
		Filename:      firmware.Filename,
		Version:       firmware.Version,
		Checksum:      firmware.Checksum,
		UpstreamURL:   firmware.UpstreamURL,
		RepositoryURL: artifactsURL,
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
		).Info("published firmware")

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
		).Info("firmware already published")

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
	).Info("duplicate firmware IDs")

	return errors.Wrap(ErrServerServiceDuplicateFirmware, strings.Join(uuids, ","))
}
