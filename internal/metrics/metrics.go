package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SyncBytesCounter metric measures the number of bytes transferred for update sync operations
	SyncBytesCounter *prometheus.CounterVec

	// SyncObjectsCounter metric measures the number of (file/directory) objects transferred for update sync operations
	SyncObjectsCounter *prometheus.CounterVec

	// SyncErrorsCounter metric measures the number of errors during update sync operations
	SyncErrorsCounter *prometheus.CounterVec
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
