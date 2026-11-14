package containers

import (
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/stretchr/testify/require"
)

func TestBoardIdentifyRequiresLocatorPermissionAtBoardSite(t *testing.T) {
	site, other := int64(7), int64(8)
	permissions := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerBlinkLED}
	for missing := -1; missing < len(permissions); missing++ {
		perms := append([]string{}, permissions...)
		if missing >= 0 {
			perms = append(perms[:missing], perms[missing+1:]...)
		}
		ctx := middleware.WithEffectivePermissions(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1}), authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site, Permissions: perms}}))
		if missing < 0 {
			require.NoError(t, authorizeBoardIdentify(ctx, &site))
		} else {
			require.Error(t, authorizeBoardIdentify(ctx, &site), "missing %s", permissions[missing])
		}
		require.Error(t, authorizeBoardIdentify(ctx, &other))
		require.Error(t, authorizeBoardIdentify(ctx, nil))
	}
}
