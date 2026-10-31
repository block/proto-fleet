package hostedaccounts_test

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClientTicketCreateRechecksPolicyAfterConcurrentSiteMove(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Moving target", f.user, hostedaccounts.AllCapabilities)
	module, controller := f.miningModule(t)
	var allowedSite, forbiddenSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Allowed','ticket-allowed') RETURNING id", f.org).Scan(&allowedSite))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Forbidden','ticket-forbidden') RETURNING id", f.org).Scan(&forbiddenSite))
	_, err := f.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", allowedSite, controller)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, account.AccountID, true, []int64{allowedSite}))
	store := sqlstores.NewSQLMaintenanceStore(f.db)
	refs := &ticketCreateResolutionGate{SQLMaintenanceStore: store, entered: make(chan struct{}), resume: make(chan struct{})}
	f.svc = hostedaccounts.NewService(f.db, maintenance.NewService(store, refs, sqlstores.NewSQLInventoryStore(f.db), sqlstores.NewSQLTransactor(f.db), nil))
	key := uuid.NewString()
	result := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		_, createErr := f.svc.CreateAccountTicket(ctx, f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Fault during site transfer", IdempotencyKey: key, Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MODULE, Id: module}}})
		result <- createErr
	}()
	select {
	case <-refs.entered:
	case <-ctx.Done():
		t.Fatal("ticket create never reached target resolution")
	}
	// Commit the move before the domain's first target lookup. Placement checks
	// will consistently see the new site, so client policy must check it too.
	_, err = f.db.ExecContext(ctx, "UPDATE device SET site_id=$1 WHERE id=$2", forbiddenSite, controller)
	close(refs.resume)
	require.NoError(t, err)
	select {
	case createErr := <-result:
		require.True(t, fleeterror.IsForbiddenError(createErr), "creation must reject the now-forbidden site: %v", createErr)
	case <-ctx.Done():
		t.Fatal("ticket creation did not finish")
	}
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT count(*) FROM repair_ticket WHERE org_id=$1 AND idempotency_key=$2", f.org, key).Scan(&count))
	require.Zero(t, count, "denied creation must not persist a ticket or its snapshots")
}

type ticketCreateResolutionGate struct {
	*sqlstores.SQLMaintenanceStore
	once            sync.Once
	entered, resume chan struct{}
}

func (s *ticketCreateResolutionGate) ResolveTicketAsset(ctx context.Context, orgID int64, kind models.TicketCategory, id int64) (*models.TicketAsset, error) {
	s.once.Do(func() {
		close(s.entered)
		select {
		case <-s.resume:
		case <-ctx.Done():
		}
	})
	return s.SQLMaintenanceStore.ResolveTicketAsset(ctx, orgID, kind, id)
}
