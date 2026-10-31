package hostedaccounts_test

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTicketAudienceRetainsOpeningAccountAfterTransfer(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Original owner", f.user, hostedaccounts.DefaultCapabilities)
	b := f.client(t, "New owner", f.host, hostedaccounts.DefaultCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	recall := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, assign, selected(5, miner, 0)))
	created, _, err := ticketServiceForTest(f.db).CreateRepairTicket(t.Context(), models.CreateParams{OrgID: f.org, Category: models.TicketCategoryMiner, Component: "PSU", IdempotencyKey: uuid.NewString(), Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryMiner, ID: miner}}})
	require.NoError(t, err)
	ticket := created.ID
	var original bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM repair_ticket_audience WHERE org_id=$1 AND ticket_id=$2 AND account_id=$3)`, f.org, ticket, a.ID).Scan(&original))
	require.True(t, original, "host-created miner tickets capture the opening account as a potential recipient")
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, recall, selected(5, miner, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, b.AccountID, assign, selected(5, miner, 2)))
	var visible bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM repair_ticket_audience WHERE org_id=$1 AND ticket_id=$2 AND account_id=$3)`, f.org, ticket, b.ID).Scan(&visible))
	require.False(t, visible, "current asset ownership must not grant historical ticket visibility")
}
