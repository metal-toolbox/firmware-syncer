package store

import (
	"context"

	"github.com/google/uuid"
	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
)

type Repository interface {
	Publish(ctx context.Context, cfv *serverservice.ComponentFirmwareVersion, dstURL string) error
	FirmwareByID(ctx context.Context, fwID uuid.UUID) (*serverservice.ComponentFirmwareVersion, error)
}
