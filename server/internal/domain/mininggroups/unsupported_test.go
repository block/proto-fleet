package mininggroups

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/session"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedAssignmentIsRejectedBeforeAdmission(t *testing.T) {
	for _, kind := range []string{"source_policy", "destination_policy_after_start", "source_policy_after_configure", "capacity"} {
		t.Run(kind, func(t *testing.T) {
			ctx, f, m, config, boards := newIdlePoolAssignmentFixture(t)
			switch kind {
			case "source_policy":
				m.groups[0].CustomPolicy = true
			case "destination_policy_after_start":
				m.groups[1].CustomPolicy = true
				m.groups[1].Mining = false
			case "source_policy_after_configure":
				m.groups[0].CustomPolicy = true
				m.groups[1].Pools = nil
			case "capacity":
				m.groups = m.groups[:1]
			}
			_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
			require.NoError(t, err)
			if kind == "capacity" {
				row := f.state(t)
				var observed sdk.MiningGroupResult
				err := decode(row.Observed, &observed)
				require.NoError(t, err)
				observed.MaxGroups = 1
				row.Observed, err = encode(observed)
				require.NoError(t, err)
				require.NoError(t, save(ctx, f.service.queries, row))
			}
			before := f.state(t)
			m.calls = nil
			request := uuid.NewString()
			_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, request, config, boards, before.Revision)
			require.Error(t, err)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			require.Equal(t, before, f.state(t), "rejected submissions must leave the journal unchanged")
			require.Empty(t, m.calls, "preflight must not mutate the controller")
			var attempts, intents, requests int
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attempts))
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hashboard_pool_intent WHERE device_id=$1`, f.target.DeviceID).Scan(&intents))
			require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_request WHERE request_id=$1`, request).Scan(&requests))
			require.Zero(t, attempts)
			require.Zero(t, intents)
			require.Zero(t, requests)
			require.Empty(t, groupActivities(t, f), "rejected requests must not be audited as accepted")
		})
	}
}

func TestNewPolicyRestrictionEndsSettledOperation(t *testing.T) {
	ctx, f, m, _, _ := newTransferFailureFixture(t)
	// The transfer was accepted before the controller acquired custom policy.
	m.groups[0].CustomPolicy = true
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	require.False(t, f.state(t).Step.Valid)
	require.Empty(t, m.receipts)
	_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err)
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Empty(t, intents)
}

func TestPolicyRestrictionCannotDiscardUnresolvedTransfer(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "lost_response"}[lost], func(t *testing.T) {
			ctx, f, m, _, _ := newTransferFailureFixture(t)
			m.transferPhase = pendingTransferPhase
			m.loseReply = lost
			f.reconcile(t, ctx)
			f.reconcile(t, ctx)
			original := f.state(t)
			m.groups[0].CustomPolicy = true
			for range 3 {
				f.reconcile(t, ctx)
			}
			require.Equal(t, original.Step, f.state(t).Step)
			require.Equal(t, original.StepKey, f.state(t).StepKey)
			require.True(t, active(f.state(t).Status))
			require.Len(t, groupActivities(t, f), 1, "unresolved work must retain only its acceptance event")
			_, err := f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.Error(t, err)
		})
	}
}

func TestPolicyChangeAfterConfigurationPreservesCompletedSetup(t *testing.T) {
	ctx, f, m, config, boards := newIdlePoolAssignmentFixture(t)
	m.groups[1].Pools = nil
	m.groups[1].Mining = false
	controller := &simulatedGroups{snapshot: sdk.MiningGroupResult{Supported: true, Enabled: true, Revision: 1, MaxGroups: 289, Groups: m.groups}, receipts: map[string]sdk.MiningGroupResult{}}
	f.service.miners = controller
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, f.state(t).Revision)
	require.NoError(t, err)
	f.reconcile(t, ctx) // Prepare pool configuration.
	f.reconcile(t, ctx) // Apply it and retain the successful receipt.
	applied := f.state(t)
	require.False(t, applied.Step.Valid)
	require.NotEmpty(t, controller.snapshot.Groups[1].Pools)
	controller.snapshot.Groups[0].CustomPolicy = true
	controller.calls = nil
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	require.Equal(t, applied.Bindings, f.state(t).Bindings)
	require.False(t, controller.snapshot.Groups[1].Mining, "newly unsupported work must stop before starting the destination")
	for _, call := range controller.calls {
		require.True(t, call.ReadOnly())
	}
	_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
	require.NoError(t, err)
}

func TestRejectedAssignmentPreservesEarlierIntent(t *testing.T) {
	ctx, f, m, config, boards := newTransferFailureFixture(t)
	m.transferPhase = transferPhaseComplete
	for range 3 {
		f.reconcile(t, ctx)
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	// Add an unselected sibling under custom policy so the new assignment would
	// split this group. The already applied intent must survive rejection.
	m.groups[1].CustomPolicy = true
	m.groups[1].MACs = append(m.groups[1].MACs, "02:00:00:00:00:02")
	_, err := f.service.GetState(ctx, f.target.DeviceIdentifier)
	require.NoError(t, err)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	profile := seedLegacyHostPoolRevision(t, ctx, sqlc.New(f.conn), f.service.secrets, info.OrganizationID, poolconfigurations.RevisionOptions{Name: "Other pools", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://other.example:3333", Username: "other"}}})
	before := f.state(t)
	_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), profile.RevisionID, boards, before.Revision)
	require.ErrorContains(t, err, "operator policy")
	require.Equal(t, before, f.state(t))
	intents, err := f.service.queries.ListHashboardPoolIntents(ctx, sqlc.ListHashboardPoolIntentsParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, config, intents[0].PoolRevisionID)
	require.Equal(t, before.OperationID.UUID, intents[0].OperationID)
}
