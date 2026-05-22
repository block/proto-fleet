package auth

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
)

// Fast-path gate tests for the four user-management handlers. These
// run without a database — the authSvc is nil and the gate fails
// before any service call. The PermissionDenied path is the only thing
// under test; positive (gate-clears, body runs) coverage lives in the
// DB-backed handler_test.go integration tests.

func TestAuthHandler_userManagementGates_denyWithoutPermission(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil)

	cases := []struct {
		name        string
		permissions []string
		call        func(*Handler, context.Context) error
	}{
		{
			name:        "ListUsers without user:read",
			permissions: []string{authz.PermFleetRead},
			call: func(h *Handler, ctx context.Context) error {
				_, err := h.ListUsers(ctx, connect.NewRequest(&pb.ListUsersRequest{}))
				return err
			},
		},
		{
			name:        "CreateUser without user:manage",
			permissions: []string{authz.PermUserRead},
			call: func(h *Handler, ctx context.Context) error {
				_, err := h.CreateUser(ctx, connect.NewRequest(&pb.CreateUserRequest{Username: "x"}))
				return err
			},
		},
		{
			name:        "DeactivateUser without user:manage",
			permissions: []string{authz.PermUserRead},
			call: func(h *Handler, ctx context.Context) error {
				_, err := h.DeactivateUser(ctx, connect.NewRequest(&pb.DeactivateUserRequest{UserId: "u"}))
				return err
			},
		},
		{
			name:        "ResetUserPassword without user:manage",
			permissions: []string{authz.PermUserRead},
			call: func(h *Handler, ctx context.Context) error {
				_, err := h.ResetUserPassword(ctx, connect.NewRequest(&pb.ResetUserPasswordRequest{UserId: "u"}))
				return err
			},
		},
		{
			name:        "ListUsers with empty permission set",
			permissions: nil,
			call: func(h *Handler, ctx context.Context) error {
				_, err := h.ListUsers(ctx, connect.NewRequest(&pb.ListUsersRequest{}))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := handlerstest.CtxWithPermissions(t, 1, tc.permissions...)
			err := tc.call(h, ctx)

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr, "expected fleeterror.FleetError, got %T", err)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
}
