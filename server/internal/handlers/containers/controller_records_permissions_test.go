package containers

import (
	"context"
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/stretchr/testify/require"
)

func TestControllerRecordsRequirePermissionsAtContainerSite(t *testing.T) {
	site, other := int64(7), int64(8)
	cases := []struct {
		name        string
		authorize   func(context.Context, *int64) error
		permissions []string
	}{
		{"logs", authorizeControllerLogs, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerDownloadLogs}},
		{"audit", authorizeControllerAudit, []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerDownloadLogs, authz.PermActivityRead}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for missing := -1; missing < len(tc.permissions); missing++ {
				perms := append([]string{}, tc.permissions...)
				if missing >= 0 {
					perms = append(perms[:missing], perms[missing+1:]...)
				}
				ctx := middleware.WithEffectivePermissions(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1}), authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site, Permissions: perms}}))
				if missing < 0 {
					require.NoError(t, tc.authorize(ctx, &site))
				} else {
					require.Error(t, tc.authorize(ctx, &site), "missing %s", tc.permissions[missing])
				}
				require.Error(t, tc.authorize(ctx, &other))
				require.Error(t, tc.authorize(ctx, nil))
			}
		})
	}
}

// The audit trail needs activity read at the container's site: an org-wide
// grant narrowed by a site role without it does not reach that site.
func TestControllerAuditHonoursSiteNarrowingOfActivityRead(t *testing.T) {
	site := int64(7)
	logs := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerDownloadLogs}
	ctx := middleware.WithEffectivePermissions(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1}), authz.NewEffectivePermissions([]authz.Assignment{
		{ScopeType: authz.ScopeOrg, Permissions: append(append([]string{}, logs...), authz.PermActivityRead)},
		{ScopeType: authz.ScopeSite, SiteID: &site, Permissions: logs},
	}))
	require.NoError(t, authorizeControllerLogs(ctx, &site))
	require.Error(t, authorizeControllerAudit(ctx, &site))
	other := int64(8)
	require.NoError(t, authorizeControllerAudit(ctx, &other))
}
