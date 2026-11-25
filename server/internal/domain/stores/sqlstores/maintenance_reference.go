package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/maintenance/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

var _ interfaces.MaintenanceReferenceStore = (*SQLMaintenanceStore)(nil)

// NewSQLMaintenanceReferenceStore returns the same transaction-aware store
// used for repair tickets; it is retained as an explicit wiring constructor.
func NewSQLMaintenanceReferenceStore(conn *sql.DB) *SQLMaintenanceStore {
	return NewSQLMaintenanceStore(conn)
}

func (s *SQLMaintenanceStore) LockSiteForTicket(ctx context.Context, orgID, siteID int64) error {
	_, err := s.GetQueries(ctx).LockMaintenanceSiteForTicket(ctx, sqlc.LockMaintenanceSiteForTicketParams{
		OrgID: orgID, SiteID: siteID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("site %d not found", siteID)
		}
		return fleeterror.NewInternalErrorf("failed to lock maintenance site: %w", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) LockBuildingForTicket(ctx context.Context, orgID, buildingID int64) error {
	_, err := s.GetQueries(ctx).LockMaintenanceBuildingForTicket(ctx, sqlc.LockMaintenanceBuildingForTicketParams{
		OrgID: orgID, BuildingID: buildingID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
		}
		return fleeterror.NewInternalErrorf("failed to lock maintenance building: %w", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) LockRackForTicket(ctx context.Context, orgID, rackID int64) error {
	_, err := s.GetQueries(ctx).LockMaintenanceRackForTicket(ctx, sqlc.LockMaintenanceRackForTicketParams{
		OrgID: orgID, RackID: rackID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("rack %d not found", rackID)
		}
		return fleeterror.NewInternalErrorf("failed to lock maintenance rack: %w", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) LockMinerForTicket(ctx context.Context, orgID int64, minerIdentifier string) error {
	_, err := s.GetQueries(ctx).LockMaintenanceMinerForTicket(ctx, sqlc.LockMaintenanceMinerForTicketParams{
		OrgID: orgID, MinerIdentifier: minerIdentifier,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("miner %q not found", minerIdentifier)
		}
		return fleeterror.NewInternalErrorf("failed to lock maintenance miner: %w", err)
	}
	return nil
}

func (s *SQLMaintenanceStore) ResolveMinerContextByID(ctx context.Context, orgID, minerID int64) (*models.AssetContext, error) {
	identifier, err := s.GetQueries(ctx).ResolveMaintenanceMinerIdentifierByID(ctx, sqlc.ResolveMaintenanceMinerIdentifierByIDParams{OrgID: orgID, ID: minerID})
	if err != nil {
		return nil, maintenanceWriteError("resolve ticket miner identifier", err)
	}
	return s.ResolveMinerContext(ctx, orgID, identifier)
}

func (s *SQLMaintenanceStore) ResolveMinerContext(ctx context.Context, orgID int64, minerIdentifier string) (*models.AssetContext, error) {
	row, err := s.GetQueries(ctx).ResolveMaintenanceMinerContext(ctx, sqlc.ResolveMaintenanceMinerContextParams{
		OrgID: orgID, MinerIdentifier: minerIdentifier,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("miner %q not found", minerIdentifier)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve maintenance miner: %w", err)
	}
	return &models.AssetContext{
		MinerIdentifier: row.MinerIdentifier,
		SiteID:          nullInt64ToPtr(row.SiteID),
		SiteName:        row.SiteName,
		BuildingID:      nullInt64ToPtr(row.BuildingID),
		BuildingName:    row.BuildingName,
		Zone:            nullStringToPtr(row.Zone),
		RackID:          nullInt64ToPtr(row.RackID),
		RackLabel:       nullStringToPtr(row.RackLabel),
		GroupLabel:      nonEmptyStringPtr(row.GroupLabel),
	}, nil
}

func (s *SQLMaintenanceStore) ResolveLocationContext(ctx context.Context, orgID int64, siteID, buildingID *int64) (*models.AssetContext, error) {
	row, err := s.GetQueries(ctx).ResolveMaintenanceLocationContext(ctx, sqlc.ResolveMaintenanceLocationContextParams{
		OrgID: orgID, SiteID: ptrToNullInt64(siteID), BuildingID: ptrToNullInt64(buildingID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("maintenance site or building not found")
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve maintenance location: %w", err)
	}
	return &models.AssetContext{
		SiteID: nullInt64ToPtr(row.SiteID), SiteName: row.SiteName,
		BuildingID: nullInt64ToPtr(row.BuildingID), BuildingName: row.BuildingName,
	}, nil
}

func (s *SQLMaintenanceStore) ResolveAssignee(ctx context.Context, orgID, userID int64) (*models.Assignee, error) {
	row, err := s.GetQueries(ctx).ResolveMaintenanceAssignee(ctx, sqlc.ResolveMaintenanceAssigneeParams{OrgID: orgID, UserID: userID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("maintenance assignee %d not found", userID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to resolve maintenance assignee: %w", err)
	}
	return &models.Assignee{UserID: row.UserID, Username: row.Username, RoleName: row.RoleName}, nil
}

func (s *SQLMaintenanceStore) ListAssignees(ctx context.Context, orgID int64) ([]models.Assignee, error) {
	rows, err := s.GetQueries(ctx).ListMaintenanceAssignees(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list maintenance assignees: %w", err)
	}
	assignees := make([]models.Assignee, 0, len(rows))
	for _, row := range rows {
		assignees = append(assignees, models.Assignee{UserID: row.UserID, Username: row.Username, RoleName: row.RoleName})
	}
	return assignees, nil
}

func nonEmptyStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *SQLMaintenanceStore) ResolveTicketAsset(ctx context.Context, orgID int64, kind models.TicketCategory, id int64) (*models.TicketAsset, error) {
	row, err := s.GetQueries(ctx).ResolveRepairTicketAsset(ctx, sqlc.ResolveRepairTicketAssetParams{OrgID: orgID, Kind: int16(kind), ID: id})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("physical ticket asset not found")
		}
		return nil, fleeterror.NewInternalErrorf("resolve physical ticket asset: %w", err)
	}
	asset := ticketAssetFromOption(row)
	return &asset, nil
}

func (s *SQLMaintenanceStore) LockModuleModelSite(ctx context.Context, orgID, moduleID int64) (*int64, error) {
	asset, err := s.ResolveTicketAsset(ctx, orgID, models.TicketCategoryModule, moduleID)
	if err != nil {
		return nil, err
	}
	if asset.ContainerID == nil {
		return nil, fleeterror.NewNotFoundError("physical module container not found")
	}
	// Lock the device before inventory, then recheck the module's placement.
	if _, err := s.GetQueries(ctx).LockMaintenanceModelDevice(ctx, sqlc.LockMaintenanceModelDeviceParams{OrgID: orgID, DeviceID: *asset.ContainerID}); err != nil {
		return nil, maintenanceWriteError("lock module container", err)
	}
	site, err := s.GetQueries(ctx).LockMaintenanceModuleModelSite(ctx, sqlc.LockMaintenanceModuleModelSiteParams{OrgID: orgID, DeviceID: *asset.ContainerID, ModuleID: moduleID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewFailedPreconditionError("module placement changed; refresh before updating its model")
		}
		return nil, maintenanceWriteError("lock module model context", err)
	}
	return nullInt64ToPtr(site), nil
}
func (s *SQLMaintenanceStore) ListTicketAssetOptions(ctx context.Context, p models.TicketAssetOptionsParams) (*models.TicketAssetPage, error) {
	query := models.TicketAssetQuery{Scope: "options", OrgID: p.OrgID, SiteIDs: p.SiteIDs, ExcludedSiteIDs: p.ExcludedSiteIDs, Kind: p.Kind, Search: p.Search, IncludeUnassigned: p.IncludeUnassigned}.Fingerprint()
	size, cursor, err := models.ParseTicketAssetPagination(p.PageSize, p.PageToken, query)
	if err != nil {
		return nil, err
	}
	rows, err := s.GetQueries(ctx).ListRepairTicketAssetOptions(ctx, sqlc.ListRepairTicketAssetOptionsParams{HostAuthority: json.RawMessage("[]"), OrgID: p.OrgID, SiteIds: nilIfEmpty(p.SiteIDs), IncludeUnassigned: p.IncludeUnassigned, ExcludedSiteIds: nilIfEmpty(p.ExcludedSiteIDs), Kind: sql.NullInt16{Int16: int16(p.Kind), Valid: p.Kind != models.TicketCategoryUnspecified}, Search: p.Search, CursorKind: int16(cursor.Kind), CursorID: cursor.ID, ResultLimit: size + 1})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list physical ticket assets: %w", err)
	}
	out := make([]models.TicketAsset, 0, len(rows))
	for _, row := range rows {
		out = append(out, ticketAssetFromOption(row))
	}
	return models.TicketAssetPageFromRows(out, size, query)
}
func ticketAssetFromOption(row sqlc.RepairTicketAssetOption) models.TicketAsset {
	var position *int32
	if row.Position.Valid {
		position = &row.Position.Int32
	}
	return models.TicketAsset{Kind: models.TicketCategory(row.Kind), ID: row.ID, Label: row.Label, Model: row.Model, SiteID: nullInt64ToPtr(row.SiteID), ModuleID: nullInt64ToPtr(row.ModuleID), InstallationID: nullInt64ToPtr(row.InstallationID), ContainerID: nullInt64ToPtr(row.ContainerID), Position: position}
}

func (s *SQLMaintenanceStore) ListTicketBoardOptions(ctx context.Context, p models.TicketAssetOptionsParams) (*models.TicketAssetPage, error) {
	query := models.TicketAssetQuery{Scope: "board-options", OrgID: p.OrgID, TicketID: p.TicketID, Search: p.Search}.Fingerprint()
	size, cursor, err := models.ParseTicketAssetPagination(p.PageSize, p.PageToken, query)
	if err != nil {
		return nil, err
	}
	if p.PageToken != "" && cursor.Kind != models.TicketCategoryHashboard {
		return nil, fleeterror.NewInvalidArgumentError("invalid board page token")
	}
	payloads, err := s.GetQueries(ctx).ListRepairTicketBoardOptions(ctx, sqlc.ListRepairTicketBoardOptionsParams{OrgID: p.OrgID, TicketID: p.TicketID, Search: p.Search, CursorID: cursor.ID, ResultLimit: size + 1})
	if err != nil {
		return nil, err
	}
	result := make([]models.TicketAsset, 0, len(payloads))
	for _, payload := range payloads {
		var asset models.TicketAsset
		if err = json.Unmarshal(payload, &asset); err != nil {
			return nil, fleeterror.NewInternalErrorf("decode historical board option: %w", err)
		}
		result = append(result, asset)
	}
	return models.TicketAssetPageFromRows(result, size, query)
}

// LockTicketAssetPlacements follows rack → device → module → installation order, with
// IDs sorted at each level so overlapping selections acquire locks consistently.
func (s *SQLMaintenanceStore) LockTicketAssetPlacements(ctx context.Context, orgID int64, selections []models.TicketAssetSelection) error {
	racks := make(map[int64]struct{})
	miners := make(map[int64]*models.AssetContext)
	devices := make(map[int64]struct{})
	modules := make(map[int64]int64)
	installations := make(map[int64]int64)
	for _, selection := range selections {
		asset, err := s.ResolveTicketAsset(ctx, orgID, selection.Kind, selection.ID)
		if err != nil {
			return err
		}
		if selection.Kind == models.TicketCategoryMiner {
			placement, err := s.ResolveMinerContextByID(ctx, orgID, asset.ID)
			if err != nil {
				return err
			}
			miners[asset.ID] = placement
			if placement.RackID != nil {
				racks[*placement.RackID] = struct{}{}
			}
			devices[asset.ID] = struct{}{}
			continue
		}
		if asset.ContainerID == nil || asset.ModuleID == nil {
			return fleeterror.NewFailedPreconditionError("ticket asset placement is unavailable; refresh before creating the ticket")
		}
		devices[*asset.ContainerID] = struct{}{}
		modules[*asset.ModuleID] = *asset.ContainerID
		if selection.Kind == models.TicketCategoryHashboard {
			if asset.InstallationID == nil {
				return fleeterror.NewFailedPreconditionError("hashboard installation is unavailable; refresh before creating the ticket")
			}
			installations[*asset.InstallationID] = *asset.ModuleID
		}
	}
	rackIDs := make([]int64, 0, len(racks))
	for id := range racks {
		rackIDs = append(rackIDs, id)
	}
	sort.Slice(rackIDs, func(i, j int) bool { return rackIDs[i] < rackIDs[j] })
	for _, id := range rackIDs {
		if err := s.LockRackForTicket(ctx, orgID, id); err != nil {
			return err
		}
	}
	deviceIDs := make([]int64, 0, len(devices))
	for id := range devices {
		deviceIDs = append(deviceIDs, id)
	}
	sort.Slice(deviceIDs, func(i, j int) bool { return deviceIDs[i] < deviceIDs[j] })
	for _, id := range deviceIDs {
		if _, err := s.GetQueries(ctx).LockMaintenanceModelDevice(ctx, sqlc.LockMaintenanceModelDeviceParams{OrgID: orgID, DeviceID: id}); err != nil {
			return maintenanceWriteError("lock ticket device placement", err)
		}
	}
	for id, initial := range miners {
		current, err := s.ResolveMinerContextByID(ctx, orgID, id)
		if err != nil {
			return err
		}
		if (initial.RackID == nil) != (current.RackID == nil) ||
			(initial.RackID != nil && *initial.RackID != *current.RackID) {
			return fleeterror.NewFailedPreconditionError("miner rack placement changed; refresh before creating the ticket")
		}
	}
	moduleIDs := make([]int64, 0, len(modules))
	for id := range modules {
		moduleIDs = append(moduleIDs, id)
	}
	sort.Slice(moduleIDs, func(i, j int) bool { return moduleIDs[i] < moduleIDs[j] })
	for _, id := range moduleIDs {
		if _, err := s.GetQueries(ctx).LockMaintenanceModuleModelSite(ctx, sqlc.LockMaintenanceModuleModelSiteParams{OrgID: orgID, DeviceID: modules[id], ModuleID: id}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fleeterror.NewFailedPreconditionError("module placement changed; refresh before creating the ticket")
			}
			return maintenanceWriteError("lock ticket module placement", err)
		}
	}
	installationIDs := make([]int64, 0, len(installations))
	for id := range installations {
		installationIDs = append(installationIDs, id)
	}
	sort.Slice(installationIDs, func(i, j int) bool { return installationIDs[i] < installationIDs[j] })
	for _, id := range installationIDs {
		if _, err := s.GetQueries(ctx).LockMaintenanceTicketInstallation(ctx, sqlc.LockMaintenanceTicketInstallationParams{OrgID: orgID, InstallationID: id, ModuleID: installations[id]}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fleeterror.NewFailedPreconditionError("hashboard placement changed; refresh before creating the ticket")
			}
			return maintenanceWriteError("lock ticket hashboard installation", err)
		}
	}
	return nil
}

func (s *SQLMaintenanceStore) CanAccountCloseTicket(ctx context.Context, orgID, ticketID, accountID int64) (bool, error) {
	allowed, err := s.GetQueries(ctx).CanAccountCloseRepairTicket(ctx, sqlc.CanAccountCloseRepairTicketParams{OrgID: orgID, TicketID: ticketID, AccountID: validNullInt64(accountID)})
	if err != nil {
		return false, maintenanceWriteError("check client ticket closure", err)
	}
	return allowed, nil
}
