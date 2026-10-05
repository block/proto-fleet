package containers

import (
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/session"

	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/stretchr/testify/require"
)

func TestEquipmentWritePermissionsFollowCurrentSite(t *testing.T) {
	site := int64(7)
	other := int64(8)
	all := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerSetCoolingMode, authz.PermMinerSetPowerTarget}
	for i := -1; i < len(all); i++ {
		perms := append([]string{}, all...)
		if i >= 0 {
			perms = append(perms[:i], perms[i+1:]...)
		}
		ctx := middleware.WithEffectivePermissions(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1}), authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site, Permissions: perms}}))
		if i < 0 {
			require.NoError(t, authorizeEquipmentWrite(ctx, &site))
		} else {
			require.Error(t, authorizeEquipmentWrite(ctx, &site))
		}
		require.Error(t, authorizeEquipmentWrite(ctx, &other))
		require.Error(t, authorizeEquipmentWrite(ctx, nil))
	}
}
