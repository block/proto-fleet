package hostedaccounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	maintenancepb "github.com/block/proto-fleet/server/generated/grpc/maintenance/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func withAccountTickets[T any](ctx context.Context, s *Service, actorID, orgID int64, accountID string, write bool, action func(context.Context, sqlc.Querier, miningEstate) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, miningTransactionTimeout)
	defer cancel()
	read := func(q sqlc.Querier) (T, error) {
		var zero T
		admission := miningRead
		if write {
			admission = miningWrite
		}
		txCtx := db.WithTxQueries(ctx, q)
		e, err := loadMiningEstate(txCtx, q, actorID, orgID, accountID, false, admission)
		if err != nil {
			return zero, err
		}
		if e.account.Depth == 0 {
			return zero, denied()
		}
		path, err := loadPath(txCtx, q, orgID, accountID)
		if err != nil {
			return zero, err
		}
		for _, a := range path {
			if !a.TicketingEnabled {
				return zero, fleeterror.NewForbiddenError("ticketing is disabled for this account")
			}
		}
		if _, err := q.GetHostedAccountMembership(txCtx, sqlc.GetHostedAccountMembershipParams{OrganizationID: orgID, AccountID: e.account.ID, UserID: actorID}); err != nil {
			return zero, accessError(err)
		}
		return action(txCtx, q, e)
	}
	if q := db.GetTxQueries(ctx); q != nil {
		return read(q)
	}
	return db.WithTransactionTimeout(ctx, s.conn, miningTransactionTimeout, read)
}
func accountTicketDetail(ctx context.Context, q sqlc.Querier, orgID int64, e miningEstate, actorID, ticketID, beforeCommentID int64) (*pb.AccountTicketDetail, error) {
	row, err := q.GetAccountRepairTicket(ctx, sqlc.GetAccountRepairTicketParams{OrgID: orgID, AccountInternalID: e.account.ID, TicketID: ticketID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fleeterror.NewNotFoundError("ticket not found")
	}
	if err != nil {
		return nil, err
	}
	targets, err := q.ListAccountRepairTicketTargets(ctx, sqlc.ListAccountRepairTicketTargetsParams{OrgID: orgID, AccountInternalID: e.account.ID, TicketIds: []int64{ticketID}})
	if err != nil {
		return nil, err
	}
	comments, err := q.ListAccountRepairTicketComments(ctx, sqlc.ListAccountRepairTicketCommentsParams{OrgID: orgID, AccountInternalID: sql.NullInt64{Int64: e.account.ID, Valid: true}, ActorID: actorID, TicketID: ticketID, BeforeCommentID: beforeCommentID})
	if err != nil {
		return nil, err
	}
	if len(comments) > 0 && !comments[0].CursorValid {
		return nil, fleeterror.NewFailedPreconditionError("comment history changed; refresh the conversation before loading earlier comments")
	}
	canClose, err := q.CanAccountCloseRepairTicket(ctx, sqlc.CanAccountCloseRepairTicketParams{OrgID: orgID, TicketID: ticketID, AccountID: sql.NullInt64{Int64: e.account.ID, Valid: true}})
	if err != nil {
		return nil, err
	}
	result := &pb.AccountTicketDetail{Ticket: e.clientTicket(row, targets), CanClose: canClose}
	if len(comments) > 50 {
		comments = comments[:50]
		result.NextBeforeCommentId = comments[len(comments)-1].ID
	}
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		if c.ID == 0 {
			continue
		}
		result.Comments = append(result.Comments, &maintenancepb.TicketComment{Id: c.ID, TicketId: c.TicketID, UserName: c.UserName, Text: c.Text, CreatedAt: timestamppb.New(c.CreatedAt), AuthoredByCaller: c.AuthoredByCaller})
	}
	return result, nil
}

// clientTicket is the client field allowlist, including ownership and live site-policy redaction.
func (e miningEstate) clientTicket(row sqlc.RepairTicket, targets []sqlc.RepairTicketTarget) *maintenancepb.RepairTicket {
	ticket := &maintenancepb.RepairTicket{
		Id: row.ID, TicketNumber: row.TicketNumber, Category: maintenancepb.TicketCategory(row.Category), Status: maintenancepb.TicketStatus(row.Status), Urgent: row.Urgent,
		WarrantyStatus: maintenancepb.WarrantyStatus(row.WarrantyStatus), Resolution: maintenancepb.TicketResolution(row.Resolution), SharedWithClients: row.SharedWithClients,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
	if row.CompletedAt.Valid {
		ticket.CompletedAt = timestamppb.New(row.CompletedAt.Time)
	}
	if !row.CreatedByAccountID.Valid || row.CreatedByAccountID.Int64 == e.account.ID {
		ticket.Component = row.Component
		ticket.Description = row.Description
	}
	if row.CreatedByAccountID.Valid && row.CreatedByAccountID.Int64 == e.account.ID {
		ticket.CreatedByAccountId = e.account.AccountID
	}
	if !row.ResolutionNoteAccountID.Valid || row.ResolutionNoteAccountID.Int64 == e.account.ID {
		ticket.ResolutionNote = row.ResolutionNote
	}
	for _, row := range targets {
		target := &maintenancepb.TicketAsset{Kind: maintenancepb.TicketCategory(row.Kind), Id: row.AssetID}
		site := row.SiteID.Int64
		if !siteAllowed(e.policy, site) || !e.physicalRead(sqlc.ListHostedAccountMiningAssetsRow{SiteID: site}) {
			target.OutsidePolicy = true
		} else {
			target.Label = row.Label
			target.Model = row.Model
			target.MinerIdentifier = row.MinerIdentifier.String
			target.ModuleId = ticketIDPointer(row.ModuleID)
			target.InstallationId = ticketIDPointer(row.InstallationID)
			target.ContainerId = ticketIDPointer(row.ContainerID)
			target.SiteId = ticketIDPointer(row.SiteID)
			if row.Position.Valid {
				position := row.Position.Int32
				target.Position = &position
			}
		}
		ticket.Targets = append(ticket.Targets, target)
	}
	return ticket
}

func ticketIDPointer(id sql.NullInt64) *int64 {
	if !id.Valid {
		return nil
	}
	return &id.Int64
}

func (s *Service) GetAccountTicket(ctx context.Context, actorID, orgID int64, request *pb.GetAccountTicketRequest) (*pb.AccountTicketDetail, error) {
	if request.BeforeCommentId < 0 {
		return nil, fleeterror.NewInvalidArgumentError("invalid comment cursor")
	}
	return withAccountTickets(ctx, s, actorID, orgID, request.AccountId, false, func(ctx context.Context, q sqlc.Querier, e miningEstate) (*pb.AccountTicketDetail, error) {
		return accountTicketDetail(ctx, q, orgID, e, actorID, request.TicketId, request.BeforeCommentId)
	})
}
func (s *Service) ListAccountTickets(ctx context.Context, actorID, orgID int64, request *pb.ListAccountTicketsRequest) (*pb.ListAccountTicketsResponse, error) {
	if request.BeforeId < 0 {
		return nil, fleeterror.NewInvalidArgumentError("invalid ticket cursor")
	}
	var statuses []int16
	for _, status := range request.Statuses {
		if status < maintenancepb.TicketStatus_TICKET_STATUS_OPEN || status > maintenancepb.TicketStatus_TICKET_STATUS_COMPLETED {
			return nil, fleeterror.NewInvalidArgumentError("invalid ticket status")
		}
		statuses = append(statuses, int16(status)) //nolint:gosec // status was checked against the bounded 1..5 enum above
	}
	return withAccountTickets(ctx, s, actorID, orgID, request.AccountId, false, func(ctx context.Context, q sqlc.Querier, e miningEstate) (*pb.ListAccountTicketsResponse, error) {
		rows, err := q.ListAccountRepairTickets(ctx, sqlc.ListAccountRepairTicketsParams{OrgID: orgID, AccountInternalID: e.account.ID, BeforeID: request.BeforeId, Statuses: statuses})
		if err != nil {
			return nil, err
		}
		result := &pb.ListAccountTicketsResponse{}
		if len(rows) > 50 {
			rows = rows[:50]
			result.NextBeforeId = rows[len(rows)-1].ID
		}
		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		targets, err := q.ListAccountRepairTicketTargets(ctx, sqlc.ListAccountRepairTicketTargetsParams{OrgID: orgID, AccountInternalID: e.account.ID, TicketIds: ids})
		if err != nil {
			return nil, err
		}
		byTicket := make(map[int64][]sqlc.RepairTicketTarget)
		for _, target := range targets {
			byTicket[target.TicketID] = append(byTicket[target.TicketID], target)
		}
		for _, row := range rows {
			result.Tickets = append(result.Tickets, e.clientTicket(row, byTicket[row.ID]))
		}
		return result, nil
	})
}
func ticketSelections(targets []*maintenancepb.TicketAssetSelection) ([]models.TicketAssetSelection, error) {
	if len(targets) > 100 {
		return nil, fleeterror.NewInvalidArgumentError("select at most 100 assets")
	}
	result := make([]models.TicketAssetSelection, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.Id <= 0 {
			return nil, fleeterror.NewInvalidArgumentError("invalid ticket asset")
		}
		kind, validKind := models.PhysicalTicketCategory(int32(target.Kind))
		if !validKind {
			return nil, fleeterror.NewInvalidArgumentError("invalid ticket asset kind")
		}
		if (kind == models.TicketCategoryHashboard) != (target.ParentModuleId != nil) || target.GetParentModuleId() < 0 {
			return nil, fleeterror.NewInvalidArgumentError("hashboards require their current parent module")
		}
		result = append(result, models.TicketAssetSelection{Kind: kind, ID: target.Id, ParentModuleID: target.ParentModuleId})
	}
	return result, nil
}
func (s *Service) CreateAccountTicket(ctx context.Context, actorID, orgID int64, request *pb.CreateAccountTicketRequest) (*pb.AccountTicketDetail, error) {
	description := strings.TrimSpace(request.Description)
	if description == "" || utf8.RuneCountInString(description) > 4096 {
		return nil, fleeterror.NewInvalidArgumentError("a problem description is required")
	}
	targets, err := ticketSelections(request.Targets)
	if err != nil {
		return nil, err
	}
	component := strings.TrimSpace(request.Component)
	if component == "" {
		component = "Unknown"
	}
	params := models.CreateParams{ShareWithClients: true, OrgID: orgID, IdempotencyKey: strings.TrimSpace(request.IdempotencyKey), Description: description, Component: component, Category: models.TicketCategoryAccount, Targets: targets}
	if len(targets) > 0 {
		params.Category = targets[0].Kind
		for _, target := range targets {
			if target.Kind == models.TicketCategoryModule {
				params.Category = models.TicketCategoryModule
			}
		}
	}
	return withAccountTickets(ctx, s, actorID, orgID, request.AccountId, true, func(ctx context.Context, q sqlc.Querier, e miningEstate) (*pb.AccountTicketDetail, error) {
		params.CreatedByAccountID = &e.account.ID
		params.BeforeCreate = func(ctx context.Context) error {
			for _, target := range targets {
				allowed, err := q.CanTargetAccountRepairAsset(ctx, sqlc.CanTargetAccountRepairAssetParams{OrgID: orgID, AccountInternalID: sql.NullInt64{Int64: e.account.ID, Valid: true}, Kind: int16(target.Kind), AssetID: target.ID, ParentModuleID: nullableTicketID(target.ParentModuleID)})
				if err != nil {
					return err
				}
				if !allowed {
					return fleeterror.NewNotFoundError("ticket asset not found or not assigned to this account")
				}
				option, err := q.ResolveRepairTicketAsset(ctx, sqlc.ResolveRepairTicketAssetParams{OrgID: orgID, Kind: int16(target.Kind), ID: target.ID})
				if err != nil {
					return err
				}
				site := option.SiteID.Int64
				if !siteAllowed(e.policy, site) || !e.physicalRead(sqlc.ListHostedAccountMiningAssetsRow{SiteID: site}) {
					return denied()
				}
			}
			return nil
		}
		created, createdNew, err := s.tickets.CreateRepairTicket(ctx, params)
		if err != nil {
			return nil, err
		}
		if createdNew {
			if err := auditAccountTicket(ctx, q, actorID, e.account, "create_account_ticket", created.ID, nil); err != nil {
				return nil, err
			}
		}
		return accountTicketDetail(ctx, q, orgID, e, actorID, created.ID, 0)
	})
}
func nullableTicketID(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *id, Valid: true}
}
func (s *Service) CommentAccountTicket(ctx context.Context, actorID, orgID int64, request *pb.CommentAccountTicketRequest) (*pb.AccountTicketDetail, error) {
	return withAccountTickets(ctx, s, actorID, orgID, request.AccountId, true, func(ctx context.Context, q sqlc.Querier, e miningEstate) (*pb.AccountTicketDetail, error) {
		if _, err := q.GetAccountTicketAccess(ctx, sqlc.GetAccountTicketAccessParams{OrgID: orgID, AccountInternalID: e.account.ID, TicketID: request.TicketId}); errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("ticket not found")
		} else if err != nil {
			return nil, err
		}
		key := strings.TrimSpace(request.IdempotencyKey)
		comment, createdNew, err := s.tickets.CreateComment(ctx, orgID, request.TicketId, actorID, "", request.Text, key, models.CommentScope{AccountID: request.AccountId})
		if err != nil {
			return nil, err
		}
		if createdNew {
			if err := auditAccountTicket(ctx, q, actorID, e.account, "comment_account_ticket", request.TicketId, map[string]any{"comment_id": comment.ID}); err != nil {
				return nil, err
			}
		}
		return accountTicketDetail(ctx, q, orgID, e, actorID, request.TicketId, 0)
	})
}
func (s *Service) ChangeAccountTicketStatus(ctx context.Context, actorID, orgID int64, request *pb.ChangeAccountTicketStatusRequest) (*pb.AccountTicketDetail, error) {
	if !request.Close && request.ResolutionNote != "" {
		return nil, fleeterror.NewInvalidArgumentError("resolution_note is only valid when closing a ticket")
	}
	if request.ExpectedUpdatedAt == nil || request.ExpectedUpdatedAt.CheckValid() != nil {
		return nil, fleeterror.NewInvalidArgumentError("expected_updated_at is required")
	}
	return withAccountTickets(ctx, s, actorID, orgID, request.AccountId, true, func(ctx context.Context, q sqlc.Querier, e miningEstate) (*pb.AccountTicketDetail, error) {
		current, err := q.GetAccountTicketAccess(ctx, sqlc.GetAccountTicketAccessParams{OrgID: orgID, AccountInternalID: e.account.ID, TicketID: request.TicketId})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("ticket not found")
		}
		if err != nil {
			return nil, err
		}
		status := models.TicketStatusOpen
		params := models.UpdateParams{OrgID: orgID, ID: request.TicketId, ExpectedUpdatedAt: request.ExpectedUpdatedAt.AsTime()}
		if request.Close {
			if !current.CreatedByAccountID.Valid || current.CreatedByAccountID.Int64 != e.account.ID {
				return nil, fleeterror.NewForbiddenError("clients can close only tickets created by their account")
			}
			note := strings.TrimSpace(request.ResolutionNote)
			if note == "" {
				return nil, fleeterror.NewInvalidArgumentError("a client-visible resolution note is required")
			}
			status = models.TicketStatusCompleted
			resolution := models.TicketResolutionNoActionNeeded
			params.Resolution = &resolution
			params.ResolutionNote = &note
			params.ClosingAccountID = &e.account.ID
		} else if models.TicketStatus(current.Status) != models.TicketStatusCompleted {
			return nil, fleeterror.NewFailedPreconditionError("only closed tickets can be reopened")
		}
		params.Status = &status
		if _, err = s.tickets.UpdateRepairTicket(ctx, params); err != nil {
			return nil, err
		}
		event := "reopen_account_ticket"
		if request.Close {
			event = "close_account_ticket"
		}
		if err := auditAccountTicket(ctx, q, actorID, e.account, event, request.TicketId, nil); err != nil {
			return nil, err
		}
		return accountTicketDetail(ctx, q, orgID, e, actorID, request.TicketId, 0)
	})
}
func (s *Service) ListAccountTicketAssets(ctx context.Context, actorID, orgID int64, request *pb.ListAccountTicketAssetsRequest) (*pb.ListAccountTicketAssetsResponse, error) {
	return withAccountTickets(ctx, s, actorID, orgID, request.AccountId, false, func(ctx context.Context, q sqlc.Querier, e miningEstate) (*pb.ListAccountTicketAssetsResponse, error) {
		var sites []int64
		if e.policy.EffectiveRestricted {
			sites = e.policy.EffectiveSiteIDs
			if len(sites) == 0 {
				return &pb.ListAccountTicketAssetsResponse{}, nil
			}
		}
		search := strings.TrimSpace(request.Search)
		query := models.TicketAssetQuery{Scope: "account-assets", OrgID: orgID, AccountID: e.account.ID, SiteIDs: sites, Search: search}.Fingerprint()
		size, cursor, err := models.ParseTicketAssetPagination(request.PageSize, request.PageToken, query)
		if err != nil {
			return nil, err
		}
		authority, err := e.physicalReadAuthority()
		if err != nil {
			return nil, err
		}
		rows, err := q.ListRepairTicketAssetOptions(ctx, sqlc.ListRepairTicketAssetOptionsParams{HostAuthority: authority, OrgID: orgID, AccountInternalID: sql.NullInt64{Int64: e.account.ID, Valid: true}, SiteIds: sites, Search: search, CursorKind: int16(cursor.Kind), CursorID: cursor.ID, ResultLimit: size + 1})
		if err != nil {
			return nil, err
		}
		result := &pb.ListAccountTicketAssetsResponse{}
		if len(rows) > int(size) {
			rows = rows[:size]
			last := rows[len(rows)-1]
			result.NextPageToken, err = (models.TicketAssetCursor{Kind: models.TicketCategory(last.Kind), ID: last.ID, Fingerprint: query}).Encode()
			if err != nil {
				return nil, err
			}
		}
		for _, row := range rows {
			site := int64(0)
			if row.SiteID.Valid {
				site = row.SiteID.Int64
			}
			if !e.physicalRead(sqlc.ListHostedAccountMiningAssetsRow{SiteID: site}) {
				continue
			}
			asset := &maintenancepb.TicketAsset{Kind: maintenancepb.TicketCategory(row.Kind), Id: row.ID, Label: row.Label, Model: row.Model}
			if row.ModuleID.Valid {
				asset.ModuleId = &row.ModuleID.Int64
			}
			if row.InstallationID.Valid {
				asset.InstallationId = &row.InstallationID.Int64
			}
			if row.ContainerID.Valid {
				asset.ContainerId = &row.ContainerID.Int64
			}
			if row.Position.Valid {
				asset.Position = &row.Position.Int32
			}
			if row.SiteID.Valid {
				asset.SiteId = &row.SiteID.Int64
			}
			result.Assets = append(result.Assets, asset)
		}
		return result, nil
	})
}

// auditAccountTicket shares the account admission transaction with the ticket
// mutation. Only identifiers enter the audit record; conversation text remains
// subject to the ticket's account audience.
func auditAccountTicket(ctx context.Context, q sqlc.Querier, actorID int64, account Account, event string, ticketID int64, metadata map[string]any) error {
	actor, err := lockActor(ctx, q, account.OrganizationID, actorID)
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["ticket_id"] = ticketID
	return auditWithCategory(ctx, q, actor, account, "fleet_management", event, metadata)
}
