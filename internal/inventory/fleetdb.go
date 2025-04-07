package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/metal-toolbox/firmware-syncer/internal/config"

	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
)

var (
	ErrServerServiceDuplicateFirmware = errors.New("duplicate firmware found")
	ErrServerServiceQuery             = errors.New("server service query failed")
)

//go:generate mockgen -source=serverservice.go -destination=mocks/serverservice.go ServerService

type ServerService interface {
	Publish(ctx context.Context, newFirmware *fleetdbapi.ComponentFirmwareVersion) error
}

type serverService struct {
	artifactsURL string
	client       *fleetdbapi.Client
	logger       *logrus.Logger
}

func New(ctx context.Context, cfg *config.ServerserviceOptions, artifactsURL string, logger *logrus.Logger) (ServerService, error) {
	var client *fleetdbapi.Client

	var err error

	if !cfg.DisableOAuth {
		client, err = newClientWithOAuth(ctx, cfg)
		if err != nil {
			return nil, err
		}
	} else {
		client, err = fleetdbapi.NewClientWithToken("fake", cfg.Endpoint, nil)
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

func newClientWithOAuth(ctx context.Context, cfg *config.ServerserviceOptions) (client *fleetdbapi.Client, err error) {
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

	client, err = fleetdbapi.NewClient(cfg.EndpointURL.String(), oauthConfig.Client(ctx))
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (s *serverService) addRepositoryURL(fw *fleetdbapi.ComponentFirmwareVersion) (err error) {
	fw.RepositoryURL, err = url.JoinPath(s.artifactsURL, fw.Vendor, fw.Filename)

	return err
}

func (s *serverService) getCurrentFirmware(ctx context.Context, newFirmware *fleetdbapi.ComponentFirmwareVersion) (*fleetdbapi.ComponentFirmwareVersion, error) {
	params := fleetdbapi.ComponentFirmwareVersionListParams{
		Checksum: newFirmware.Checksum,
	}

	firmwares, _, err := s.client.ListServerComponentFirmware(ctx, &params)
	if err != nil {
		return nil, errors.Wrap(ErrServerServiceQuery, "ListServerComponentFirmware: "+err.Error())
	}

	firmwareCount := len(firmwares)

	if firmwareCount == 0 {
		return nil, nil
	}

	if firmwareCount != 1 {
		uuids := make([]string, len(firmwares))
		for i := range firmwares {
			uuids[i] = firmwares[i].UUID.String()
		}

		s.logger.WithField("matchingUUIDs", uuids).
			WithField("checksum", newFirmware.Checksum).
			WithField("firmware", newFirmware.Filename).
			WithField("vendor", newFirmware.Vendor).
			WithField("version", newFirmware.Version).
			Error("Multiple firmware IDs found with checksum")

		return nil, errors.Wrap(ErrServerServiceDuplicateFirmware, strings.Join(uuids, ","))
	}

	return &firmwares[0], nil
}

// Publish adds firmware data to Hollow's ServerService
func (s *serverService) Publish(ctx context.Context, newFirmware *fleetdbapi.ComponentFirmwareVersion) error {
	if err := s.addRepositoryURL(newFirmware); err != nil {
		return err
	}

	currentFirmware, err := s.getCurrentFirmware(ctx, newFirmware)
	if err != nil {
		return err
	}

	if currentFirmware == nil {
		return s.createFirmware(ctx, newFirmware)
	}

	newFirmware.UUID = currentFirmware.UUID
	newFirmware.Model = mergeModels(currentFirmware.Model, newFirmware.Model)

	isDifferent, err := hasDifferences(newFirmware, currentFirmware)
	if err != nil {
		return err
	}

	if isDifferent {
		return s.updateFirmware(ctx, newFirmware)
	}

	s.logger.WithField("firmware", newFirmware.Filename).
		WithField("uuid", newFirmware.UUID).
		WithField("vendor", newFirmware.Vendor).
		WithField("version", newFirmware.Version).
		Debug("Firmware already exists and is up to date")

	return nil
}

func mergeModels(models1, models2 []string) []string {
	allModels := models1
	modelsSet := make(map[string]bool)

	for _, model := range models1 {
		modelsSet[model] = true
	}

	for _, model := range models2 {
		if !modelsSet[model] {
			allModels = append(allModels, model)
		}
	}

	return allModels
}

// hasDifferences will return true if the two provided firmwares have differences.
// This simply marshals the structs to JSON and does a bytes comparison.
// Not using reflect.DeepEqual as that will also compare non-public fields (which we don't care about),
// and can cause a panic.
func hasDifferences(firmware1, firmware2 *fleetdbapi.ComponentFirmwareVersion) (bool, error) {
	firmware1JSON, err := json.Marshal(firmware1)
	if err != nil {
		return false, err
	}

	firmware2JSON, err := json.Marshal(firmware2)
	if err != nil {
		return false, err
	}

	return !bytes.Equal(firmware1JSON, firmware2JSON), nil
}

func (s *serverService) createFirmware(ctx context.Context, firmware *fleetdbapi.ComponentFirmwareVersion) error {
	id, _, err := s.client.CreateServerComponentFirmware(ctx, *firmware)
	if err != nil {
		return errors.Wrap(ErrServerServiceQuery, "CreateServerComponentFirmware: "+err.Error())
	}

	s.logger.WithField("firmware", firmware.Filename).
		WithField("version", firmware.Version).
		WithField("vendor", firmware.Vendor).
		WithField("uuid", id).
		Info("Created firmware")

	return nil
}

func (s *serverService) updateFirmware(ctx context.Context, firmware *fleetdbapi.ComponentFirmwareVersion) error {
	_, err := s.client.UpdateServerComponentFirmware(ctx, firmware.UUID, *firmware)
	if err != nil {
		return errors.Wrap(ErrServerServiceQuery, "UpdateServerComponentFirmware: "+err.Error())
	}

	s.logger.WithField("firmware", firmware.Filename).
		WithField("uuid", firmware.UUID).
		WithField("version", firmware.Version).
		WithField("vendor", firmware.Vendor).
		Info("Updated firmware")

	return nil
}
