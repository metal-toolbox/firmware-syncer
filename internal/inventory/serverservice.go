package inventory

import (
	"context"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc"
	"github.com/google/uuid"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
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

func New(ctx context.Context, cfg *config.ServerserviceOptions, artifactsURL string, logger *logrus.Logger) (*ServerService, error) {
	var client *serverservice.Client

	var err error

	if !cfg.DisableOAuth {
		client, err = newClientWithOAuth(ctx, cfg, logger)
		if err != nil {
			return nil, err
		}
	} else {
		client, err = serverservice.NewClientWithToken("fake", cfg.Endpoint, nil)
		if err != nil {
			return nil, err
		}
	}

	return &ServerService{
		artifactsURL: artifactsURL,
		client:       client,
		logger:       logger,
	}, nil
}

func newClientWithOAuth(ctx context.Context, cfg *config.ServerserviceOptions, logger *logrus.Logger) (client *serverservice.Client, err error) {
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

// nolint:gocyclo // silence cyclo warning
// Publish adds firmware data to Hollow's ServerService
func (s *ServerService) Publish(ctx context.Context, cfv *serverservice.ComponentFirmwareVersion, dstURL string) error {
	artifactsURL, err := s.getArtifactsURL(dstURL)
	if err != nil {
		return err
	}

	cfv.RepositoryURL = artifactsURL

	params := serverservice.ComponentFirmwareVersionListParams{
		Vendor:   cfv.Vendor,
		Version:  cfv.Version,
		Filename: cfv.Filename,
	}

	firmwares, _, err := s.client.ListServerComponentFirmware(ctx, &params)
	if err != nil {
		return errors.Wrap(ErrServerServiceQuery, "ListServerComponentFirmware: "+err.Error())
	}

	if len(firmwares) == 0 {
		var u *uuid.UUID

		u, _, err = s.client.CreateServerComponentFirmware(ctx, *cfv)
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
		// check if the firmware already includes this model
		var update bool

		for _, m := range cfv.Model {
			if !slices.Contains(firmwares[0].Model, m) {
				firmwares[0].Model = append(firmwares[0].Model, m)
				update = true
			} else {
				s.logger.WithFields(
					logrus.Fields{
						"uuid":    &firmwares[0].UUID,
						"vendor":  firmwares[0].Vendor,
						"model":   cfv.Model,
						"version": cfv.Version,
					},
				).Info("firmware already published for model")
			}
		}

		// Submit changed firmware to server service
		if update {
			_, err = s.client.UpdateServerComponentFirmware(ctx, firmwares[0].UUID, firmwares[0])
			if err != nil {
				return errors.Wrap(ErrServerServiceQuery, "UpdateServerComponentFirmware: "+err.Error())
			}

			s.logger.WithFields(
				logrus.Fields{
					"uuid":  &firmwares[0].UUID,
					"model": &firmwares[0].Model,
				},
			).Info("firmware updated with new models")
		}

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
