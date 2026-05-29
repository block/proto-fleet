package schedule

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Action -> required miner permission is the security contract that
// keeps a schedule:manage holder from smuggling a privileged action
// through the scheduler. A drift here (new action, wrong perm) needs
// to fail loudly.
func TestRequiredPermForAction_Mapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action  pb.ScheduleAction
		wantKey string
		wantOk  bool
	}{
		{pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET, authz.PermMinerSetPowerTarget, true},
		{pb.ScheduleAction_SCHEDULE_ACTION_REBOOT, authz.PermMinerReboot, true},
		{pb.ScheduleAction_SCHEDULE_ACTION_SLEEP, authz.PermMinerStopMining, true},
		{pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.action.String(), func(t *testing.T) {
			t.Parallel()
			gotKey, gotOk := requiredPermForAction(tc.action)
			assert.Equal(t, tc.wantKey, gotKey)
			assert.Equal(t, tc.wantOk, gotOk)
		})
	}
}

func TestRequireActionAuthority_DeniesWithoutUnderlyingMinerPerm(t *testing.T) {
	t.Parallel()
	// Caller holds schedule:manage but not miner:reboot. Scheduling a
	// REBOOT must fail with PermissionDenied so the scheduler can't be
	// used as an end-run around the action gate.
	ctx := ctxWithPermissions(t, authz.PermScheduleManage)

	err := requireActionAuthority(ctx, pb.ScheduleAction_SCHEDULE_ACTION_REBOOT)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

func TestRequireActionAuthority_AllowsWithUnderlyingMinerPerm(t *testing.T) {
	t.Parallel()
	ctx := ctxWithPermissions(t, authz.PermScheduleManage, authz.PermMinerReboot)
	require.NoError(t, requireActionAuthority(ctx, pb.ScheduleAction_SCHEDULE_ACTION_REBOOT))
}

func TestRequireActionAuthority_UnspecifiedIsNoOp(t *testing.T) {
	t.Parallel()
	// UNSPECIFIED is the caller leaving the field empty; field
	// validation in the service layer rejects it with InvalidArgument.
	// requireActionAuthority must not short-circuit that flow.
	ctx := ctxWithPermissions(t) // no permissions
	require.NoError(t, requireActionAuthority(ctx, pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED))
}

func ctxWithPermissions(t *testing.T, permissions ...string) context.Context {
	t.Helper()
	ctx := authn.SetInfo(t.Context(), &session.Info{OrganizationID: 1})
	eff := authz.NewEffectivePermissions([]authz.Assignment{{
		AssignmentID: 1,
		ScopeType:    authz.ScopeOrg,
		Permissions:  permissions,
	}})
	return middleware.WithEffectivePermissions(ctx, eff)
}
