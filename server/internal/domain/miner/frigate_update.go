package miner

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// frigateRelayer is a command miner that runs update calls on a Fleet Node.
type frigateRelayer interface {
	FrigateRelay() sdk.FrigateRelay
}

var (
	_ interfaces.FrigateUpdater       = (*RemoteFleetNodeMiner)(nil)
	_ interfaces.FrigatePackageStager = (*RemoteFleetNodeMiner)(nil)
	_ frigateRelayer                  = (*remotenode.Miner)(nil)
)

// frigateUpdater returns the relay of the command miner, which runs update
// calls on the Fleet Node. A handle without one, such as a telemetry handle,
// has no way to reach the controllers for an update.
func (m *RemoteFleetNodeMiner) frigateUpdater() (interfaces.FrigateUpdater, error) {
	if relayer, ok := m.delegate.(frigateRelayer); ok {
		return relayer.FrigateRelay(), nil
	}
	return nil, fleeterror.NewFailedPreconditionError("Controller software updates are not available on this Fleet Node handle")
}

func (m *RemoteFleetNodeMiner) FrigateUpdateSupport(ctx context.Context) ([]sdk.FrigateInstanceSupport, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return nil, err
	}
	return updater.FrigateUpdateSupport(ctx)
}

func (m *RemoteFleetNodeMiner) FrigateStageBegin(ctx context.Context, instanceID string, manifest sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateStageProgress{}, err
	}
	return updater.FrigateStageBegin(ctx, instanceID, manifest)
}

// FrigateStageAppend is refused by the Fleet Node path: the node copies the
// package itself. See FrigateStagePackage.
func (m *RemoteFleetNodeMiner) FrigateStageAppend(ctx context.Context, instanceID string, a sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateStageProgress{}, err
	}
	return updater.FrigateStageAppend(ctx, instanceID, a)
}

func (m *RemoteFleetNodeMiner) FrigateStageCommit(ctx context.Context, instanceID string, ref sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateStageCommit{}, err
	}
	return updater.FrigateStageCommit(ctx, instanceID, ref)
}

func (m *RemoteFleetNodeMiner) FrigateStageAbort(ctx context.Context, instanceID string, ref sdk.FrigateStageRef) error {
	updater, err := m.frigateUpdater()
	if err != nil {
		return err
	}
	return updater.FrigateStageAbort(ctx, instanceID, ref)
}

func (m *RemoteFleetNodeMiner) FrigateUpdateStart(ctx context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateRollout{}, err
	}
	return updater.FrigateUpdateStart(ctx, r)
}

func (m *RemoteFleetNodeMiner) FrigateUpdateStatus(ctx context.Context, jobID string) (sdk.FrigateStatus, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateStatus{}, err
	}
	return updater.FrigateUpdateStatus(ctx, jobID)
}

func (m *RemoteFleetNodeMiner) FrigateUpdateRetry(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateRollout{}, err
	}
	return updater.FrigateUpdateRetry(ctx, a)
}

func (m *RemoteFleetNodeMiner) FrigateUpdateCancel(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	updater, err := m.frigateUpdater()
	if err != nil {
		return sdk.FrigateRollout{}, err
	}
	return updater.FrigateUpdateCancel(ctx, a)
}

// FrigateStagePackage has the Fleet Node download the package and copy it to
// the controllers.
func (m *RemoteFleetNodeMiner) FrigateStagePackage(ctx context.Context, pkg interfaces.FrigatePackageRef, targets []interfaces.FrigateStageTarget) ([]interfaces.FrigateStageOutcome, error) {
	stager, ok := m.delegate.(interfaces.FrigatePackageStager)
	if !ok {
		return nil, fleeterror.NewFailedPreconditionError("Controller software updates are not available on this Fleet Node handle")
	}
	return stager.FrigateStagePackage(ctx, pkg, targets)
}
