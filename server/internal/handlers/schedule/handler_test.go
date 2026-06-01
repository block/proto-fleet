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

// Caller holds schedule:manage but not the underlying miner action key.
// The scheduler can't be used as an end-run around the action gate, so
// each action must deny with PermissionDenied. The grant case asserts
// the per-action key is the only thing missing from the deny case.
func TestRequireActionAuthority_PerAction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action      pb.ScheduleAction
		requiredKey string
	}{
		{pb.ScheduleAction_SCHEDULE_ACTION_REBOOT, authz.PermMinerReboot},
		{pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET, authz.PermMinerSetPowerTarget},
		{pb.ScheduleAction_SCHEDULE_ACTION_SLEEP, authz.PermMinerStopMining},
	}
	for _, tc := range cases {
		t.Run(tc.action.String()+"_denies_without_underlying_perm", func(t *testing.T) {
			t.Parallel()
			ctx := ctxWithPermissions(t, authz.PermScheduleManage)

			err := requireActionAuthority(ctx, tc.action)

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
		t.Run(tc.action.String()+"_allows_with_underlying_perm", func(t *testing.T) {
			t.Parallel()
			ctx := ctxWithPermissions(t, authz.PermScheduleManage, tc.requiredKey)
			require.NoError(t, requireActionAuthority(ctx, tc.action))
		})
	}
}

func TestRequireActionAuthority_UnspecifiedIsNoOp(t *testing.T) {
	t.Parallel()
	// UNSPECIFIED is the caller leaving the field empty; field
	// validation in the service layer rejects it with InvalidArgument.
	// requireActionAuthority must not short-circuit that flow.
	ctx := ctxWithPermissions(t) // no permissions
	require.NoError(t, requireActionAuthority(ctx, pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED))
}

func TestRequireActionAuthority_UnknownActionFailsClosed(t *testing.T) {
	t.Parallel()
	// A future proto value not yet mapped here must be rejected before
	// it reaches the service. If we no-opped instead, a schedule:manage
	// holder could persist a schedule the processor doesn't understand.
	unknown := pb.ScheduleAction(9999)
	ctx := ctxWithPermissions(t, authz.PermScheduleManage)

	err := requireActionAuthority(ctx, unknown)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInvalidArgument, fleetErr.GRPCCode)
}

// Every ScheduleService RPC must reject sessions that hold no schedule
// permissions before any service work runs. A future refactor that
// drops a RequirePermission line from a handler stays green only if
// this list keeps that handler covered.
func TestScheduleHandler_GatesEveryRPC(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil)
	ctx := ctxWithPermissions(t) // no permissions

	cases := []struct {
		name string
		call func() error
	}{
		{
			"ListSchedules",
			func() error {
				_, err := h.ListSchedules(ctx, connect.NewRequest(&pb.ListSchedulesRequest{}))
				return err
			},
		},
		{
			"CreateSchedule",
			func() error {
				_, err := h.CreateSchedule(ctx, connect.NewRequest(&pb.CreateScheduleRequest{}))
				return err
			},
		},
		{
			"UpdateSchedule",
			func() error {
				_, err := h.UpdateSchedule(ctx, connect.NewRequest(&pb.UpdateScheduleRequest{}))
				return err
			},
		},
		{
			"DeleteSchedule",
			func() error {
				_, err := h.DeleteSchedule(ctx, connect.NewRequest(&pb.DeleteScheduleRequest{}))
				return err
			},
		},
		{
			"PauseSchedule",
			func() error {
				_, err := h.PauseSchedule(ctx, connect.NewRequest(&pb.PauseScheduleRequest{}))
				return err
			},
		},
		{
			"ResumeSchedule",
			func() error {
				_, err := h.ResumeSchedule(ctx, connect.NewRequest(&pb.ResumeScheduleRequest{}))
				return err
			},
		},
		{
			"ReorderSchedules",
			func() error {
				_, err := h.ReorderSchedules(ctx, connect.NewRequest(&pb.ReorderSchedulesRequest{}))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.call()
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr, "expected fleeterror.FleetError, got %T", err)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
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
