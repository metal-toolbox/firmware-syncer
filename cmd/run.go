package cmd

import (
	"context"
	"log"

	"github.com/equinix-labs/otel-init-go/otelinit"
	"github.com/metal-toolbox/firmware-syncer/app"
	"github.com/metal-toolbox/firmware-syncer/internal/metrics"
	"github.com/metal-toolbox/firmware-syncer/internal/store"
	"github.com/metal-toolbox/firmware-syncer/internal/syncer"
	"github.com/metal-toolbox/firmware-syncer/internal/version"
	"github.com/metal-toolbox/firmware-syncer/pkg/types"
	"github.com/spf13/cobra"
	"go.hollow.sh/toolbox/events"
)

var cmdRun = &cobra.Command{
	Use:   "run",
	Short: "Run firmware-syncer service to listen for events and sync firmware",
	Run: func(cmd *cobra.Command, args []string) {
		runWorker(cmd.Context())
	},
}

// run worker command
var (
	useStatusKV    bool
	dryrun         bool
	faultInjection bool
	facilityCode   string
	storeKind      string
	replicas       int
)

func runWorker(ctx context.Context) {
	theApp, termCh, err := app.New(
		types.AppKindSyncer,
		types.StoreKind(storeKind),
		cfgFile,
		logLevel,
		enableProfiling,
	)
	if err != nil {
		log.Fatal(err)
	}

	// serve metrics endpoint
	metrics.ListenAndServe()
	version.ExportBuildInfoMetric()

	ctx, otelShutdown := otelinit.InitOpenTelemetry(ctx, "firmware-syncer")
	defer otelShutdown(ctx)

	// Setup cancel context with cancel func.
	ctx, cancelFunc := context.WithCancel(ctx)

	// routine listens for termination signal and cancels the context
	go func() {
		<-termCh
		theApp.Logger.Info("got TERM signal, exiting...")
		cancelFunc()
	}()

	inv, err := store.New(ctx, theApp.Config.ServerserviceOptions, theApp.Logger)
	if err != nil {
		theApp.Logger.Fatal(err)
	}

	stream, err := events.NewStream(*theApp.Config.NatsOptions)
	if err != nil {
		theApp.Logger.Fatal(err)
	}

	if useStatusKV && facilityCode == "" {
		theApp.Logger.Fatal("--use-kv flag requires a --facility-code parameter")
	}

	w := syncer.New(
		facilityCode,
		dryrun,
		useStatusKV,
		faultInjection,
		theApp.Config.Concurrency,
		replicas,
		stream,
		inv,
		theApp.Logger,
	)

	w.Run(ctx)
}

func init() {
	cmdRun.PersistentFlags().StringVar(&storeKind, "store", "", "Inventory store to lookup firmwares for syncing - 'serverservice' or an inventory file with a .yml/.yaml extenstion")
	cmdRun.PersistentFlags().BoolVarP(&dryrun, "dry-run", "", false, "In dryrun mode, the worker actions the task without syncing firmware")
	cmdRun.PersistentFlags().BoolVarP(&useStatusKV, "use-kv", "", false, "When this is true, syncer writes status to a NATS KV store instead of sending reply messages (requires --facility-code)")
	cmdRun.PersistentFlags().BoolVarP(&faultInjection, "fault-injection", "", false, "Tasks can include a Fault attribute to allow fault injection for development purposes")
	cmdRun.PersistentFlags().IntVarP(&replicas, "replica-count", "r", 3, "The number of replicas to use for NATS data")
	cmdRun.PersistentFlags().StringVar(&facilityCode, "facility-code", "", "The facility code this syncer instance is associated with")

	if err := cmdRun.MarkPersistentFlagRequired("store"); err != nil {
		log.Fatal(err)
	}

	rootCmd.AddCommand(cmdRun)
}
