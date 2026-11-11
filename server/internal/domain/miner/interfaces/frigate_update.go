package interfaces

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// FrigateUpdater is the server-side view of the optional SDK capability
// sdk.CapabilityFrigateServerUpdate. It stages a signed Frigate package on each
// instance of a CT1 HA pair and drives or observes one self-coordinated rollout.
// Refusals are returned as *sdk.FrigateRefusal; use sdk.AsFrigateRefusal.
//
// Implementations that cannot update a device (a plugin without the capability,
// a FleetNode-owned controller) return an error rather than a partial result.
// Callers must check the device's advertised capabilities before relying on it.
//
//go:generate go run go.uber.org/mock/mockgen -source=frigate_update.go -destination=mocks/mock_frigate_updater.go -package=mocks FrigateUpdater
type FrigateUpdater interface {
	FrigateUpdateSupport(ctx context.Context) ([]sdk.FrigateInstanceSupport, error)
	FrigateStageBegin(ctx context.Context, instanceID string, m sdk.FrigateManifest) (sdk.FrigateStageProgress, error)
	FrigateStageAppend(ctx context.Context, instanceID string, a sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error)
	FrigateStageCommit(ctx context.Context, instanceID string, r sdk.FrigateStageRef) (sdk.FrigateStageCommit, error)
	FrigateStageAbort(ctx context.Context, instanceID string, r sdk.FrigateStageRef) error
	FrigateUpdateStart(ctx context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error)
	FrigateUpdateStatus(ctx context.Context, jobID string) (sdk.FrigateStatus, error)
	FrigateUpdateRetry(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error)
	FrigateUpdateCancel(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error)
}

var _ FrigateUpdater = sdk.DeviceFrigateUpdater(nil)
