package inventory

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/coreos/go-oidc"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/metal-toolbox/firmware-syncer/internal/config"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

var (
	ErrServerServiceDuplicateFirmware = errors.New("duplicate firmware found")
	ErrServerServiceQuery             = errors.New("server service query failed")
)

//go:generate mockgen -source=serverservice.go -destination=mocks/serverservice.go ServerService

type ServerService interface {
	Publish(ctx context.Context, newFirmware *serverservice.ComponentFirmwareVersion) error
}

type serverService struct {
	artifactsURL string
	client       *serverservice.Client
	logger       *logrus.Logger
}

func New(ctx context.Context, cfg *config.ServerserviceOptions, artifactsURL string, logger *logrus.Logger) (ServerService, error) {
	var client *serverservice.Client

	var err error

	if !cfg.DisableOAuth {
		client, err = newClientWithOAuth(ctx, cfg)
		if err != nil {
			return nil, err
		}
	} else {
		client, err = serverservice.NewClientWithToken("fake", cfg.Endpoint, nil)
		if err != nil {
			return nil, err
		}
	}

	return &serverService{
		artifactsURL: artifactsURL,
		client:       client,
		logger:       logger,
	}, nil
}

func newClientWithOAuth(ctx context.Context, cfg *config.ServerserviceOptions) (client *serverservice.Client, err error) {
	provider, err := oidc.NewProvider(ctx, cfg.OidcIssuerEndpoint)
	if err != nil {
		return nil, err
	}

	oauthConfig := clientcredentials.Config{
		ClientID:       cfg.OidcClientID,
		ClientSecret:   cfg.OidcClientSecret,
		TokenURL:       provider.Endpoint().TokenURL,
		Scopes:         cfg.OidcClientScopes,
		EndpointParams: url.Values{"audience": {cfg.OidcAudienceEndpoint}},
	}

	client, err = serverservice.NewClient(cfg.EndpointURL.String(), oauthConfig.Client(ctx))
	if err != nil {
		return nil, err
	}

	return client, nil
}

func makeFirmwarePath(fw *serverservice.ComponentFirmwareVersion) string {
	return path.Join(fw.Vendor, fw.Filename)
}

// Publish adds firmware data to Hollow's ServerService
func (s *serverService) Publish(ctx context.Context, newFirmware *serverservice.ComponentFirmwareVersion) error {
	artifactsURL, err := url.JoinPath(s.artifactsURL, makeFirmwarePath(newFirmware))
	if err != nil {
		return err
	}

	newFirmware.RepositoryURL = artifactsURL

	params := serverservice.ComponentFirmwareVersionListParams{
		Checksum: newFirmware.Checksum,
	}

	firmwares, _, err := s.client.ListServerComponentFirmware(ctx, &params)
	if err != nil {
		return errors.Wrap(ErrServerServiceQuery, "ListServerComponentFirmware: "+err.Error())
	}

	firmwareCount := len(firmwares)

	if firmwareCount == 0 {
		return s.createFirmware(ctx, newFirmware)
	}

	if firmwareCount != 1 {
		uuids := make([]string, len(firmwares))
		for i := range firmwares {
			uuids[i] = firmwares[i].UUID.String()
		}

		uuidLog := strings.Join(uuids, ",")

		s.logger.WithField("uuids", uuidLog).
			WithField("checksum", newFirmware.Checksum).
			Error("Multiple firmware IDs found with checksum")

		return errors.Wrap(ErrServerServiceDuplicateFirmware, uuidLog)
	}

	newFirmware.UUID = firmwares[0].UUID

	if isDifferent(newFirmware, &firmwares[0]) {
		return s.updateFirmware(ctx, newFirmware)
	}

	s.logger.WithField("firmware", newFirmware.Filename).
		WithField("vendor", newFirmware.Vendor).
		Debug("Firmware already exists and is up to date")

	return nil
}

func isDifferent(firmware1, firmware2 *serverservice.ComponentFirmwareVersion) bool {
	if firmware1.Vendor != firmware2.Vendor {
		return true
	}

	if firmware1.Filename != firmware2.Filename {
		return true
	}

	if firmware1.Version != firmware2.Version {
		return true
	}

	if firmware1.Component != firmware2.Component {
		return true
	}

	if firmware1.Checksum != firmware2.Checksum {
		return true
	}

	if firmware1.UpstreamURL != firmware2.UpstreamURL {
		return true
	}

	if firmware1.RepositoryURL != firmware2.RepositoryURL {
		return true
	}

	if strings.Join(firmware1.Model, ",") != strings.Join(firmware2.Model, ",") {
		return true
	}

	return false
}

func (s *serverService) createFirmware(ctx context.Context, firmware *serverservice.ComponentFirmwareVersion) error {
	id, response, err := s.client.CreateServerComponentFirmware(ctx, *firmware)

	if err != nil {
		return errors.Wrap(ErrServerServiceQuery, "CreateServerComponentFirmware: "+err.Error())
	}

	s.logger.WithField("response", response).
		WithField("firmware", firmware.Filename).
		WithField("vendor", firmware.Vendor).
		WithField("uuid", id).
		Info("Created firmware")

	return nil
}

func (s *serverService) updateFirmware(ctx context.Context, firmware *serverservice.ComponentFirmwareVersion) error {
	response, err := s.client.UpdateServerComponentFirmware(ctx, firmware.UUID, *firmware)
	if err != nil {
		return errors.Wrap(ErrServerServiceQuery, "UpdateServerComponentFirmware: "+err.Error())
	}

	s.logger.WithField("firmware", firmware.Filename).
		WithField("vendor", firmware.Vendor).
		WithField("response", response).
		Info("Updated firmware")

	return nil
}
