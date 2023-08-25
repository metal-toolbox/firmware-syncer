package app

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	runtime "github.com/banzaicloud/logrus-runtime-formatter"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	ErrAppInit = errors.New("error initializing app")
)

const (
	ProfilingEndpoint = "localhost:9091"
)

// App holds attributes for the firmware-syncer application
type App struct {
	// Viper loads configuration parameters.
	v *viper.Viper
	// firmware-syncer configuration.
	Config *Configuration
	// Logger is the app logger
	Logger *logrus.Logger
	// Kind is the type of application - worker
	Kind types.AppKind
}

// New returns a new instance of the firmware-syncer app
func New(appKind types.AppKind, inventoryKind types.InventoryKind, cfgFile, loglevel string, profiling bool) (*App, <-chan os.Signal, error) {
	if appKind != types.AppKindSyncer {
		return nil, nil, errors.Wrap(ErrAppInit, "invalid app kind: "+string(appKind))
	}

	app := &App{
		v:      viper.New(),
		Kind:   appKind,
		Config: &Configuration{},
		Logger: logrus.New(),
	}

	if err := app.LoadConfiguration(cfgFile, inventoryKind); err != nil {
		return nil, nil, err
	}

	switch types.LogLevel(loglevel) {
	case types.LogLevelDebug:
		app.Logger.Level = logrus.DebugLevel
	case types.LogLevelTrace:
		app.Logger.Level = logrus.TraceLevel
	default:
		app.Logger.Level = logrus.InfoLevel
	}

	runtimeFormatter := &runtime.Formatter{
		ChildFormatter: &logrus.JSONFormatter{},
		File:           true,
		Line:           true,
		BaseNameOnly:   true,
	}

	app.Logger.SetFormatter(runtimeFormatter)

	termCh := make(chan os.Signal, 1)

	// register for SIGINT, SIGTERM
	signal.Notify(termCh, syscall.SIGINT, syscall.SIGTERM)

	if profiling {
		enableProfilingEndpoint()
	}

	return app, termCh, nil
}

// enableProfilingEndpoint enables the profiling endpoint
func enableProfilingEndpoint() {
	go func() {
		server := &http.Server{
			Addr:              ProfilingEndpoint,
			ReadHeaderTimeout: 2 * time.Second, // nolint:gomnd // time duration value is clear as is.
		}

		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	log.Println("profiling enabled: " + ProfilingEndpoint + "/debug/pprof")
}
