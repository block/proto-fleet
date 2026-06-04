package pairing

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// ctxWithPerms builds the request context the auth interceptor would produce:
// session info plus the caller's effective org-scoped permissions.
func ctxWithPerms(perms ...string) context.Context {
	info := &session.Info{
		AuthMethod:     session.AuthMethodSession,
		SessionID:      "sess-1",
		UserID:         1,
		OrganizationID: 1,
		ExternalUserID: "user-1",
		Username:       "alice",
	}
	ctx := authn.SetInfo(context.Background(), info)
	return middleware.WithEffectivePermissions(ctx, authz.NewEffectivePermissions(
		[]authz.Assignment{{AssignmentID: 1, ScopeType: authz.ScopeOrg, Permissions: perms}},
	))
}

func TestCallerCanManageFleetNodes(t *testing.T) {
	tests := []struct {
		name  string
		perms []string
		want  bool
	}{
		{
			// The fan-out regression: miner:pair alone (no fleetnode:manage) must
			// NOT unlock fleet-node discovery commands.
			name:  "miner:pair only does not grant fleet-node management",
			perms: []string{authz.PermMinerPair},
			want:  false,
		},
		{
			name:  "fleetnode:manage grants it",
			perms: []string{authz.PermMinerPair, authz.PermFleetnodeManage},
			want:  true,
		},
		{
			name:  "fleetnode:read alone does not grant it",
			perms: []string{authz.PermMinerPair, authz.PermFleetnodeRead},
			want:  false,
		},
		{
			name:  "no permissions",
			perms: nil,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ctx := ctxWithPerms(tc.perms...)

			// Act
			got := callerCanManageFleetNodes(ctx)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}
}
