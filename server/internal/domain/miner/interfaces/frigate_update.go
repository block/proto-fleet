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

// FrigatePackageStager copies a whole package to controllers in one call. A
// controller that Fleet reaches through a Fleet Node implements it: package
// bytes do not cross the node as FrigateStageAppend calls. The node downloads
// the package once and copies it to each target.
//
// Before the call, the caller begins each copy with FrigateStageBegin and
// records the fingerprint. The call returns one outcome for each target, in
// order, or an error when no outcome is known.
type FrigatePackageStager interface {
	FrigateStagePackage(ctx context.Context, pkg FrigatePackageRef, targets []FrigateStageTarget) ([]FrigateStageOutcome, error)
}

// FrigatePackageRef identifies an uploaded package in Fleet's package store.
type FrigatePackageRef struct {
	ID        string
	Filename  string
	SizeBytes int64
	SHA256    string
}

// FrigateStageTarget is one controller that takes the package.
type FrigateStageTarget struct {
	InstanceID string
	Manifest   sdk.FrigateManifest
	// ManifestSHA256 is the fingerprint the controller returned when the copy
	// began. No bytes are sent if the controller now reports a different one.
	ManifestSHA256 string
}

// FrigateStageOutcome is the result for one target. Err is a
// *sdk.FrigateRefusal when the controller refused, which is final.
type FrigateStageOutcome struct {
	Commit sdk.FrigateStageCommit
	Err    error
}
