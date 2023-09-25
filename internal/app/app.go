package app

import (
	"context"
	"os"

	"github.com/bmc-toolbox/common"
	"github.com/metal-toolbox/firmware-syncer/internal/config"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/asrockrack"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/dell"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/equinix"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/intel"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/mellanox"
	"github.com/metal-toolbox/firmware-syncer/internal/vendors/supermicro"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	LogLevelInfo  = 0
	LogLevelDebug = 1
	LogLevelTrace = 2
)

type Syncer struct {
	dryRun  bool
	config  *config.Syncer
	logger  *logrus.Logger
	vendors []vendors.Vendor
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

	// Load up configs
	v := viper.New()
	v.SetConfigFile("config.yaml")
	v.AddConfigPath(".")
	err := v.ReadInConfig()

	if err != nil {
		logger.Error("Failed to find viper config file")
	}

	// Load configuration
	cfgSyncer, err := config.LoadSyncerConfig(configFile)
	if err != nil {
		logger.Error(err.Error())
		return nil, err
	}

	// Load firmware manifest
	firmwaresByVendor, err := config.LoadFirmwareManifest(context.TODO(), cfgSyncer.FirmwareManifestURL)
	if err != nil {
		logger.Error(err.Error())
		return nil, err
	}

	var fwVendors []vendors.Vendor

	for vendor, firmwares := range firmwaresByVendor {
		switch vendor {
		case common.VendorDell:
			var dup vendors.Vendor

			dup, err = dell.NewDUP(context.TODO(), firmwares, cfgSyncer, logger, v)
			if err != nil {
				logger.Error("Failed to initialize Dell vendor: " + err.Error())
				return nil, err
			}

			fwVendors = append(fwVendors, dup)
		case common.VendorAsrockrack:
			var asrr vendors.Vendor

			asrr, err = asrockrack.New(context.TODO(), firmwares, cfgSyncer, logger, v)
			if err != nil {
				logger.Error("Failed to initialize ASRockRack vendor:" + err.Error())
				return nil, err
			}

			fwVendors = append(fwVendors, asrr)
		case common.VendorSupermicro:
			var sm vendors.Vendor

			sm, err = supermicro.New(context.TODO(), firmwares, cfgSyncer, logger, v)
			if err != nil {
				logger.Error("Failed to initialize Supermicro vendor: " + err.Error())
				return nil, err
			}

			fwVendors = append(fwVendors, sm)
		case common.VendorMellanox:
			var mlx vendors.Vendor

			mlx, err = mellanox.New(context.TODO(), firmwares, cfgSyncer, logger, v)
			if err != nil {
				logger.Error("Failed to initialize Mellanox vendor: " + err.Error())
				return nil, err
			}

			fwVendors = append(fwVendors, mlx)
		case common.VendorIntel:
			var i vendors.Vendor

			i, err = intel.New(context.TODO(), firmwares, cfgSyncer, logger, v)
			if err != nil {
				logger.Error("Failed to initialize Intel vendor: " + err.Error())
				return nil, err
			}

			fwVendors = append(fwVendors, i)
		case "equinix":
			var e vendors.Vendor

			e, err = equinix.New(context.TODO(), firmwares, cfgSyncer, logger, v)
			if err != nil {
				logger.Error("Failed to initialize Equinix vendor: " + err.Error())
				return nil, err
			}

			fwVendors = append(fwVendors, e)
		default:
			logger.Error("Vendor not supported: " + vendor)
			continue
		}
	}

	return &Syncer{
		config:  cfgSyncer,
		logger:  logger,
		vendors: fwVendors,
	}, nil
}

// SyncFirmwares syncs all firmware files from the configured providers
func (s *Syncer) SyncFirmwares(ctx context.Context, dryRun bool) error {
	s.dryRun = dryRun

	for _, v := range s.vendors {
		err := v.Sync(ctx)
		if err != nil {
			s.logger.Error("Failed to sync: " + err.Error())
		}
	}

	return nil
}
