package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

// frigateUpdaterSDKDevice adds the update surface to the shared mock device.
type frigateUpdaterSDKDevice struct {
	*mockSDKDevice
	err   error
	calls []string
}

func (d *frigateUpdaterSDKDevice) rec(name string) error {
	d.calls = append(d.calls, name)
	return d.err
}

func (d *frigateUpdaterSDKDevice) FrigateUpdateSupport(context.Context) ([]sdk.FrigateInstanceSupport, error) {
	return []sdk.FrigateInstanceSupport{{InstanceID: "a"}}, d.rec("support")
}
func (d *frigateUpdaterSDKDevice) FrigateStageBegin(context.Context, string, sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	return sdk.FrigateStageProgress{NextOffset: 5}, d.rec("begin")
}
func (d *frigateUpdaterSDKDevice) FrigateStageAppend(context.Context, string, sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	return sdk.FrigateStageProgress{NextOffset: 6}, d.rec("append")
}
func (d *frigateUpdaterSDKDevice) FrigateStageCommit(context.Context, string, sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	return sdk.FrigateStageCommit{BytesStaged: 7}, d.rec("commit")
}
func (d *frigateUpdaterSDKDevice) FrigateStageAbort(context.Context, string, sdk.FrigateStageRef) error {
	return d.rec("abort")
}
func (d *frigateUpdaterSDKDevice) FrigateUpdateStart(context.Context, sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{JobID: "start"}, d.rec("start")
}
func (d *frigateUpdaterSDKDevice) FrigateUpdateStatus(context.Context, string) (sdk.FrigateStatus, error) {
	return sdk.FrigateStatus{AnsweredBy: "a"}, d.rec("status")
}
func (d *frigateUpdaterSDKDevice) FrigateUpdateRetry(context.Context, sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{JobID: "retry"}, d.rec("retry")
}
func (d *frigateUpdaterSDKDevice) FrigateUpdateCancel(context.Context, sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{JobID: "cancel"}, d.rec("cancel")
}

func frigateOperations(t *testing.T, u interface {
	sdk.DeviceFrigateUpdater
}) map[string]func() error {
	t.Helper()
	ctx := t.Context()
	return map[string]func() error{
		"support": func() error { _, err := u.FrigateUpdateSupport(ctx); return err },
		"begin":   func() error { _, err := u.FrigateStageBegin(ctx, "a", sdk.FrigateManifest{}); return err },
		"append":  func() error { _, err := u.FrigateStageAppend(ctx, "a", sdk.FrigateStageAppend{}); return err },
		"commit":  func() error { _, err := u.FrigateStageCommit(ctx, "a", sdk.FrigateStageRef{}); return err },
		"abort":   func() error { return u.FrigateStageAbort(ctx, "a", sdk.FrigateStageRef{}) },
		"start":   func() error { _, err := u.FrigateUpdateStart(ctx, sdk.FrigateStartRequest{}); return err },
		"status":  func() error { _, err := u.FrigateUpdateStatus(ctx, "j"); return err },
		"retry":   func() error { _, err := u.FrigateUpdateRetry(ctx, sdk.FrigateAction{}); return err },
		"cancel":  func() error { _, err := u.FrigateUpdateCancel(ctx, sdk.FrigateAction{}); return err },
	}
}

func TestPluginMinerFrigateUpdateRequiresCapabilityAndUpdater(t *testing.T) {
	_, base := createTestPluginMiner()
	t.Run("capability not advertised", func(t *testing.T) {
		device := &frigateUpdaterSDKDevice{mockSDKDevice: base}
		miner := createTestPluginMinerWithDevice(device)
		miner.caps = sdk.Capabilities{}
		for name, call := range frigateOperations(t, miner) {
			require.True(t, fleeterror.IsUnimplementedError(call()), name)
		}
		require.Empty(t, device.calls)
	})
	t.Run("device cannot update", func(t *testing.T) {
		miner := createTestPluginMinerWithDevice(base)
		miner.caps = sdk.Capabilities{sdk.CapabilityFrigateServerUpdate: true}
		for name, call := range frigateOperations(t, miner) {
			require.True(t, fleeterror.IsUnimplementedError(call()), name)
		}
	})
}

// gatedFrigateMiner returns an update-capable miner whose write gate records
// each admitted kind and then runs the call.
func gatedFrigateMiner() (*PluginMiner, *frigateUpdaterSDKDevice, *[]commandtype.Type) {
	_, base := createTestPluginMiner()
	device := &frigateUpdaterSDKDevice{mockSDKDevice: base}
	miner := createTestPluginMinerWithDevice(device)
	miner.caps = sdk.Capabilities{sdk.CapabilityFrigateServerUpdate: true}
	kinds := &[]commandtype.Type{}
	miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		*kinds = append(*kinds, kind)
		return call(ctx)
	}
	return miner, device, kinds
}

var frigateMutationNames = []string{"begin", "append", "commit", "abort", "start", "retry", "cancel"}

func TestPluginMinerFrigateUpdateDelegatesAndKeepsRefusals(t *testing.T) {
	miner, device, kinds := gatedFrigateMiner()
	for name, call := range frigateOperations(t, miner) {
		require.NoError(t, call(), name)
	}
	require.ElementsMatch(t, []string{"support", "begin", "append", "commit", "abort", "start", "status", "retry", "cancel"}, device.calls)
	require.Len(t, *kinds, len(frigateMutationNames), "every mutation, and only the mutations, pass the write gate")
	for _, kind := range *kinds {
		require.Equal(t, commandtype.FrigateServerUpdate, kind)
	}

	status, err := miner.FrigateUpdateStatus(t.Context(), "j")
	require.NoError(t, err)
	require.Equal(t, "a", status.AnsweredBy)

	device.err = &sdk.FrigateRefusal{Reason: "UPDATE_ACTIVE_JOB", Message: "busy", Code: codes.FailedPrecondition}
	for name, call := range frigateOperations(t, miner) {
		err := call()
		refusal := sdk.AsFrigateRefusal(err)
		require.NotNil(t, refusal, name)
		require.Equal(t, "UPDATE_ACTIVE_JOB", refusal.Reason, name)
	}
}

func TestPluginMinerFrigateUpdateMutationsNeedWriteAdmission(t *testing.T) {
	t.Run("no gate", func(t *testing.T) {
		miner, device, _ := gatedFrigateMiner()
		miner.writeGate = nil
		ops := frigateOperations(t, miner)
		for _, name := range frigateMutationNames {
			err := ops[name]()
			require.True(t, devicewrite.IsNotDispatched(err), name)
			require.True(t, fleeterror.IsFailedPreconditionError(err), name)
		}
		require.Empty(t, device.calls, "a handle without admission never reaches the controller")
		require.NoError(t, ops["status"](), "reads need no admission")
		require.NoError(t, ops["support"]())
	})
	t.Run("gate refuses", func(t *testing.T) {
		miner, device, _ := gatedFrigateMiner()
		stale := errors.New("binding changed")
		miner.writeGate = func(context.Context, commandtype.Type, func(context.Context) error) error { return stale }
		ops := frigateOperations(t, miner)
		for _, name := range frigateMutationNames {
			err := ops[name]()
			require.ErrorIs(t, err, stale, name)
			require.True(t, devicewrite.IsNotDispatched(err), name)
		}
		require.Empty(t, device.calls)
	})
}
