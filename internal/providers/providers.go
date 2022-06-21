package providers

import (
	"context"
)

type Provider interface {
	Sync(ctx context.Context) error
	Verify(ctx context.Context) error
}
