package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClientTicketAssetsUseRackSiteForPickerAndCreate(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Rack equipment owner", f.user, hostedaccounts.DefaultCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	var site, otherSite, rack int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Rack site','rack-ticket-site') RETURNING id", f.org).Scan(&site))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Other site','other-ticket-site') RETURNING id", f.org).Scan(&otherSite))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO device_set(org_id,type,label) VALUES($1,'rack','Ticket rack') RETURNING id", f.org).Scan(&rack))
	_, err := f.db.ExecContext(t.Context(), "INSERT INTO device_set_rack(device_set_id,org_id,site_id,rows,columns) VALUES($1,$2,$3,1,1)", rack, f.org, site)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "INSERT INTO device_set_membership(org_id,device_set_id,device_set_type,device_id,device_identifier) SELECT $1,$2,'rack',id,device_identifier FROM device WHERE id=$3", f.org, rack, miner)
	require.NoError(t, err)
	var deviceSite *int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT site_id FROM device WHERE id=$1", miner).Scan(&deviceSite))
	require.Nil(t, deviceSite, "the fixture must depend on the rack placement")
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	rootCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), f.root)
	require.NoError(t, err)
	for _, allowed := range []bool{true, false} {
		policySite := otherSite
		if allowed {
			policySite = site
		}
		require.NoError(t, f.svc.ConfigureSites(rootCtx, f.host, f.org, account.AccountID, true, []int64{policySite}))
		options, err := f.svc.ListAccountTicketAssets(t.Context(), f.user, f.org, &pb.ListAccountTicketAssetsRequest{AccountId: account.AccountID})
		require.NoError(t, err)
		hostOptions, err := ticketServiceForTest(f.db).ListTicketAssetOptions(t.Context(), models.TicketAssetOptionsParams{OrgID: f.org, SiteIDs: []int64{policySite}, Kind: models.TicketCategoryMiner})
		require.NoError(t, err)
		request := &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Rack miner fault", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MINER, Id: miner}}}
		created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, request)
		if !allowed {
			require.Empty(t, options.Assets, "a mismatching site policy must hide rack-only equipment")
			require.Empty(t, hostOptions.Assets)
			require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
			var count int
			require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM repair_ticket WHERE org_id=$1", f.org).Scan(&count))
			require.Equal(t, 1, count, "denied creation must leave only the earlier authorized ticket")
			continue
		}
		require.NoError(t, err)
		require.Len(t, options.Assets, 1, "one-rack-per-device membership must produce one picker entry")
		require.Equal(t, miner, options.Assets[0].Id)
		require.Equal(t, &site, options.Assets[0].SiteId)
		require.Len(t, hostOptions.Assets, 1)
		require.Equal(t, miner, hostOptions.Assets[0].ID)
		require.Equal(t, &site, hostOptions.Assets[0].SiteID)
		detail, err := ticketServiceForTest(f.db).GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
		require.NoError(t, err)
		require.Equal(t, &site, detail.Ticket.SiteID)
		require.Equal(t, &rack, detail.Ticket.RackID)
		require.Len(t, detail.Ticket.Targets, 1)
		require.Equal(t, &site, detail.Ticket.Targets[0].SiteID, "the opening snapshot and picker must agree on rack placement")
	}
}
