package metrics

import (
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	cptypes "github.com/metal-toolbox/conditionorc/pkg/types"
)

const (
	MetricsEndpoint = "0.0.0.0:9090"
)

var (
	// SyncBytesCounter metric measures the number of bytes transferred for update sync operations
	SyncBytesCounter *prometheus.CounterVec

	// SyncObjectsCounter metric measures the number of (file/directory) objects transferred for update sync operations
	SyncObjectsCounter *prometheus.CounterVec

	// SyncErrorsCounter metric measures the number of errors during update sync operations
	SyncErrorsCounter *prometheus.CounterVec

	EventsCounter *prometheus.CounterVec

	ConditionRunTimeSummary *prometheus.SummaryVec
	StoreQueryErrorCount    *prometheus.CounterVec

	NATSErrors *prometheus.CounterVec
)

func init() {
	// labelsSync are labels included in the Sync* metrics
	// vendor: the hardware vendor
	// actionKind: sync/verify
	labelsSync := []string{"vendor", "actionKind"}

	// SyncBytesCounter metric measures bytes transferred
	SyncBytesCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sync_bytes",
		Help: "A counter metric for bytes transferred by update sync operations",
	},
		labelsSync,
	)

	// SyncObjectsCounter metric measures file objects transferred
	SyncObjectsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sync_objects",
		Help: "A counter metric for objects transferred by update sync operations",
	},
		labelsSync,
	)

	// SyncErrorsCounter metric measures errors encountered during transfers
	SyncErrorsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sync_errors",
		Help: "A counter metric for errors when running update sync operations",
	},
		labelsSync,
	)
}

// UpdateSyncLabels is a helper method to return labels included in a update sync prometheus metric
//
// The cardinality of the labels returned here must match the ones defined in init()
func UpdateSyncLabels(deviceVendor, actionKind string) prometheus.Labels {
	return prometheus.Labels{
		"vendor":     deviceVendor,
		"actionKind": actionKind,
	}
}

// ListenAndServeMetrics exposes prometheus metrics as /metrics
func ListenAndServe() {
	go func() {
		http.Handle("/metrics", promhttp.Handler())

		server := &http.Server{
			Addr:              MetricsEndpoint,
			ReadHeaderTimeout: 2 * time.Second, // nolint:gomnd // time duration value is clear as is.
		}

		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()
}

// RegisterSpanEvent adds a span event along with the given attributes.
//
// event here is arbitrary and can be in the form of strings like - publishCondition, updateCondition etc
func RegisterSpanEvent(span trace.Span, condition *cptypes.Condition, workerID, firmwareID, event string) {
	span.AddEvent(event, trace.WithAttributes(
		attribute.String("workerID", workerID),
		attribute.String("firmwareID", firmwareID),
		attribute.String("conditionID", condition.ID.String()),
		attribute.String("conditionKind", string(condition.Kind)),
	))
}

func NATSError(op string) {
	NATSErrors.WithLabelValues(op).Inc()
}
