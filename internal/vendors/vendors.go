package vendors

import (
	"context"
	"sync"
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
