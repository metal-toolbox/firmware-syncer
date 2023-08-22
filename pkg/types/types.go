package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	cptypes "github.com/metal-toolbox/conditionorc/pkg/types"
)

type (
	AppKind   string
	StoreKind string
	// LogLevel is the logging level string.
	LogLevel string
)

const (
	AppName                                         = "firmware-syncer"
	AppKindSyncer             AppKind               = "worker"
	ConditionKindFirmwareSync cptypes.ConditionKind = "firmwareSync"

	InventoryStoreYAML          StoreKind = "yaml"
	InventoryStoreServerservice StoreKind = "serverservice"

	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
	LogLevelTrace LogLevel = "trace"
)

// AppKinds returns the supported firmware-syncer app kinds
func AppKinds() []AppKind { return []AppKind{AppKindSyncer} }

// StoreKinds returns the supported asset inventory, firmware configuration sources
func StoreKinds() []StoreKind {
	return []StoreKind{InventoryStoreYAML, InventoryStoreServerservice}
}

type Firmware struct{ ID uuid.UUID }

// Parameters are the parameters set for each firmware sync request
type Parameters struct {
	// Inventory identifier for the firmware to be synced.
	FirmwareID uuid.UUID `json:"firmwareID"`

	// Fault is a field to inject failures into a firmware syncer task execution,
	// this is set from the Condition only when the worker is run with fault-injection enabled.
	Fault *cptypes.Fault `json:"fault,-"`
}

const (
	Version int32 = 1
)

// StatusValue is the canonical structure for reporting status of an ongoing firmware sync
type StatusValue struct {
	UpdatedAt       time.Time       `json:"updated"`
	WorkerID        string          `json:"worker"`
	Target          string          `json:"target"`
	TraceID         string          `json:"traceID"`
	SpanID          string          `json:"spanID"`
	State           string          `json:"state"`
	Status          json.RawMessage `json:"status"`
	ResourceVersion int64           `json:"resourceVersion"` // for updates to server-service
	MsgVersion      int32           `json:"msgVersion"`
	// WorkSpec json.RawMessage `json:"spec"` XXX: for re-publish use-cases
}

// MustBytes sets the version field of the StatusValue so any callers don't have
// to deal with it. It will panic if we cannot serialize to JSON for some reason.
func (v *StatusValue) MustBytes() []byte {
	v.MsgVersion = Version
	byt, err := json.Marshal(v)
	if err != nil {
		panic("unable to serialize status value: " + err.Error())
	}
	return byt
}
