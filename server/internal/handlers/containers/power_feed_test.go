package containers

import (
	"testing"
	"time"

	"connectrpc.com/authn"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPowerFeedReadRequiresReadPermissionsAtContainerSite(t *testing.T) {
	site, other := int64(7), int64(8)
	permissions := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead}
	for missing := -1; missing < len(permissions); missing++ {
		perms := append([]string{}, permissions...)
		if missing >= 0 {
			perms = append(perms[:missing], perms[missing+1:]...)
		}
		ctx := middleware.WithEffectivePermissions(authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1}), authz.NewEffectivePermissions([]authz.Assignment{{ScopeType: authz.ScopeSite, SiteID: &site, Permissions: perms}}))
		if missing < 0 {
			require.NoError(t, authorizePowerFeedRead(ctx, &site))
		} else {
			require.Error(t, authorizePowerFeedRead(ctx, &site), "missing %s", permissions[missing])
		}
		require.Error(t, authorizePowerFeedRead(ctx, &other))
		require.Error(t, authorizePowerFeedRead(ctx, nil))
	}
}

func TestModulePowerFeedResponseKeepsUnknownsDistinct(t *testing.T) {
	observed := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	restore := observed.Add(30 * time.Second)
	got := modulePowerFeedResponse(command.ModulePowerFeed{ModuleID: 9, DeviceIdentifier: "ct1", ContainerName: "CT1 A", Tank: 2, Module: 5, Inspection: sdk.ModulePowerFeedInspection{
		ObservedAt: observed,
		Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingAmbiguous, FeedID: "pdu-2-outlet-05", Reasons: []string{"feed_shared"}, SharedWith: []sdk.PowerFeedModuleRef{{Tank: 2, Module: 6}},
			Boards: []sdk.PowerFeedBoardEvidence{{Position: 1, Occupied: true, Serial: "sn-1", SlotFeedID: "pdu-2-outlet-05", WiringStatus: "unverified", WiringReasons: []string{"no_live_board_observed"}}}},
		Feed:               &sdk.PowerFeedReading{ID: "pdu-2-outlet-05", State: sdk.PowerFeedStateUnknown, VoltageV: new(0.0)},
		RecoveryCycleKnown: true,
		RecoveryCycle:      &sdk.PowerRecoveryCycle{ID: "cycle-1", OutletIDs: []string{"pdu-2-outlet-05"}, RestoreAfter: &restore},
	}})
	require.Equal(t, pb.PowerFeedMappingConfidence_POWER_FEED_MAPPING_CONFIDENCE_AMBIGUOUS, got.Mapping.Confidence)
	require.Equal(t, []*pb.PowerFeedModuleRef{{Tank: 2, Module: 6}}, got.Mapping.SharedWith)
	require.Equal(t, "sn-1", got.Mapping.Boards[0].SerialNumber)
	require.Equal(t, pb.PowerFeedState_POWER_FEED_STATE_UNSPECIFIED, got.Feed.State, "unknown state is not reported as enabled")
	require.NotNil(t, got.Feed.VoltageV, "a measured zero is kept")
	require.Zero(t, *got.Feed.VoltageV)
	require.Nil(t, got.Feed.PowerW, "an absent measurement stays unset")
	require.Nil(t, got.Feed.Active)
	require.Nil(t, got.ControllerWritePrimary)
	require.Equal(t, restore, got.RecoveryCycle.RestoreAfter.AsTime())
	require.Nil(t, got.RecoveryCycle.CreatedAt)
	require.Equal(t, observed, got.ObservedAt.AsTime())
}
