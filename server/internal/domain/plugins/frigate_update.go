package plugins

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

var _ interfaces.FrigateUpdater = (*PluginMiner)(nil)

// frigateUpdater returns the plugin device's update surface. Update calls
// bypass the command queue, and errors are returned unwrapped so callers can
// inspect *sdk.FrigateRefusal.
func (p *PluginMiner) frigateUpdater() (sdk.DeviceFrigateUpdater, error) {
	updater, ok := p.sdkDevice.(sdk.DeviceFrigateUpdater)
	if !ok || !p.caps[sdk.CapabilityFrigateServerUpdate] {
		return nil, fleeterror.NewUnimplementedError("plugin does not support controller software updates")
	}
	return updater, nil
}

// admitUpdate sends one update mutation through the device write gate. The
// gate revalidates the handle's stored binding (address, credentials and
// identity) and host ownership right before the call and keeps them locked
// until it returns, so a handle resolved before an unpair, re-pair or address
// change can never send that controller's credentials to a stale address. A
// handle without a gate is refused rather than dispatched. The call's own
// error is returned unwrapped so a *sdk.FrigateRefusal stays visible.
func (p *PluginMiner) admitUpdate(ctx context.Context, call func(context.Context) error) error {
	if p.writeGate == nil {
		return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError("controller handle has no write admission; resolve a fresh handle"))
	}
	invoked := false
	err := p.writeGate(ctx, commandtype.FrigateServerUpdate, func(ctx context.Context) error {
		invoked = true
		return call(ctx)
	})
	if err != nil && !invoked {
		return devicewrite.NotDispatched(err)
	}
	return err
}

// admitUpdateResult is admitUpdate for calls that return a value.
func admitUpdateResult[T any](ctx context.Context, p *PluginMiner, call func(context.Context, sdk.DeviceFrigateUpdater) (T, error)) (T, error) {
	var out T
	u, err := p.frigateUpdater()
	if err != nil {
		return out, err
	}
	err = p.admitUpdate(ctx, func(ctx context.Context) error {
		var callErr error
		out, callErr = call(ctx, u)
		return callErr
	})
	return out, err
}

// FrigateUpdateSupport and FrigateUpdateStatus only read, so they skip the gate.
func (p *PluginMiner) FrigateUpdateSupport(ctx context.Context) ([]sdk.FrigateInstanceSupport, error) {
	u, err := p.frigateUpdater()
	if err != nil {
		return nil, err
	}
	return u.FrigateUpdateSupport(ctx)
}

func (p *PluginMiner) FrigateUpdateStatus(ctx context.Context, jobID string) (sdk.FrigateStatus, error) {
	u, err := p.frigateUpdater()
	if err != nil {
		return sdk.FrigateStatus{}, err
	}
	return u.FrigateUpdateStatus(ctx, jobID)
}

func (p *PluginMiner) FrigateStageBegin(ctx context.Context, instanceID string, m sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	return admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (sdk.FrigateStageProgress, error) {
		return u.FrigateStageBegin(ctx, instanceID, m)
	})
}

func (p *PluginMiner) FrigateStageAppend(ctx context.Context, instanceID string, a sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	return admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (sdk.FrigateStageProgress, error) {
		return u.FrigateStageAppend(ctx, instanceID, a)
	})
}

func (p *PluginMiner) FrigateStageCommit(ctx context.Context, instanceID string, r sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	return admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (sdk.FrigateStageCommit, error) {
		return u.FrigateStageCommit(ctx, instanceID, r)
	})
}

func (p *PluginMiner) FrigateStageAbort(ctx context.Context, instanceID string, r sdk.FrigateStageRef) error {
	_, err := admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (struct{}, error) {
		return struct{}{}, u.FrigateStageAbort(ctx, instanceID, r)
	})
	return err
}

func (p *PluginMiner) FrigateUpdateStart(ctx context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	return admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (sdk.FrigateRollout, error) {
		return u.FrigateUpdateStart(ctx, r)
	})
}

func (p *PluginMiner) FrigateUpdateRetry(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (sdk.FrigateRollout, error) {
		return u.FrigateUpdateRetry(ctx, a)
	})
}

func (p *PluginMiner) FrigateUpdateCancel(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return admitUpdateResult(ctx, p, func(ctx context.Context, u sdk.DeviceFrigateUpdater) (sdk.FrigateRollout, error) {
		return u.FrigateUpdateCancel(ctx, a)
	})
}
