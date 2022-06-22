package app

import (
	"context"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/equinixmetal/firmware-syncer/internal/config"
	"github.com/equinixmetal/firmware-syncer/internal/providers"
	"github.com/equinixmetal/firmware-syncer/internal/providers/dell"
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

// New returns a Syncer object configured with Providers
func New(configFile string, logLevel int) *Syncer {
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
	}

	var provs []providers.Provider

	for _, cfgProvider := range cfg.Providers {
		switch cfgProvider.Vendor {
		case "dell":
			dellProvider, err := dell.New(context.TODO(), cfgProvider, logger)
			if err != nil {
				logger.Error("Failed to initialize Dell provider: " + err.Error())
			}

			provs = append(provs, dellProvider)
		default:
			logger.Error("Provider not supported: " + cfgProvider.Vendor)
		}
	}

	return &Syncer{
		config:    cfg,
		logger:    logger,
		providers: provs,
	}
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
