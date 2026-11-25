package mininggroups

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// The incident shape: an organization-owned whole-container assignment on a
// mining-group container whose module membership is incomplete. The membership
// gap hides the container from the ordinary assignment lookup and denies the
// ordinary whole-container removal authorization, yet the operator must still be
// able to discover and retire the retained intent (keep device settings only).
func TestUnjournaledRetainedContainerAssignmentIsRecoverable(t *testing.T) {
	ctx, f, miner, service, _, boards, _ := newActivationPoolFixture(t)
	checker := poolapplications.NewChecker(f.conn, poolapplications.NewReviewer(f.service.pools, sharedCapabilities{}), miner, f.service.secrets, f.service.pools)
	f.service.WithPoolRemovals(checker)
	service.WithChecker(checker)
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerActivateMiningGroups, authz.PermMinerUpdatePools})

	// Add a present module with no slots (and therefore no membership record),
	// reproducing the unconfirmed module that makes container inventory incomplete
	// and denies the normal whole-container removal authorization.
	var tank int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT m.tank_id FROM container_module m JOIN hashboard_installation i ON i.module_id=m.id WHERE i.hashboard_id=$1 LIMIT 1`, boards[0]).Scan(&tank))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at,state) VALUES($1,99,'unconfirmed-no-membership',now(),now(),'unknown')`, tank)
	require.NoError(t, err)

	pending, err := sqlc.New(f.conn).HasPendingMiningGroupPoolHandoff(ctx, sqlc.HasPendingMiningGroupPoolHandoffParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, pending, "fixture must be unjournaled")

	// (A) Recovery visibility: the retained assignment is surfaced for the selected
	// container even though the membership-gated lookup now excludes it.
	assignments, err := service.GetAssignments(ctx, "", []string{f.target.DeviceIdentifier}, nil, nil)
	require.NoError(t, err)
	var found *sqlc.ListPoolEquipmentAssignmentsRow
	for i := range assignments.Assignments {
		if assignments.Assignments[i].DeviceID == f.target.DeviceID {
			found = &assignments.Assignments[i]
		}
	}
	require.NotNil(t, found, "retained container assignment must be recoverable, not reported absent")
	require.NotEmpty(t, found.ConfigurationID)
	require.Equal(t, "recoverable", found.State)

	// (B) The operator can review and retire the retained assignment keep-settings.
	removal, err := service.ReviewRemoval(ctx, "", poolconfigurations.AssignmentTarget{Kind: poolconfigurations.TargetDevice, ID: f.target.DeviceID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.False(t, removal.ClearSupported, "a retained-recovery removal must never clear the device")

	p, err := f.service.pools.AuthorizePoolConfiguration(ctx, sqlc.New(f.conn), "", poolconfigurations.Read)
	require.NoError(t, err)

	// A clear request must be rejected; only keep-settings removal is permitted.
	err = f.service.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		return checker.EnqueueRemoval(ctx, q, p, removal.ID, false)
	})
	require.ErrorContains(t, err, "Keep device settings")

	var calls []sdk.MiningGroupRequest
	miner.beforeCall = func(_ context.Context, r sdk.MiningGroupRequest) { calls = append(calls, r) }
	writes := miner.writes
	err = f.service.writes.WithBindingUpdateTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		return checker.EnqueueRemoval(ctx, q, p, removal.ID, true)
	})
	require.NoError(t, err)

	source, err := sqlc.New(f.conn).GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: f.target.OrganizationID, TargetKind: poolconfigurations.TargetDevice, TargetID: f.target.DeviceID})
	require.NoError(t, err)
	require.True(t, source.RetiredAt.Valid, "the retained assignment must be retired")
	require.Equal(t, writes, miner.writes, "keep-settings removal must send no controller write")
	require.Empty(t, calls, "keep-settings removal must send no controller request")

	// The mining-group state row is left intact; only the assignment was retired.
	state, err := sqlc.New(f.conn).LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{OrganizationID: f.target.OrganizationID, DeviceID: f.target.DeviceID})
	require.NoError(t, err)
	require.Equal(t, "idle", state.Status)

	result, err := service.GetRemoval(ctx, "", removal.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", result.State)
	_ = boards
}

// Assigning hashboards while the whole-container assignment is still active must
// return an actionable conflict during review, not fail against the mixed-kind
// consistency trigger as an opaque internal error.
func TestConflictingKindAssignmentIsRejectedDuringReview(t *testing.T) {
	ctx, f, _, service, config, boards, _ := newActivationPoolFixture(t)
	setMiningGroupPermissions(t, ctx, f, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermPoolRead, authz.PermMinerActivateMiningGroups, authz.PermMinerUpdatePools})

	_, err := service.ReviewBoards(ctx, "", config.ID, config.Version, poolconfigurations.BoardSelection{HashboardIDs: boards})
	require.Error(t, err)
	require.ErrorContains(t, err, "whole-container pool assignment is still active")
	require.True(t, fleeterror.IsFailedPreconditionError(err), "the conflict must be actionable, not an internal error")
}
