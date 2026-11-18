package hostedaccounts_test

import (
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	mpb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClientTicketProjectionKeepsTargetEvidenceInternal(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Evidence owner", f.user, hostedaccounts.DefaultCapabilities)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Module fault", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MODULE, Id: module}}})
	require.NoError(t, err)
	diagnosis, warranty, repair, rma := "Private diagnosis", "Private warranty", "Private repair", "Private RMA"
	host, err := ticketServiceForTest(f.db).UpdateTicketTarget(t.Context(), models.UpdateTargetParams{OrgID: f.org, TicketID: created.Ticket.Id, Kind: models.TicketCategoryModule, AssetID: module, ExpectedUpdatedAt: created.Ticket.UpdatedAt.AsTime(), Diagnosis: &diagnosis, WarrantyEvidence: &warranty, RepairOutcome: &repair, RMAOutcome: &rma})
	require.NoError(t, err)
	require.Equal(t, diagnosis, host.Targets[0].Diagnosis)
	require.Equal(t, warranty, host.Targets[0].WarrantyEvidence)
	require.Equal(t, repair, host.Targets[0].RepairOutcome)
	require.Equal(t, rma, host.Targets[0].RMAOutcome)
	view, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	listed, err := f.svc.ListAccountTickets(t.Context(), f.user, f.org, &pb.ListAccountTicketsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Tickets, 1)
	for _, ticket := range []*mpb.RepairTicket{view.Ticket, listed.Tickets[0]} {
		require.Len(t, ticket.Targets, 1)
		require.Equal(t, module, ticket.Targets[0].Id)
		require.Empty(t, ticket.Targets[0].Diagnosis)
		require.Empty(t, ticket.Targets[0].WarrantyEvidence)
		require.Empty(t, ticket.Targets[0].RepairOutcome)
		require.Empty(t, ticket.Targets[0].RmaOutcome)
	}
}

func TestParentTicketingDisableRevokesChildTicketWorkflow(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	created := createClientIssue(t, f, f.host, child.AccountID, "Child issue")
	disabled := false
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities, false, &disabled))
	_, err = f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: child.AccountID, TicketId: created.Ticket.Id})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	_, err = f.svc.CreateAccountTicket(t.Context(), f.host, f.org, &pb.CreateAccountTicketRequest{AccountId: child.AccountID, Description: "Another child issue", IdempotencyKey: uuid.NewString()})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	_, err = f.svc.CommentAccountTicket(t.Context(), f.host, f.org, &pb.CommentAccountTicketRequest{AccountId: child.AccountID, TicketId: created.Ticket.Id, Text: "Follow up", IdempotencyKey: uuid.NewString()})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.host, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: child.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Resolved", ExpectedUpdatedAt: created.Ticket.UpdatedAt})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	_, err = ticketServiceForTest(f.db).GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
	require.NoError(t, err, "disabling a parent cannot remove host records")
}

func TestClientTicketReopeningRejectsResolutionNote(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Reopening owner", f.user, hostedaccounts.DefaultCapabilities)
	created := createClientIssue(t, f, f.user, account.AccountID, "Issue")
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: "Resolved", ExpectedUpdatedAt: created.Ticket.UpdatedAt})
	require.NoError(t, err)
	_, err = f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id, ResolutionNote: "Must not be silently ignored", ExpectedUpdatedAt: closed.Ticket.UpdatedAt})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
}

func TestMaintenanceAssetKindFilterPrecedesResultLimit(t *testing.T) {
	f := setup(t)
	for range 200 {
		f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	}
	module, _ := f.miningModule(t)
	var board int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL", module).Scan(&board))
	for _, target := range []struct {
		kind models.TicketCategory
		id   int64
	}{{models.TicketCategoryModule, module}, {models.TicketCategoryHashboard, board}} {
		assets, err := ticketServiceForTest(f.db).ListTicketAssetOptions(t.Context(), models.TicketAssetOptionsParams{OrgID: f.org, Kind: target.kind})
		require.NoError(t, err)
		expected := 1
		if target.kind == models.TicketCategoryHashboard {
			expected = 3
		}
		require.Len(t, assets.Assets, expected, "earlier equipment kinds cannot consume the selected kind's result limit")
		require.Equal(t, target.id, assets.Assets[0].ID)
	}
	// Walk the combined picker past the old global cap without knowing a label.
	var all []models.TicketAsset
	request := models.TicketAssetOptionsParams{OrgID: f.org}
	for {
		page, err := ticketServiceForTest(f.db).ListTicketAssetOptions(t.Context(), request)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Assets), 50)
		all = append(all, page.Assets...)
		if page.NextPageToken == "" {
			break
		}
		require.NotEqual(t, request.PageToken, page.NextPageToken)
		request.PageToken = page.NextPageToken
	}
	require.Len(t, all, 204)
	seen := make(map[models.TicketAssetCursor]bool)
	for _, asset := range all {
		key := models.TicketAssetCursor{Kind: asset.Kind, ID: asset.ID}
		require.False(t, seen[key], "pagination must not repeat an asset")
		seen[key] = true
	}
	require.True(t, seen[models.TicketAssetCursor{Kind: models.TicketCategoryModule, ID: module}])
	require.True(t, seen[models.TicketAssetCursor{Kind: models.TicketCategoryHashboard, ID: board}])
}

func TestTicketModelChangeRequiresCurrentModuleSiteAuthority(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Moved equipment owner", f.user, hostedaccounts.DefaultCapabilities)
	module, controller := f.miningModule(t)
	var originalSite, currentSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Original site','original-model-site') RETURNING id", f.org).Scan(&originalSite))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Current site','current-model-site') RETURNING id", f.org).Scan(&currentSite))
	_, err := f.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", originalSite, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "UPDATE container_module SET model='Opening model' WHERE id=$1", module)
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, controller)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Module fault before move", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MODULE, Id: module}}})
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", currentSite, controller)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "UPDATE container_module SET model='Current model' WHERE id=$1", module)
	require.NoError(t, err)
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{originalSite}})
	model := "Model written from old ticket"
	_, err = ticketServiceForTest(f.db).UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: f.org, TicketID: created.Ticket.Id, Kind: models.TicketCategoryModule, AssetID: module, ExpectedUpdatedAt: created.Ticket.UpdatedAt.AsTime(), ModuleModel: &model})
	require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
	var recorded string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT model FROM container_module WHERE id=$1", module).Scan(&recorded))
	require.Equal(t, "Current model", recorded, "historical ticket access must not mutate out-of-scope live inventory")
	openingModel, diagnosis := "Opening model", "Historical repair evidence"
	updated, err := ticketServiceForTest(f.db).UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: f.org, TicketID: created.Ticket.Id, Kind: models.TicketCategoryModule, AssetID: module, ExpectedUpdatedAt: created.Ticket.UpdatedAt.AsTime(), ModuleModel: &openingModel, Diagnosis: &diagnosis})
	require.NoError(t, err, "resubmitting an unchanged historical model must not require live inventory authority")
	require.Equal(t, diagnosis, updated.Targets[0].Diagnosis)
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT model FROM container_module WHERE id=$1", module).Scan(&recorded))
	require.Equal(t, "Current model", recorded, "an unchanged snapshot must not overwrite a newer live inventory model")
}

func TestTicketModelAuthorizationRechecksSiteAfterPlacementLockWait(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Moving equipment owner", f.user, hostedaccounts.DefaultCapabilities)
	module, controller := f.miningModule(t)
	var originalSite, destinationSite int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Original','model-lock-original') RETURNING id", f.org).Scan(&originalSite))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "INSERT INTO site(org_id,name,slug) VALUES($1,'Destination','model-lock-destination') RETURNING id", f.org).Scan(&destinationSite))
	_, err := f.db.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", originalSite, controller)
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, controller)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Model evidence before move", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_MODULE, Id: module}}})
	require.NoError(t, err)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var movingPID int
	require.NoError(t, tx.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&movingPID))
	_, err = tx.ExecContext(t.Context(), "UPDATE device SET site_id=$1 WHERE id=$2", destinationSite, controller)
	require.NoError(t, err)
	ctx := authz.WithAuthorizedSiteScope(t.Context(), authz.AuthorizedSiteScope{SiteIDs: []int64{originalSite}})
	result := make(chan error, 1)
	go func() {
		model := "Unauthorized new model"
		_, saveErr := ticketServiceForTest(f.db).UpdateTicketTarget(ctx, models.UpdateTargetParams{OrgID: f.org, TicketID: created.Ticket.Id, Kind: models.TicketCategoryModule, AssetID: module, ExpectedUpdatedAt: created.Ticket.UpdatedAt.AsTime(), ModuleModel: &model})
		result <- saveErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := f.db.QueryRowContext(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", movingPID).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "model writes must wait for current device placement")
	require.NoError(t, tx.Commit())
	select {
	case saveErr := <-result:
		require.True(t, fleeterror.IsForbiddenError(saveErr), "%v", saveErr)
	case <-time.After(5 * time.Second):
		t.Fatal("model update did not finish after placement committed")
	}
	var model string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT model FROM container_module WHERE id=$1", module).Scan(&model))
	require.Empty(t, model, "a site move committed during the lock wait must revoke the live inventory write")
}

func TestBoardTicketRecordsActualParentModuleModelWithoutParentTicket(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Board owner", f.user, hostedaccounts.DefaultCapabilities)
	unrelated, _ := f.miningModule(t)
	module, _ := f.miningModule(t)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(6, module, 0)))
	var board int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL", module).Scan(&board))
	created, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "Board hardware fault", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory_TICKET_CATEGORY_HASHBOARD, Id: board, ParentModuleId: &module}}})
	require.NoError(t, err)
	actualModel := "BM1368 module"
	updated, err := ticketServiceForTest(f.db).UpdateTicketTarget(t.Context(), models.UpdateTargetParams{OrgID: f.org, TicketID: created.Ticket.Id, Kind: models.TicketCategoryHashboard, AssetID: board, ExpectedUpdatedAt: created.Ticket.UpdatedAt.AsTime(), ModuleModel: &actualModel})
	require.NoError(t, err, "recording board repair evidence must not require a duplicate parent ticket")
	require.Equal(t, actualModel, updated.Targets[0].Model)
	var recorded, untouched string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT model FROM container_module WHERE id=$1", module).Scan(&recorded))
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT model FROM container_module WHERE id=$1", unrelated).Scan(&untouched))
	require.Equal(t, actualModel, recorded)
	require.Empty(t, untouched, "a board ID must never be used as a module ID")
}

func TestSharedTicketRedactsOtherClientAssetsAndClosureConversation(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	delegated, _ := f.miningModule(t)
	retained, _ := f.miningModule(t)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	require.NoError(t, f.allocate(t, f.host, f.root, parent.AccountID, assign, selected(6, delegated, 0), selected(6, retained, 0)))
	require.NoError(t, f.allocate(t, f.user, parent.AccountID, child.AccountID, assign, selected(6, delegated, 1)))
	created := seedLegacyClientTicket(t, f, parent, models.CreateParams{Category: models.TicketCategoryModule, Description: "Private client submission", Component: "Private client component details", Targets: []models.TicketAssetSelection{{Kind: models.TicketCategoryModule, ID: delegated}, {Kind: models.TicketCategoryModule, ID: retained}}})
	require.Equal(t, "Private client component details", created.Ticket.Component)
	view, err := f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: child.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.Len(t, view.Ticket.Targets, 1)
	require.Equal(t, delegated, view.Ticket.Targets[0].Id)
	require.Empty(t, view.Ticket.Description)
	require.Empty(t, view.Ticket.Component, "client-supplied component text is private to its creator account")
	listed, err := f.svc.ListAccountTickets(t.Context(), f.host, f.org, &pb.ListAccountTicketsRequest{AccountId: child.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Tickets, 1)
	require.Empty(t, listed.Tickets[0].Component)
	require.Empty(t, listed.Tickets[0].Description)
	creatorView, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: parent.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.Equal(t, "Private client component details", creatorView.Ticket.Component)
	require.Equal(t, "Private client submission", creatorView.Ticket.Description)
	require.Empty(t, view.Ticket.Diagnosis)
	note := "Private client resolution details"
	closed, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.user, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: parent.AccountID, TicketId: created.Ticket.Id, Close: true, ResolutionNote: note, ExpectedUpdatedAt: created.Ticket.UpdatedAt})
	require.NoError(t, err)
	require.Equal(t, note, closed.Ticket.ResolutionNote)
	view, err = f.svc.GetAccountTicket(t.Context(), f.host, f.org, &pb.GetAccountTicketRequest{AccountId: child.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.Equal(t, mpb.TicketStatus_TICKET_STATUS_COMPLETED, view.Ticket.Status)
	require.Empty(t, view.Ticket.ResolutionNote)
	reopened, err := f.svc.ChangeAccountTicketStatus(t.Context(), f.host, f.org, &pb.ChangeAccountTicketStatusRequest{AccountId: child.AccountID, TicketId: created.Ticket.Id, ExpectedUpdatedAt: view.Ticket.UpdatedAt})
	require.NoError(t, err, "a visible closed ticket can be reopened without creator or host approval")
	require.Equal(t, mpb.TicketStatus_TICKET_STATUS_OPEN, reopened.Ticket.Status)
}

func TestVisibleTicketFieldUpdatesKeepOperationalNotesInternal(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	created := createClientIssue(t, f, f.user, account.AccountID, "Equipment fault")
	diagnosis := "Power supply fault confirmed"
	service := ticketServiceForTest(f.db)
	updated, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: f.org, ID: created.Ticket.Id, ExpectedUpdatedAt: created.Ticket.UpdatedAt.AsTime(), Diagnosis: &diagnosis})
	require.NoError(t, err)
	internal := "Host operational notes"
	_, err = service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: f.org, ID: created.Ticket.Id, ExpectedUpdatedAt: updated.UpdatedAt, Notes: &internal})
	require.NoError(t, err)
	view, err := f.svc.GetAccountTicket(t.Context(), f.user, f.org, &pb.GetAccountTicketRequest{AccountId: account.AccountID, TicketId: created.Ticket.Id})
	require.NoError(t, err)
	require.Empty(t, view.Ticket.Diagnosis, "host diagnosis stays internal")
	listed, err := f.svc.ListAccountTickets(t.Context(), f.user, f.org, &pb.ListAccountTicketsRequest{AccountId: account.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Tickets, 1)
	require.Empty(t, listed.Tickets[0].Diagnosis)
	require.Empty(t, view.Ticket.Notes, "host operational notes cannot appear in client ticket details")
	host, err := service.GetRepairTicket(t.Context(), f.org, created.Ticket.Id)
	require.NoError(t, err)
	require.Equal(t, diagnosis, *host.Ticket.Diagnosis)
	empty := ""
	cleared, err := service.UpdateRepairTicket(t.Context(), models.UpdateParams{OrgID: f.org, ID: created.Ticket.Id, ExpectedUpdatedAt: host.Ticket.UpdatedAt, Diagnosis: &empty})
	require.NoError(t, err)
	require.NotNil(t, cleared.Diagnosis)
	require.Empty(t, *cleared.Diagnosis)

}

func TestClientTicketRejectsCategoryBeforeNarrowingToStorageType(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Typed targets", f.user, hostedaccounts.DefaultCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, account.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	_, err := f.svc.CreateAccountTicket(t.Context(), f.user, f.org, &pb.CreateAccountTicketRequest{AccountId: account.AccountID, Description: "A valid assigned miner with a forged category", IdempotencyKey: uuid.NewString(), Targets: []*mpb.TicketAssetSelection{{Kind: mpb.TicketCategory(65537), Id: miner}}})
	require.Error(t, err, "a wider enum must not wrap to a permitted stored target category")
}
