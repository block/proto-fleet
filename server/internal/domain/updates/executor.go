package updates

import (
	"context"

	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type executorClient interface {
	Status(ctx context.Context) (updaterapi.StatusResponse, error)
	Trigger(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error)
	Acknowledge(ctx context.Context, operationID string) (updaterapi.Operation, error)
}

func newExecutorClient(socketPath string) executorClient {
	if socketPath == "" {
		return nil
	}
	return updaterapi.NewClient(socketPath)
}
