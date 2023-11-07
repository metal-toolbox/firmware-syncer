package types

type (
	InventoryKind string
	// LogLevel is the logging level string.
	LogLevel string
)

const (
	AppName = "syncer"

	InventoryStoreYAML          InventoryKind = "yaml"
	InventoryStoreServerservice InventoryKind = "serverservice"

	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
	LogLevelTrace LogLevel = "trace"
)

// InventoryKinds returns the supported asset inventory, firmware configuration sources
func InventoryKinds() []InventoryKind {
	return []InventoryKind{InventoryStoreYAML, InventoryStoreServerservice}
}
