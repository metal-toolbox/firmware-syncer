package app

import (
	"context"
	"os"

	"github.com/bmc-toolbox/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/providers"
	"github.com/metal-toolbox/firmware-syncer/internal/providers/asrockrack"
	"github.com/metal-toolbox/firmware-syncer/internal/providers/dell"
	"github.com/metal-toolbox/firmware-syncer/internal/providers/supermicro"
)

var (
	LogLevelInfo  = 0
	LogLevelDebug = 1
	LogLevelTrace = 2
)

type Syncer struct {
	dryRun    bool
	config    *config.Syncer
	logger    *logrus.Logger
	providers []providers.Provider
}

// nolint:gocyclo // silence cyclo warning for now until function can be re-worked
// New returns a Syncer object configured with Providers
func New(configFile string, logLevel int) (*Syncer, error) {
	// Setup logger
	var logger = logrus.New()
	logger.Out = os.Stdout

	switch logLevel {
	case LogLevelDebug:
		logger.SetLevel(logrus.DebugLevel)
	case LogLevelTrace:
		logger.SetLevel(logrus.TraceLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}

	// Load configuration
	cfg, err := config.LoadSyncerConfig(configFile)
	if err != nil {
		logger.Error(err.Error())
		return nil, err
	}

	var provs []providers.Provider

	for _, cfgProvider := range cfg.Providers {
		switch cfgProvider.Vendor {
		case common.VendorDell:
			var dup providers.Provider

			dup, err = dell.NewDUP(context.TODO(), cfgProvider, cfg.ServerServiceURL, logger)
			if err != nil {
				logger.Error("Failed to initialize Dell provider: " + err.Error())
				return nil, err
			}

			provs = append(provs, dup)
		case common.VendorAsrockrack:
			var asrr providers.Provider

			asrr, err = asrockrack.New(context.TODO(), cfgProvider, cfg.ServerServiceURL, logger)
			if err != nil {
				logger.Error("Failed to initialize ASRockRack provider:" + err.Error())
				return nil, err
			}

			provs = append(provs, asrr)
		case common.VendorSupermicro:
			var sm providers.Provider

			sm, err = supermicro.New(context.TODO(), cfgProvider, cfg.ServerServiceURL, logger)
			if err != nil {
				logger.Error("Failed to initialize Supermicro provider: " + err.Error())
				return nil, err
			}

			provs = append(provs, sm)
		default:
			logger.Error("Provider not supported: " + cfgProvider.Vendor)
			return nil, errors.Wrap(config.ErrProviderNotSupported, cfgProvider.Vendor)
		}
	}

	return &Syncer{
		config:    cfg,
		logger:    logger,
		providers: provs,
	}, nil
}

// SyncFirmwares syncs all firmware files from the configured providers
func (s *Syncer) SyncFirmwares(ctx context.Context, dryRun bool) error {
	s.dryRun = dryRun

	for _, provider := range s.providers {
		err := provider.Sync(ctx)
		if err != nil {
			s.logger.Error("Failed to sync: " + err.Error())
		}
	}

	return nil
}
