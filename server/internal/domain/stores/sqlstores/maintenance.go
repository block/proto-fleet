package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.MaintenanceStore = (*SQLMaintenanceStore)(nil)

// SQLMaintenanceStore persists organization-scoped repair ticket state through
// prepared sqlc queries and honors transaction-bound queries carried by ctx.
type SQLMaintenanceStore struct {
	SQLConnectionManager
}

func NewSQLMaintenanceStore(conn *sql.DB) *SQLMaintenanceStore {
	return &SQLMaintenanceStore{SQLConnectionManager: NewSQLConnectionManager(conn)}
}

func (s *SQLMaintenanceStore) LockRepairTicketCreateKey(ctx context.Context, orgID int64, idempotencyKey string) error {
	err := s.GetQueries(ctx).LockRepairTicketCreateKey(ctx, sqlc.LockRepairTicketCreateKeyParams{
		OrgID: orgID, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to lock repair ticket create key: %w", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) GetRepairTicketByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.RepairTicket, string, error) {
	row, err := s.GetQueries(ctx).GetRepairTicketIDByIdempotencyKey(ctx, sqlc.GetRepairTicketIDByIdempotencyKeyParams{
		OrgID: orgID, IdempotencyKey: sql.NullString{String: idempotencyKey, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", fleeterror.NewNotFoundError("repair ticket create key not found")
		}
		return nil, "", fleeterror.NewInternalErrorf("failed to get repair ticket by idempotency key: %w", err)
	}
	if !row.CreateRequestHash.Valid {
		return nil, "", fleeterror.NewInternalError("repair ticket create key is missing its request hash")
	}
	ticket, err := s.GetRepairTicket(ctx, orgID, row.ID)
	return ticket, row.CreateRequestHash.String, err
}

func (s *SQLMaintenanceStore) NextTicketNumber(ctx context.Context, orgID int64) (int64, error) {
	number, err := s.GetQueries(ctx).NextRepairTicketNumber(ctx, orgID)
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to allocate repair ticket number: %w", err)
	}
	return number, nil
}

func (s *SQLMaintenanceStore) CreateRepairTicket(ctx context.Context, params models.CreateParams, ticketNumber string) (*models.RepairTicket, error) {
	id, err := s.GetQueries(ctx).CreateRepairTicket(ctx, sqlc.CreateRepairTicketParams{
		OrgID: params.OrgID, TicketNumber: ticketNumber, Category: int16(params.Category),
		Urgent: params.Urgent, Component: params.Component, Diagnosis: ptrToNullString(params.Diagnosis),
		MinerIdentifier: ptrToNullString(params.MinerIdentifier), AlertID: ptrToNullString(params.AlertID),
		AssigneeUserID: ptrToNullInt64(params.AssigneeUserID), WarrantyStatus: int16(params.WarrantyStatus),
		SiteID: ptrToNullInt64(params.SiteID), BuildingID: ptrToNullInt64(params.BuildingID),
		Zone: ptrToNullString(params.Zone), RackID: ptrToNullInt64(params.RackID),
		RackLabel: ptrToNullString(params.RackLabel), GroupLabel: ptrToNullString(params.GroupLabel),
		Notes: ptrToNullString(params.Notes), DailyImpactUsd: numericFromFloat(params.DailyImpactUsd),
		IdempotencyKey:    sql.NullString{String: params.IdempotencyKey, Valid: true},
		CreateRequestHash: sql.NullString{String: params.CreateRequestHash, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fleeterror.NewAlreadyExistsErrorf("repair ticket %q already exists", ticketNumber)
		}
		return nil, maintenanceWriteError("create repair ticket", err)
	}
	return s.GetRepairTicket(ctx, params.OrgID, id)
}

func (s *SQLMaintenanceStore) GetRepairTicket(ctx context.Context, orgID, id int64) (*models.RepairTicket, error) {
	row, err := s.GetQueries(ctx).GetRepairTicket(ctx, sqlc.GetRepairTicketParams{ID: id, OrgID: orgID})
	if err != nil {
		return nil, maintenanceGetError(err, id)
	}
	ticket := ticketFromGetRow(row)
	return &ticket, nil
}

func (s *SQLMaintenanceStore) GetRepairTicketForUpdate(ctx context.Context, orgID, id int64) (*models.RepairTicket, error) {
	row, err := s.GetQueries(ctx).GetRepairTicketForUpdate(ctx, sqlc.GetRepairTicketForUpdateParams{ID: id, OrgID: orgID})
	if err != nil {
		return nil, maintenanceGetError(err, id)
	}
	ticket := ticketFromForUpdateRow(row)
	return &ticket, nil
}

func (s *SQLMaintenanceStore) ListRepairTickets(ctx context.Context, filter models.ListFilter) ([]models.RepairTicketSummary, error) {
	sortField, direction, cursor, err := normalizeCursor(filter.SortField, filter.SortDirection, filter.Cursor)
	if err != nil {
		return nil, err
	}
	rows, err := s.GetQueries(ctx).ListRepairTickets(ctx, sqlc.ListRepairTicketsParams{
		CursorValue: cursorValue(cursor), CursorID: cursorID(cursor), SortField: int16(sortField),
		SortDirection: int16(direction), LimitN: maintenanceLimit(filter.Limit), OrgID: filter.OrgID,
		FilterStatuses: maintenanceNilIfEmpty(filter.Statuses), FilterCategories: maintenanceNilIfEmpty(filter.Categories),
		FilterSiteIds: maintenanceNilIfEmpty(filter.SiteIDs), ExcludedSiteIds: maintenanceNilIfEmpty(filter.ExcludedSiteIDs),
		FilterBuildingIds: maintenanceNilIfEmpty(filter.BuildingIDs),
		FilterRackIds:     maintenanceNilIfEmpty(filter.RackIDs), FilterGroupLabels: maintenanceNilIfEmpty(filter.GroupLabels),
		FilterAssigneeUserID: ptrToNullInt64(filter.AssigneeUserID), FilterUrgentOnly: filter.UrgentOnly,
		FilterExcludeCompleted: filter.ExcludeCompleted, FilterOverdueOnly: filter.OverdueOnly,
		SearchQuery: filter.SearchQuery,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list repair tickets: %w", err)
	}
	out := make([]models.RepairTicketSummary, 0, len(rows))
	for _, row := range rows {
		ticket := ticketFromListRow(row)
		out = append(out, models.RepairTicketSummary{
			RepairTicket: ticket, CommentCount: row.CommentCount, PartsCount: row.PartsCount,
			Cursor: models.TicketCursor{SortField: sortField, SortDirection: direction, Value: sqlText(row.SortValue), ID: row.ID},
		})
	}
	return out, nil
}

func (s *SQLMaintenanceStore) CountRepairTickets(ctx context.Context, filter models.ListFilter) (int32, error) {
	count, err := s.GetQueries(ctx).CountRepairTickets(ctx, countRepairParams(filter))
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count repair tickets: %w", err)
	}
	return count, nil
}

func (s *SQLMaintenanceStore) UpdateRepairTicket(ctx context.Context, params models.UpdateParams) (*models.RepairTicket, error) {
	id, err := s.GetQueries(ctx).UpdateRepairTicket(ctx, sqlc.UpdateRepairTicketParams{
		Status: ticketStatusToNull(params.Status), Urgent: boolToNull(params.Urgent), ClearAssignee: params.ClearAssignee,
		AssigneeUserID: ptrToNullInt64(params.AssigneeUserID), Component: ptrToNullString(params.Component),
		Diagnosis: ptrToNullString(params.Diagnosis), WarrantyStatus: warrantyToNull(params.WarrantyStatus),
		Resolution: resolutionToNull(params.Resolution), RepairLocation: repairLocationToNull(params.RepairLocation),
		Notes: ptrToNullString(params.Notes), RmaVendor: ptrToNullString(params.RMAVendor),
		RmaTracking: ptrToNullString(params.RMATracking), ClearRmaEta: params.ClearRMAEta,
		RmaEta: ptrToNullTime(params.RMAEta), ID: params.ID, OrgID: params.OrgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("repair ticket %d not found", params.ID)
		}
		return nil, maintenanceWriteError("update repair ticket", err)
	}
	return s.GetRepairTicket(ctx, params.OrgID, id)
}

func (s *SQLMaintenanceStore) BulkUpdateTicketStatus(ctx context.Context, orgID int64, ticketIDs []int64, newStatus int16) (int64, error) {
	rows, err := s.GetQueries(ctx).BulkUpdateTicketStatus(ctx, sqlc.BulkUpdateTicketStatusParams{Status: newStatus, OrgID: orgID, TicketIds: ticketIDs})
	if err != nil {
		return 0, maintenanceWriteError("bulk update ticket status", err)
	}
	return rows, nil
}

func (s *SQLMaintenanceStore) BulkAssignTickets(ctx context.Context, orgID int64, ticketIDs []int64, assigneeUserID *int64) (int64, error) {
	rows, err := s.GetQueries(ctx).BulkAssignTickets(ctx, sqlc.BulkAssignTicketsParams{AssigneeUserID: ptrToNullInt64(assigneeUserID), OrgID: orgID, TicketIds: ticketIDs})
	if err != nil {
		return 0, maintenanceWriteError("bulk assign tickets", err)
	}
	return rows, nil
}

func (s *SQLMaintenanceStore) BulkMarkUrgent(ctx context.Context, orgID int64, ticketIDs []int64) (int64, error) {
	rows, err := s.GetQueries(ctx).BulkMarkTicketsUrgent(ctx, sqlc.BulkMarkTicketsUrgentParams{OrgID: orgID, TicketIds: ticketIDs})
	if err != nil {
		return 0, maintenanceWriteError("bulk mark tickets urgent", err)
	}
	return rows, nil
}

func (s *SQLMaintenanceStore) BulkCloseTickets(ctx context.Context, orgID int64, ticketIDs []int64, resolution, repairLocation int16, notes *string) (int64, error) {
	rows, err := s.GetQueries(ctx).BulkCloseTickets(ctx, sqlc.BulkCloseTicketsParams{
		Resolution: resolution, RepairLocation: repairLocation, Notes: ptrToNullString(notes), OrgID: orgID, TicketIds: ticketIDs,
	})
	if err != nil {
		return 0, maintenanceWriteError("bulk close tickets", err)
	}
	return rows, nil
}

func (s *SQLMaintenanceStore) GetTicketStats(ctx context.Context, filter models.ListFilter) (*models.TicketStats, error) {
	row, err := s.GetQueries(ctx).GetFilteredTicketStats(ctx, statsParams(filter))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get repair ticket stats: %w", err)
	}
	return &models.TicketStats{
		CountByStatus: map[models.TicketStatus]int32{
			models.TicketStatusOpen: row.OpenCount, models.TicketStatusInProgress: row.InProgressCount,
			models.TicketStatusOnHold: row.OnHoldCount, models.TicketStatusSentToVendor: row.SentToVendorCount,
			models.TicketStatusCompleted: row.CompletedCount,
		},
		Unassigned: row.UnassignedCount, Urgent: row.UrgentCount, Overdue: row.OverdueCount, AvgAgeHours: row.AvgAgeHours,
	}, nil
}

func (s *SQLMaintenanceStore) ListCompletedTicketAssignees(ctx context.Context, filter models.CompletedFilter) ([]models.Assignee, error) {
	rows, err := s.GetQueries(ctx).ListCompletedTicketAssignees(ctx, sqlc.ListCompletedTicketAssigneesParams{
		OrgID: filter.OrgID, FilterSiteIds: maintenanceNilIfEmpty(filter.SiteIDs),
		ExcludedSiteIds: maintenanceNilIfEmpty(filter.ExcludedSiteIDs),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list completed ticket assignees: %w", err)
	}
	assignees := make([]models.Assignee, 0, len(rows))
	for _, row := range rows {
		assignees = append(assignees, models.Assignee{UserID: row.UserID, Username: row.Username, RoleName: row.RoleName})
	}
	return assignees, nil
}

func (s *SQLMaintenanceStore) ListCompletedTickets(ctx context.Context, filter models.CompletedFilter) ([]models.RepairTicketSummary, error) {
	sortField, direction, cursor, err := normalizeCursor(filter.SortField, filter.SortDirection, filter.Cursor)
	if err != nil {
		return nil, err
	}
	rows, err := s.GetQueries(ctx).ListCompletedTickets(ctx, sqlc.ListCompletedTicketsParams{
		CursorValue: cursorValue(cursor), CursorID: cursorID(cursor), SortDirection: int16(direction),
		LimitN: maintenanceLimit(filter.Limit), SortField: int16(sortField), OrgID: filter.OrgID,
		ComponentFilter: ptrToNullString(filter.Component), AssigneeFilter: ptrToNullInt64(filter.AssigneeUserID),
		FilterSiteIds: maintenanceNilIfEmpty(filter.SiteIDs), ExcludedSiteIds: maintenanceNilIfEmpty(filter.ExcludedSiteIDs),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list completed tickets: %w", err)
	}
	out := make([]models.RepairTicketSummary, 0, len(rows))
	for _, row := range rows {
		ticket := ticketFromCompletedRow(row)
		out = append(out, models.RepairTicketSummary{RepairTicket: ticket, CommentCount: row.CommentCount, PartsCount: row.PartsCount,
			Cursor: models.TicketCursor{SortField: sortField, SortDirection: direction, Value: sqlText(row.SortValue), ID: row.ID}})
	}
	return out, nil
}

func (s *SQLMaintenanceStore) CountCompletedTickets(ctx context.Context, filter models.CompletedFilter) (int32, error) {
	count, err := s.GetQueries(ctx).CountCompletedTickets(ctx, sqlc.CountCompletedTicketsParams{
		OrgID: filter.OrgID, ComponentFilter: ptrToNullString(filter.Component),
		AssigneeFilter: ptrToNullInt64(filter.AssigneeUserID), FilterSiteIds: maintenanceNilIfEmpty(filter.SiteIDs),
		ExcludedSiteIds: maintenanceNilIfEmpty(filter.ExcludedSiteIDs),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count completed tickets: %w", err)
	}
	return count, nil
}

func (s *SQLMaintenanceStore) LockTicketCommentCreateKey(ctx context.Context, orgID int64, idempotencyKey string) error {
	if err := s.GetQueries(ctx).LockRepairTicketCommentCreateKey(ctx, sqlc.LockRepairTicketCommentCreateKeyParams{
		OrgID: orgID, IdempotencyKey: idempotencyKey,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to lock repair ticket comment key: %w", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) GetTicketCommentByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.TicketComment, string, error) {
	row, err := s.GetQueries(ctx).GetRepairTicketCommentByIdempotencyKey(ctx, sqlc.GetRepairTicketCommentByIdempotencyKeyParams{
		OrgID: orgID, IdempotencyKey: sql.NullString{String: idempotencyKey, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", fleeterror.NewNotFoundError("repair ticket comment idempotency key not found")
		}
		return nil, "", fleeterror.NewInternalErrorf("failed to get repair ticket comment by idempotency key: %w", err)
	}
	comment := models.TicketComment{ID: row.ID, OrgID: row.OrgID, TicketID: row.TicketID, UserID: row.UserID, UserName: row.UserName, Text: row.Text, CreatedAt: row.CreatedAt, DeletedAt: timePtr(row.DeletedAt)}
	return &comment, row.CreateRequestHash.String, nil
}

func (s *SQLMaintenanceStore) CreateTicketComment(ctx context.Context, orgID, ticketID, userID int64, text, idempotencyKey, requestHash string) (*models.TicketComment, error) {
	row, err := s.GetQueries(ctx).CreateRepairTicketComment(ctx, sqlc.CreateRepairTicketCommentParams{
		OrgID: orgID, TicketID: ticketID, UserID: userID, Text: text,
		IdempotencyKey:    sql.NullString{String: idempotencyKey, Valid: true},
		CreateRequestHash: sql.NullString{String: requestHash, Valid: true},
	})
	if err != nil {
		return nil, maintenanceWriteError("create repair ticket comment", err)
	}
	comment := models.TicketComment{ID: row.ID, OrgID: row.OrgID, TicketID: row.TicketID, UserID: row.UserID, UserName: row.UserName, Text: row.Text, CreatedAt: row.CreatedAt, DeletedAt: timePtr(row.DeletedAt)}
	return &comment, nil
}
func (s *SQLMaintenanceStore) ListTicketComments(ctx context.Context, orgID, ticketID int64) ([]models.TicketComment, error) {
	rows, err := s.GetQueries(ctx).ListRepairTicketComments(ctx, sqlc.ListRepairTicketCommentsParams{OrgID: orgID, TicketID: ticketID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list repair ticket comments: %w", err)
	}
	out := make([]models.TicketComment, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.TicketComment{ID: row.ID, OrgID: row.OrgID, TicketID: row.TicketID, UserID: row.UserID, UserName: row.UserName, Text: row.Text, CreatedAt: row.CreatedAt, DeletedAt: timePtr(row.DeletedAt)})
	}
	return out, nil
}
func (s *SQLMaintenanceStore) GetTicketCommentSiteForUpdate(ctx context.Context, orgID, callerUserID, id int64) (*int64, error) {
	siteID, err := s.GetQueries(ctx).GetRepairTicketCommentSiteForUpdate(ctx, sqlc.GetRepairTicketCommentSiteForUpdateParams{
		ID: id, OrgID: orgID, CallerUserID: callerUserID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("comment %d not found", id)
		}
		return nil, fleeterror.NewInternalErrorf("failed to lock repair ticket comment: %w", err)
	}
	return nullInt64ToPtr(siteID), nil
}

func (s *SQLMaintenanceStore) SoftDeleteTicketComment(ctx context.Context, orgID, callerUserID, id int64) (int64, error) {
	rows, err := s.GetQueries(ctx).SoftDeleteRepairTicketCommentByAuthor(ctx, sqlc.SoftDeleteRepairTicketCommentByAuthorParams{ID: id, OrgID: orgID, CallerUserID: callerUserID})
	if err != nil {
		return 0, maintenanceWriteError("delete repair ticket comment", err)
	}
	return rows, nil
}

func (s *SQLMaintenanceStore) SetTicketParts(ctx context.Context, orgID, ticketID int64) error {
	if err := s.GetQueries(ctx).DeleteActiveRepairTicketParts(ctx, sqlc.DeleteActiveRepairTicketPartsParams{OrgID: orgID, TicketID: ticketID}); err != nil {
		return maintenanceWriteError("replace repair ticket parts", err)
	}
	return nil
}
func (s *SQLMaintenanceStore) InsertTicketPart(ctx context.Context, orgID, ticketID, inventoryPartID int64, partName string, quantity int32) error {
	err := s.GetQueries(ctx).InsertRepairTicketPart(ctx, sqlc.InsertRepairTicketPartParams{OrgID: orgID, TicketID: ticketID, InventoryPartID: inventoryPartID, PartName: partName, Quantity: quantity})
	if err != nil {
		return maintenanceWriteError("insert repair ticket part", err)
	}
	return nil
}
func (s *SQLMaintenanceStore) MarkTicketPartsConsumed(ctx context.Context, orgID, ticketID int64) error {
	if err := s.GetQueries(ctx).MarkRepairTicketPartsConsumed(ctx, sqlc.MarkRepairTicketPartsConsumedParams{OrgID: orgID, TicketID: ticketID}); err != nil {
		return maintenanceWriteError("consume repair ticket parts", err)
	}
	return nil
}
func (s *SQLMaintenanceStore) ListTicketParts(ctx context.Context, orgID, ticketID int64) ([]models.PartUsage, error) {
	rows, err := s.GetQueries(ctx).ListRepairTicketParts(ctx, sqlc.ListRepairTicketPartsParams{OrgID: orgID, TicketID: ticketID})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list repair ticket parts: %w", err)
	}
	out := make([]models.PartUsage, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.PartUsage{InventoryPartID: row.InventoryPartID, PartName: row.PartName, Quantity: row.Quantity, ConsumedAt: timePtr(row.ConsumedAt)})
	}
	return out, nil
}

func ticketFromGetRow(row sqlc.GetRepairTicketRow) models.RepairTicket {
	return ticket(row.ID, row.OrgID, row.TicketNumber, row.Category, row.Status, row.Urgent, row.Component, row.Diagnosis, row.MinerIdentifier, row.AlertID, row.AssigneeUserID, row.AssigneeName, row.WarrantyStatus, row.Resolution, row.RepairLocation, row.Notes, row.DailyImpactUsd, row.RmaVendor, row.RmaTracking, row.RmaEta, row.SiteID, row.SiteName, row.BuildingID, row.BuildingName, row.Zone, row.RackID, row.RackLabel, row.GroupLabel, row.CompletedAt, row.CreatedAt, row.UpdatedAt, row.DeletedAt)
}
func ticketFromForUpdateRow(row sqlc.GetRepairTicketForUpdateRow) models.RepairTicket {
	return ticket(row.ID, row.OrgID, row.TicketNumber, row.Category, row.Status, row.Urgent, row.Component, row.Diagnosis, row.MinerIdentifier, row.AlertID, row.AssigneeUserID, row.AssigneeName, row.WarrantyStatus, row.Resolution, row.RepairLocation, row.Notes, row.DailyImpactUsd, row.RmaVendor, row.RmaTracking, row.RmaEta, row.SiteID, row.SiteName, row.BuildingID, row.BuildingName, row.Zone, row.RackID, row.RackLabel, row.GroupLabel, row.CompletedAt, row.CreatedAt, row.UpdatedAt, row.DeletedAt)
}
func ticketFromListRow(row sqlc.ListRepairTicketsRow) models.RepairTicket {
	return ticket(row.ID, row.OrgID, row.TicketNumber, row.Category, row.Status, row.Urgent, row.Component, row.Diagnosis, row.MinerIdentifier, row.AlertID, row.AssigneeUserID, row.AssigneeName, row.WarrantyStatus, row.Resolution, row.RepairLocation, row.Notes, row.DailyImpactUsd, row.RmaVendor, row.RmaTracking, row.RmaEta, row.SiteID, row.SiteName, row.BuildingID, row.BuildingName, row.Zone, row.RackID, row.RackLabel, row.GroupLabel, row.CompletedAt, row.CreatedAt, row.UpdatedAt, row.DeletedAt)
}
func ticketFromCompletedRow(row sqlc.ListCompletedTicketsRow) models.RepairTicket {
	return ticket(row.ID, row.OrgID, row.TicketNumber, row.Category, row.Status, row.Urgent, row.Component, row.Diagnosis, row.MinerIdentifier, row.AlertID, row.AssigneeUserID, row.AssigneeName, row.WarrantyStatus, row.Resolution, row.RepairLocation, row.Notes, row.DailyImpactUsd, row.RmaVendor, row.RmaTracking, row.RmaEta, row.SiteID, row.SiteName, row.BuildingID, row.BuildingName, row.Zone, row.RackID, row.RackLabel, row.GroupLabel, row.CompletedAt, row.CreatedAt, row.UpdatedAt, row.DeletedAt)
}

func ticket(id, orgID int64, number string, category, status int16, urgent bool, component string, diagnosis, miner, alert sql.NullString, assignee sql.NullInt64, assigneeName string, warranty, resolution, location int16, notes, impact, vendor, tracking sql.NullString, eta sql.NullTime, siteID sql.NullInt64, siteName string, buildingID sql.NullInt64, buildingName string, zone sql.NullString, rackID sql.NullInt64, rackLabel, groupLabel sql.NullString, completed sql.NullTime, created, updated time.Time, deleted sql.NullTime) models.RepairTicket {
	return models.RepairTicket{ID: id, OrgID: orgID, TicketNumber: number, Category: models.TicketCategory(category), Status: models.TicketStatus(status), Urgent: urgent, Component: component, Diagnosis: stringPtr(diagnosis), MinerIdentifier: stringPtr(miner), AlertID: stringPtr(alert), AssigneeUserID: nullInt64ToPtr(assignee), AssigneeName: assigneeName, WarrantyStatus: models.WarrantyStatus(warranty), Resolution: models.TicketResolution(resolution), RepairLocation: models.RepairLocation(location), Notes: stringPtr(notes), DailyImpactUsd: floatFromNumeric(impact), RMAVendor: stringPtr(vendor), RMATracking: stringPtr(tracking), RMAEta: timePtr(eta), SiteID: nullInt64ToPtr(siteID), SiteName: siteName, BuildingID: nullInt64ToPtr(buildingID), BuildingName: buildingName, Zone: stringPtr(zone), RackID: nullInt64ToPtr(rackID), RackLabel: stringPtr(rackLabel), GroupLabel: stringPtr(groupLabel), CompletedAt: timePtr(completed), CreatedAt: created, UpdatedAt: updated, DeletedAt: timePtr(deleted)}
}

func normalizeCursor(field models.TicketSortField, direction models.SortDirection, cursor *models.TicketCursor) (models.TicketSortField, models.SortDirection, *models.TicketCursor, error) {
	if field == models.TicketSortFieldUnspecified {
		field = models.TicketSortFieldCreatedAt
	}
	if direction == models.SortDirectionUnspecified {
		direction = models.SortDirectionDescending
	}
	if field < models.TicketSortFieldComponent || field > models.TicketSortFieldCompletedAt || direction < models.SortDirectionAscending || direction > models.SortDirectionDescending {
		return 0, 0, nil, fleeterror.NewInvalidArgumentError("invalid ticket sort")
	}
	if cursor != nil && (cursor.SortField != field || cursor.SortDirection != direction || cursor.ID <= 0 || cursor.Value == "") {
		return 0, 0, nil, fleeterror.NewInvalidArgumentError("page token does not match ticket sort")
	}
	return field, direction, cursor, nil
}
func cursorValue(cursor *models.TicketCursor) sql.NullString {
	if cursor == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: cursor.Value, Valid: true}
}
func cursorID(cursor *models.TicketCursor) sql.NullInt64 {
	if cursor == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: cursor.ID, Valid: true}
}
func maintenanceLimit(limit int32) int32 {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}
func maintenanceNilIfEmpty[T any](values []T) []T {
	if len(values) == 0 {
		return nil
	}
	return values
}
func sqlText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}
func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}
func boolToNull(value *bool) sql.NullBool {
	if value == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *value, Valid: true}
}
func ticketStatusToNull(value *models.TicketStatus) sql.NullInt16 {
	if value == nil {
		return sql.NullInt16{}
	}
	return sql.NullInt16{Int16: int16(*value), Valid: true}
}
func warrantyToNull(value *models.WarrantyStatus) sql.NullInt16 {
	if value == nil {
		return sql.NullInt16{}
	}
	return sql.NullInt16{Int16: int16(*value), Valid: true}
}
func resolutionToNull(value *models.TicketResolution) sql.NullInt16 {
	if value == nil {
		return sql.NullInt16{}
	}
	return sql.NullInt16{Int16: int16(*value), Valid: true}
}
func repairLocationToNull(value *models.RepairLocation) sql.NullInt16 {
	if value == nil {
		return sql.NullInt16{}
	}
	return sql.NullInt16{Int16: int16(*value), Valid: true}
}
func maintenanceGetError(err error, id int64) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundErrorf("repair ticket %d not found", id)
	}
	return fleeterror.NewInternalErrorf("failed to get repair ticket: %w", err)
}
func maintenanceWriteError(action string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundError("referenced maintenance resource not found")
	}
	if isForeignKeyViolationOn(err, "fk_repair_ticket_site") || isForeignKeyViolationOn(err, "fk_repair_ticket_building") || isForeignKeyViolationOn(err, "fk_repair_ticket_assignee") || isForeignKeyViolationOn(err, "fk_ticket_comment_ticket") || isForeignKeyViolationOn(err, "fk_ticket_part_ticket") || isForeignKeyViolationOn(err, "fk_ticket_part_inventory") {
		return fleeterror.NewNotFoundError("referenced maintenance resource not found")
	}
	return fleeterror.NewInternalErrorf("failed to %s: %w", action, err)
}
func countRepairParams(f models.ListFilter) sqlc.CountRepairTicketsParams {
	return sqlc.CountRepairTicketsParams{OrgID: f.OrgID, FilterStatuses: maintenanceNilIfEmpty(f.Statuses), FilterCategories: maintenanceNilIfEmpty(f.Categories), FilterSiteIds: maintenanceNilIfEmpty(f.SiteIDs), ExcludedSiteIds: maintenanceNilIfEmpty(f.ExcludedSiteIDs), FilterBuildingIds: maintenanceNilIfEmpty(f.BuildingIDs), FilterRackIds: maintenanceNilIfEmpty(f.RackIDs), FilterGroupLabels: maintenanceNilIfEmpty(f.GroupLabels), FilterAssigneeUserID: ptrToNullInt64(f.AssigneeUserID), FilterUrgentOnly: f.UrgentOnly, FilterExcludeCompleted: f.ExcludeCompleted, FilterOverdueOnly: f.OverdueOnly, SearchQuery: f.SearchQuery}
}
func statsParams(f models.ListFilter) sqlc.GetFilteredTicketStatsParams {
	return sqlc.GetFilteredTicketStatsParams{OrgID: f.OrgID, FilterStatuses: maintenanceNilIfEmpty(f.Statuses), FilterCategories: maintenanceNilIfEmpty(f.Categories), FilterSiteIds: maintenanceNilIfEmpty(f.SiteIDs), ExcludedSiteIds: maintenanceNilIfEmpty(f.ExcludedSiteIDs), FilterBuildingIds: maintenanceNilIfEmpty(f.BuildingIDs), FilterRackIds: maintenanceNilIfEmpty(f.RackIDs), FilterGroupLabels: maintenanceNilIfEmpty(f.GroupLabels), FilterAssigneeUserID: ptrToNullInt64(f.AssigneeUserID), FilterUrgentOnly: f.UrgentOnly, FilterExcludeCompleted: f.ExcludeCompleted, FilterOverdueOnly: f.OverdueOnly, SearchQuery: f.SearchQuery}
}
