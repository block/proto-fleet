package miner

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

var _ interfaces.FrigateUpdater = (*RemoteFleetNodeMiner)(nil)

// errFrigateUpdateOnFleetNode is returned for every update operation: a
// FleetNode-owned controller is reached only through the node gateway, which
// carries no Frigate update protocol in this version.
func errFrigateUpdateOnFleetNode() error {
	return fleeterror.NewFailedPreconditionError("Frigate software updates are not supported for FleetNode-owned controllers")
}

func (m *RemoteFleetNodeMiner) FrigateUpdateSupport(context.Context) ([]sdk.FrigateInstanceSupport, error) {
	return nil, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateStageBegin(context.Context, string, sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	return sdk.FrigateStageProgress{}, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateStageAppend(context.Context, string, sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	return sdk.FrigateStageProgress{}, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateStageCommit(context.Context, string, sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	return sdk.FrigateStageCommit{}, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateStageAbort(context.Context, string, sdk.FrigateStageRef) error {
	return errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateUpdateStart(context.Context, sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{}, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateUpdateStatus(context.Context, string) (sdk.FrigateStatus, error) {
	return sdk.FrigateStatus{}, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateUpdateRetry(context.Context, sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{}, errFrigateUpdateOnFleetNode()
}

func (m *RemoteFleetNodeMiner) FrigateUpdateCancel(context.Context, sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{}, errFrigateUpdateOnFleetNode()
}
