package miner

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestRemoteFleetNodeMinerRefusesFrigateUpdates(t *testing.T) {
	m := &RemoteFleetNodeMiner{}
	ctx := t.Context()
	for name, call := range map[string]func() error{
		"support": func() error { _, err := m.FrigateUpdateSupport(ctx); return err },
		"begin":   func() error { _, err := m.FrigateStageBegin(ctx, "a", sdk.FrigateManifest{}); return err },
		"append":  func() error { _, err := m.FrigateStageAppend(ctx, "a", sdk.FrigateStageAppend{}); return err },
		"commit":  func() error { _, err := m.FrigateStageCommit(ctx, "a", sdk.FrigateStageRef{}); return err },
		"abort":   func() error { return m.FrigateStageAbort(ctx, "a", sdk.FrigateStageRef{}) },
		"start":   func() error { _, err := m.FrigateUpdateStart(ctx, sdk.FrigateStartRequest{}); return err },
		"status":  func() error { _, err := m.FrigateUpdateStatus(ctx, "j"); return err },
		"retry":   func() error { _, err := m.FrigateUpdateRetry(ctx, sdk.FrigateAction{}); return err },
		"cancel":  func() error { _, err := m.FrigateUpdateCancel(ctx, sdk.FrigateAction{}); return err },
	} {
		err := call()
		require.Error(t, err, name)
		require.True(t, fleeterror.IsFailedPreconditionError(err), name)
		require.Contains(t, err.Error(), "FleetNode", name)
	}
}
