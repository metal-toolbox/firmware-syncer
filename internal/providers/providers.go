package providers

import (
	"context"

	"github.com/equinixmetal/firmware-syncer/internal/config"
)

type Provider interface {
	Sync(ctx context.Context) error
	Verify(ctx context.Context) error
}

func New(cfgProvider *config.Provider) Provider {
	// switch on cfgProvider.Vendor and then return the corresponding provider implementation
	switch cfgProvider.Vendor {
	case "dell":
		// instantiate dell provider without cyclic import
		//dellProvider, err := dell.New(cfgProvider)
		//if err != nil {
		//	// handle error
		//}
		return nil
	//case "supermicro":
	//	return supermicro.New(cfgProvider)
	//case "asrr":
	//	return asrr.ASRR(cfgProvider)
	default:
		return nil
	}
}
