package containers

import (
	"testing"

	"connectrpc.com/authn"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestContainerMiningRequiresTheRequestedActionAtCurrentSite(t *testing.T) {
	site := int64(7)
	other := int64(8)
	for _, tc := range []struct {
		req        sdk.ContainerMiningRequest
		permission string
	}{
		{sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(true)}, authz.PermMinerStartMining},
		{sdk.ContainerMiningRequest{Action: "set_mining", Mining: new(false)}, authz.PermMinerStopMining},
		{sdk.ContainerMiningRequest{Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 2500}}, authz.PermMinerSetPowerTarget},
	} {
		all := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, tc.permission}
		for i := -1; i < len(all); i++ {
			perms := append([]string{}, all...)
			if i >= 0 {
				perms = append(perms[:i], perms[i+1:]...)
			}
			ctx := middleware.WithEffectivePermissions(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1}), authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site, Permissions: perms}}))
			if i < 0 {
				require.NoError(t, authorizeContainerMining(ctx, &site, tc.req))
			} else {
				require.Error(t, authorizeContainerMining(ctx, &site, tc.req))
			}
			require.Error(t, authorizeContainerMining(ctx, &other, tc.req))
			require.Error(t, authorizeContainerMining(ctx, nil, tc.req))
		}
	}
}
