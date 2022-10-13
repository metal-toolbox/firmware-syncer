package providers

import (
	"context"
	"sync"

	"github.com/metal-toolbox/firmware-syncer/internal/metrics"
)

const (
	MetricTransferredBytes   = "TransferredBytes"
	MetricTransferredObjects = "TransferredObjects"
	MetricErrorsCount        = "ErrorsCount"
	ActionSync               = "sync"
	ActionSign               = "sign"
	ActionVerify             = "verify"
)

type Vendor interface {
	Sync(ctx context.Context) error
	Stats() *Metrics
}

// Metrics is a struct with a key value map under an RWMutex
// to collect file transfer metrics in a provider syncer context
type Metrics struct {
	mutex       sync.RWMutex
	int64Values map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{int64Values: make(map[string]int64)}
}

// AddInt64Value adds a given string key and int64 value
func (m *Metrics) AddInt64Value(key string, value int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.int64Values[key] += value
}

// GetAllInt64Values returns a map of metrics that are of type int64
func (m *Metrics) GetAllInt64Values() map[string]int64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	values := map[string]int64{}
	for key, value := range m.int64Values {
		values[key] = value
	}

	return values
}

// Clear purges all existing key, values
func (m *Metrics) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.int64Values = make(map[string]int64)
}

// FromDownloader collects metrics from the given downloader object
func (m *Metrics) FromDownloader(downloader *Downloader, deviceVendor, actionKind string) {
	ds := downloader.Stats()

	// metrics returned in Status
	m.AddInt64Value(MetricTransferredBytes, ds.BytesTransferred)
	m.AddInt64Value(MetricTransferredObjects, ds.ObjectsTransferred)
	m.AddInt64Value(MetricErrorsCount, ds.Errors)

	// prometheus metrics
	metrics.SyncErrorsCounter.With(
		metrics.UpdateSyncLabels(deviceVendor, actionKind),
	).Add(float64(ds.Errors))

	metrics.SyncBytesCounter.With(
		metrics.UpdateSyncLabels(deviceVendor, actionKind),
	).Add(float64(ds.BytesTransferred))

	metrics.SyncObjectsCounter.With(
		metrics.UpdateSyncLabels(deviceVendor, actionKind),
	).Add(float64(ds.ObjectsTransferred))
}
