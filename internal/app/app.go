package app

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/equinixmetal/firmware-syncer/internal/config"
	"github.com/equinixmetal/firmware-syncer/internal/providers"
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
func New(configFile string, logger *logrus.Logger) *Syncer {
	// TODO: read config file
	// initilize providers
	cfg, err := config.LoadSyncerConfig(configFile)
	if err != nil {
		// log the error and exit
	}
	var provs []providers.Provider
	for _, cfgProvider := range cfg.Providers {
		// init the provider based on the cfg.provider
		p := providers.New(cfgProvider)
		provs = append(provs, p)

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
		provider.Sync(ctx)
	}

	return nil
}
