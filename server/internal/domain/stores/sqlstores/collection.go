package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"

	pb "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ interfaces.CollectionStore = &SQLCollectionStore{}

// SQLCollectionStore implements CollectionStore using PostgreSQL via sqlc.
type SQLCollectionStore struct {
	SQLConnectionManager
}

// NewSQLCollectionStore creates a new SQLCollectionStore.
func NewSQLCollectionStore(conn *sql.DB) *SQLCollectionStore {
	return &SQLCollectionStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLCollectionStore) CreateCollection(ctx context.Context, orgID int64, collectionType pb.CollectionType, label, description string) (*pb.DeviceCollection, error) {
	row, err := s.GetQueries(ctx).CreateDeviceSet(ctx, sqlc.CreateDeviceSetParams{
		OrgID:       orgID,
		Type:        protoDeviceSetTypeToSQL(collectionType),
		Label:       label,
		Description: toNullString(description),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fleeterror.NewPlainError("a collection with this name already exists", connect.CodeAlreadyExists)
		}
		return nil, fleeterror.NewInternalErrorf("failed to create collection: %v", err)
	}

	return &pb.DeviceCollection{
		Id:          row.ID,
		Type:        sqlDeviceSetTypeToProto(row.Type),
		Label:       row.Label,
		Description: fromNullString(row.Description),
		DeviceCount: 0,
		CreatedAt:   timestamppb.New(row.CreatedAt),
		UpdatedAt:   timestamppb.New(row.UpdatedAt),
	}, nil
}

func (s *SQLCollectionStore) CreateRackExtension(ctx context.Context, params interfaces.CreateRackExtensionParams) error {
	err := s.GetQueries(ctx).CreateRackExtension(ctx, sqlc.CreateRackExtensionParams{
		DeviceSetID: params.CollectionID,
		Zone:        toNullString(params.Zone),
		Rows:        params.Rows,
		Columns:     params.Columns,
		OrderIndex:  safeInt32ToInt16(params.OrderIndex),
		CoolingType: safeInt32ToInt16(params.CoolingType),
		OrgID:       params.OrgID,
		SiteID:      ptrToNullInt64(params.SiteID),
		BuildingID:  ptrToNullInt64(params.BuildingID),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create rack extension: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) GetCollection(ctx context.Context, orgID int64, collectionID int64) (*pb.DeviceCollection, error) {
	row, err := s.GetQueries(ctx).GetDeviceSet(ctx, sqlc.GetDeviceSetParams{
		ID:    collectionID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", collectionID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get collection: %v", err)
	}

	collection := newDeviceCollection(row.ID, row.Type, row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt)
	collection.Placement = collectionPlacementRefs(row.SiteID, row.SiteLabel, row.BuildingID, row.BuildingLabel)
	return collection, nil
}

func (s *SQLCollectionStore) GetRackInfo(ctx context.Context, collectionID int64, orgID int64) (*pb.RackInfo, error) {
	row, err := s.GetQueries(ctx).GetRackInfo(ctx, sqlc.GetRackInfoParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to get rack info: %v", err)
	}

	rackInfo := &pb.RackInfo{
		Rows:        row.Rows,
		Columns:     row.Columns,
		OrderIndex:  pb.RackOrderIndex(row.OrderIndex),
		CoolingType: pb.RackCoolingType(row.CoolingType),
		SiteId:      nullInt64ToPtr(row.SiteID),
		BuildingId:  nullInt64ToPtr(row.BuildingID),
	}
	if row.Zone.Valid {
		rackInfo.Zone = row.Zone.String
	}
	return rackInfo, nil
}

// getRackInfoBatch fetches rack info for multiple collection IDs in a single query.
func (s *SQLCollectionStore) getRackInfoBatch(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]*pb.RackInfo, error) {
	if len(collectionIDs) == 0 {
		return make(map[int64]*pb.RackInfo), nil
	}

	rows, err := s.GetQueries(ctx).GetRackInfoBatch(ctx, sqlc.GetRackInfoBatchParams{
		OrgID:        orgID,
		DeviceSetIds: collectionIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to batch-fetch rack info: %v", err)
	}

	result := make(map[int64]*pb.RackInfo, len(collectionIDs))
	for _, row := range rows {
		ri := &pb.RackInfo{
			Rows:        row.Rows,
			Columns:     row.Columns,
			OrderIndex:  pb.RackOrderIndex(row.OrderIndex),
			CoolingType: pb.RackCoolingType(row.CoolingType),
			SiteId:      nullInt64ToPtr(row.SiteID),
			BuildingId:  nullInt64ToPtr(row.BuildingID),
		}
		if row.Zone.Valid {
			ri.Zone = row.Zone.String
		}
		result[row.DeviceSetID] = ri
	}
	return result, nil
}

func (s *SQLCollectionStore) UpdateCollection(ctx context.Context, orgID int64, collectionID int64, label, description *string) error {
	q := s.GetQueries(ctx)

	var err error
	switch {
	case label != nil && description != nil:
		err = q.UpdateDeviceSetLabelAndDescription(ctx, sqlc.UpdateDeviceSetLabelAndDescriptionParams{
			Label:       *label,
			Description: toNullString(*description),
			ID:          collectionID,
			OrgID:       orgID,
		})
	case label != nil:
		err = q.UpdateDeviceSetLabel(ctx, sqlc.UpdateDeviceSetLabelParams{
			Label: *label,
			ID:    collectionID,
			OrgID: orgID,
		})
	case description != nil:
		err = q.UpdateDeviceSetDescription(ctx, sqlc.UpdateDeviceSetDescriptionParams{
			Description: toNullString(*description),
			ID:          collectionID,
			OrgID:       orgID,
		})
	default:
		return nil
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fleeterror.NewPlainError("a collection with this name already exists", connect.CodeAlreadyExists)
		}
		return fleeterror.NewInternalErrorf("failed to update collection: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) UpdateRackInfo(ctx context.Context, collectionID int64, zone string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error {
	err := s.GetQueries(ctx).UpdateRackInfo(ctx, sqlc.UpdateRackInfoParams{
		Zone:        toNullString(zone),
		Rows:        rows,
		Columns:     columns,
		OrderIndex:  safeInt32ToInt16(orderIndex),
		CoolingType: safeInt32ToInt16(coolingType),
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update rack info: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) LockRackPlacementForWrite(ctx context.Context, collectionID, orgID int64) (interfaces.RackPlacement, error) {
	row, err := s.GetQueries(ctx).LockRackPlacementForWrite(ctx, sqlc.LockRackPlacementForWriteParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return interfaces.RackPlacement{}, fleeterror.NewNotFoundErrorf("rack %d not found", collectionID)
		}
		return interfaces.RackPlacement{}, fleeterror.NewInternalErrorf("failed to lock rack placement: %v", err)
	}
	placement := interfaces.RackPlacement{
		SiteID:     nullInt64ToPtr(row.SiteID),
		BuildingID: nullInt64ToPtr(row.BuildingID),
	}
	if row.Zone.Valid {
		placement.Zone = row.Zone.String
	}
	return placement, nil
}

func (s *SQLCollectionStore) UpdateRackPlacement(ctx context.Context, collectionID, orgID int64, siteID, buildingID *int64, zone string) error {
	err := s.GetQueries(ctx).UpdateRackPlacement(ctx, sqlc.UpdateRackPlacementParams{
		SiteID:      ptrToNullInt64(siteID),
		BuildingID:  ptrToNullInt64(buildingID),
		Zone:        toNullString(zone),
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update rack placement: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) UpdateRackPlacementBulkForBuilding(ctx context.Context, orgID int64, rackIDs []int64, targetSiteID, targetBuildingID *int64) (int64, error) {
	if len(rackIDs) == 0 {
		return 0, nil
	}
	rowsAffected, err := s.GetQueries(ctx).UpdateRackPlacementBulkForBuilding(ctx, sqlc.UpdateRackPlacementBulkForBuildingParams{
		TargetBuildingID: ptrToNullInt64(targetBuildingID),
		TargetSiteID:     ptrToNullInt64(targetSiteID),
		RackIds:          rackIDs,
		OrgID:            orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to bulk-update rack placement: %w", err)
	}
	return rowsAffected, nil
}

func (s *SQLCollectionStore) UpdateRackPlacementBulkForSite(ctx context.Context, orgID int64, rackIDs []int64, targetSiteID *int64) error {
	if len(rackIDs) == 0 {
		return nil
	}
	if err := s.GetQueries(ctx).UpdateRackPlacementBulkForSite(ctx, sqlc.UpdateRackPlacementBulkForSiteParams{
		TargetSiteID: ptrToNullInt64(targetSiteID),
		RackIds:      rackIDs,
		OrgID:        orgID,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to bulk-update rack placement (site): %w", err)
	}
	return nil
}

func (s *SQLCollectionStore) UnassignDeviceSitesByRack(ctx context.Context, collectionID, orgID int64) (int64, error) {
	n, err := s.GetQueries(ctx).UnassignDeviceSitesByRack(ctx, sqlc.UnassignDeviceSitesByRackParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to unassign device sites by rack: %v", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) CascadeRackDeviceSites(ctx context.Context, collectionID, orgID int64, targetSiteID *int64) (int64, error) {
	n, err := s.GetQueries(ctx).CascadeRackDeviceSites(ctx, sqlc.CascadeRackDeviceSitesParams{
		DeviceSetID:  collectionID,
		OrgID:        orgID,
		TargetSiteID: ptrToNullInt64(targetSiteID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to cascade rack device sites: %v", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) CascadeRackDeviceSitesBulk(ctx context.Context, orgID int64, rackIDs []int64, targetSiteID *int64) (int64, error) {
	if len(rackIDs) == 0 {
		return 0, nil
	}
	n, err := s.GetQueries(ctx).CascadeRackDeviceSitesBulk(ctx, sqlc.CascadeRackDeviceSitesBulkParams{
		TargetSiteID: ptrToNullInt64(targetSiteID),
		RackIds:      rackIDs,
		OrgID:        orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to bulk-cascade rack device sites: %w", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) UnassignDeviceBuildingsByRack(ctx context.Context, collectionID, orgID int64) (int64, error) {
	n, err := s.GetQueries(ctx).UnassignDeviceBuildingsByRack(ctx, sqlc.UnassignDeviceBuildingsByRackParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to unassign device buildings by rack: %w", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) CascadeRackDeviceBuildings(ctx context.Context, collectionID, orgID int64, targetBuildingID *int64) (int64, error) {
	n, err := s.GetQueries(ctx).CascadeRackDeviceBuildings(ctx, sqlc.CascadeRackDeviceBuildingsParams{
		DeviceSetID:      collectionID,
		OrgID:            orgID,
		TargetBuildingID: ptrToNullInt64(targetBuildingID),
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to cascade rack device buildings: %w", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) CascadeRackDeviceBuildingsBulk(ctx context.Context, orgID int64, rackIDs []int64, targetBuildingID *int64) (int64, error) {
	if len(rackIDs) == 0 {
		return 0, nil
	}
	n, err := s.GetQueries(ctx).CascadeRackDeviceBuildingsBulk(ctx, sqlc.CascadeRackDeviceBuildingsBulkParams{
		TargetBuildingID: ptrToNullInt64(targetBuildingID),
		RackIds:          rackIDs,
		OrgID:            orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to bulk-cascade rack device buildings: %w", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) CascadeAddedDeviceBuildings(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) (int64, error) {
	if len(deviceIdentifiers) == 0 {
		return 0, nil
	}
	n, err := s.GetQueries(ctx).CascadeAddedDeviceBuildings(ctx, sqlc.CascadeAddedDeviceBuildingsParams{
		OrgID:             orgID,
		ID:                deviceSetID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to cascade added-device buildings: %w", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) GetDeviceSiteIDsByMembership(ctx context.Context, collectionID, orgID int64) (map[string]*int64, error) {
	rows, err := s.GetQueries(ctx).GetDeviceSiteIDsByMembership(ctx, sqlc.GetDeviceSiteIDsByMembershipParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to load device sites for rack members: %v", err)
	}
	out := make(map[string]*int64, len(rows))
	for _, row := range rows {
		out[row.DeviceIdentifier] = nullInt64ToPtr(row.SiteID)
	}
	return out, nil
}

func (s *SQLCollectionStore) GetBuildingSite(ctx context.Context, orgID, buildingID int64) (*int64, error) {
	siteID, err := s.GetQueries(ctx).GetBuildingSite(ctx, sqlc.GetBuildingSiteParams{
		ID:    buildingID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("building %d not found", buildingID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to look up building site: %v", err)
	}
	return nullInt64ToPtr(siteID), nil
}

func (s *SQLCollectionStore) GetAddedDeviceSiteConflicts(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) ([]interfaces.AddedDeviceSiteConflict, error) {
	if len(deviceIdentifiers) == 0 {
		return nil, nil
	}
	rows, err := s.GetQueries(ctx).GetAddedDeviceSiteConflicts(ctx, sqlc.GetAddedDeviceSiteConflictsParams{
		OrgID:             orgID,
		ID:                deviceSetID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to load added-device site conflicts: %v", err)
	}
	out := make([]interfaces.AddedDeviceSiteConflict, 0, len(rows))
	for _, row := range rows {
		if !row.TargetSiteID.Valid {
			continue
		}
		out = append(out, interfaces.AddedDeviceSiteConflict{
			DeviceIdentifier: row.DeviceIdentifier,
			PriorSiteID:      nullInt64ToPtr(row.PriorSiteID),
			TargetSiteID:     row.TargetSiteID.Int64,
		})
	}
	return out, nil
}

func (s *SQLCollectionStore) CascadeAddedDeviceSites(ctx context.Context, orgID, deviceSetID int64, deviceIdentifiers []string) (int64, error) {
	if len(deviceIdentifiers) == 0 {
		return 0, nil
	}
	n, err := s.GetQueries(ctx).CascadeAddedDeviceSites(ctx, sqlc.CascadeAddedDeviceSitesParams{
		OrgID:             orgID,
		ID:                deviceSetID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to cascade added-device sites: %v", err)
	}
	return n, nil
}

func (s *SQLCollectionStore) SoftDeleteCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error) {
	return s.GetQueries(ctx).SoftDeleteDeviceSet(ctx, sqlc.SoftDeleteDeviceSetParams{
		ID:    collectionID,
		OrgID: orgID,
	})
}

func (s *SQLCollectionStore) ClearRackPlacementForSoftDelete(ctx context.Context, orgID, collectionID int64) error {
	if err := s.GetQueries(ctx).ClearRackPlacementForSoftDelete(ctx, sqlc.ClearRackPlacementForSoftDeleteParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to clear rack placement on soft-delete: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) ListCollections(ctx context.Context, orgID int64, collectionType pb.CollectionType, pageSize int32, pageToken string, sort *interfaces.SortConfig, filter *interfaces.DeviceSetFilter) ([]*pb.DeviceCollection, string, int32, error) {
	cursor, err := decodeCollectionCursor(pageToken)
	if err != nil {
		return nil, "", 0, err
	}

	sortField, sortDir := resolveCollectionSort(sort)

	// Validate cursor matches current sort (reject stale cursors from a different sort)
	if cursor != nil && cursor.SortField != "" && cursor.SortField != sortField {
		return nil, "", 0, fleeterror.NewInvalidArgumentErrorf("cursor was created with sort field %q but request uses %q", cursor.SortField, sortField)
	}
	if cursor != nil && cursor.SortDir != "" && cursor.SortDir != sortDir {
		return nil, "", 0, fleeterror.NewInvalidArgumentErrorf("cursor was created with sort direction %q but request uses %q", cursor.SortDir, sortDir)
	}

	// Count total
	var totalCount int32
	countQuery, countArgs := buildCollectionCountQuery(orgID, collectionType, filter)
	if err := s.conn.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to count collections: %v", err)
	}

	// Build list query
	fetchLimit := pageSize + 1
	query, args := buildCollectionListQuery(orgID, collectionType, cursor, sortField, sortDir, fetchLimit, filter)

	sqlRows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to list collections: %v", err)
	}
	defer sqlRows.Close()

	type collectionRow struct {
		ID            int64
		Type          string
		Label         string
		Description   sql.NullString
		DeviceCount   int32
		IssueCount    int32
		CreatedAt     time.Time
		UpdatedAt     time.Time
		Zone          sql.NullString
		SiteID        sql.NullInt64
		SiteLabel     string
		BuildingID    sql.NullInt64
		BuildingLabel string
	}

	var rows []collectionRow
	for sqlRows.Next() {
		var r collectionRow
		if err := sqlRows.Scan(&r.ID, &r.Type, &r.Label, &r.Description, &r.CreatedAt, &r.UpdatedAt, &r.DeviceCount, &r.IssueCount, &r.Zone, &r.SiteID, &r.SiteLabel, &r.BuildingID, &r.BuildingLabel); err != nil {
			return nil, "", 0, fleeterror.NewInternalErrorf("failed to scan collection row: %v", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to iterate collection rows: %v", err)
	}

	var nextPageToken string
	if len(rows) > int(pageSize) {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		cur := &collectionCursor{Label: last.Label, ID: last.ID, SortField: sortField, SortDir: sortDir}
		if sortField == collectionSortFieldDeviceCount {
			cur.DeviceCount = &last.DeviceCount
		}
		if sortField == collectionSortFieldIssueCount {
			cur.IssueCount = &last.IssueCount
		}
		if sortField == collectionSortFieldZone && last.Zone.Valid {
			z := last.Zone.String
			cur.Zone = &z
		}
		nextPageToken = encodeCollectionCursor(cur)
	}

	result := make([]*pb.DeviceCollection, len(rows))
	var rackIDs []int64
	for i, row := range rows {
		result[i] = newDeviceCollection(row.ID, sqlc.DeviceSetType(row.Type), row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt)
		result[i].Placement = collectionPlacementRefs(row.SiteID, row.SiteLabel, row.BuildingID, row.BuildingLabel)
		if sqlc.DeviceSetType(row.Type) == sqlc.DeviceSetTypeRack {
			rackIDs = append(rackIDs, row.ID)
		}
	}

	// Batch-fetch rack info for rack-type collections so typeDetails is populated.
	if len(rackIDs) > 0 {
		rackInfoMap, err := s.getRackInfoBatch(ctx, orgID, rackIDs)
		if err != nil {
			return nil, "", 0, err
		}
		for _, c := range result {
			if ri, ok := rackInfoMap[c.Id]; ok {
				c.TypeDetails = &pb.DeviceCollection_RackInfo{RackInfo: ri}
			}
		}
	}

	return result, nextPageToken, totalCount, nil
}

func (s *SQLCollectionStore) CollectionBelongsToOrg(ctx context.Context, collectionID int64, orgID int64) (bool, error) {
	return s.GetQueries(ctx).DeviceSetBelongsToOrg(ctx, sqlc.DeviceSetBelongsToOrgParams{
		ID:    collectionID,
		OrgID: orgID,
	})
}

func (s *SQLCollectionStore) GetCollectionType(ctx context.Context, orgID int64, collectionID int64) (pb.CollectionType, error) {
	sqlType, err := s.GetQueries(ctx).GetDeviceSetType(ctx, sqlc.GetDeviceSetTypeParams{
		ID:    collectionID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, fleeterror.NewNotFoundErrorf("collection not found: %d", collectionID)
		}
		return pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, fleeterror.NewInternalErrorf("failed to get collection type: %v", err)
	}
	return sqlDeviceSetTypeToProto(sqlType), nil
}

func (s *SQLCollectionStore) GetCollectionTypes(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]pb.CollectionType, error) {
	if len(collectionIDs) == 0 {
		return make(map[int64]pb.CollectionType), nil
	}

	rows, err := s.GetQueries(ctx).GetDeviceSetTypesBatch(ctx, sqlc.GetDeviceSetTypesBatchParams{
		OrgID:        orgID,
		DeviceSetIds: collectionIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get collection types: %v", err)
	}

	result := make(map[int64]pb.CollectionType, len(collectionIDs))
	for _, row := range rows {
		result[row.ID] = sqlDeviceSetTypeToProto(row.Type)
	}
	return result, nil
}

func (s *SQLCollectionStore) AddDevicesToCollection(ctx context.Context, orgID int64, collectionID int64, deviceIdentifiers []string) (int64, error) {
	count, err := s.GetQueries(ctx).AddDevicesToDeviceSet(ctx, sqlc.AddDevicesToDeviceSetParams{
		OrgID:             orgID,
		DeviceSetID:       collectionID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to add devices to collection: %v", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) RemoveAllDevicesFromCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error) {
	count, err := s.GetQueries(ctx).RemoveAllDevicesFromDeviceSet(ctx, sqlc.RemoveAllDevicesFromDeviceSetParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to remove all devices from collection: %v", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) RemoveDevicesFromCollection(ctx context.Context, orgID int64, collectionID int64, deviceIdentifiers []string) (int64, error) {
	count, err := s.GetQueries(ctx).RemoveDevicesFromDeviceSet(ctx, sqlc.RemoveDevicesFromDeviceSetParams{
		DeviceSetID:       collectionID,
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to remove devices from collection: %v", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) RemoveDevicesFromAnyRack(ctx context.Context, orgID int64, deviceIdentifiers []string, targetRackID int64) (int64, error) {
	count, err := s.GetQueries(ctx).RemoveDevicesFromAnyRack(ctx, sqlc.RemoveDevicesFromAnyRackParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
		TargetRackID:      targetRackID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to remove devices from rack: %w", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) FindDevicesWithSiteOrBuilding(ctx context.Context, orgID int64, deviceIdentifiers []string) ([]string, error) {
	rows, err := s.GetQueries(ctx).FindDevicesWithSiteOrBuilding(ctx, sqlc.FindDevicesWithSiteOrBuildingParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to find devices with site or building: %w", err)
	}
	return rows, nil
}

func (s *SQLCollectionStore) ClearDeviceSitesAndBuildings(ctx context.Context, orgID int64, deviceIdentifiers []string) (int64, error) {
	count, err := s.GetQueries(ctx).ClearDeviceSitesAndBuildings(ctx, sqlc.ClearDeviceSitesAndBuildingsParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to clear device sites and buildings: %w", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) LockRacksForReparent(ctx context.Context, orgID int64, deviceIdentifiers []string, targetRackID int64) ([]int64, error) {
	ids, err := s.GetQueries(ctx).LockRacksForReparent(ctx, sqlc.LockRacksForReparentParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
		TargetRackID:      targetRackID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to lock racks for reparent: %w", err)
	}
	return ids, nil
}

func (s *SQLCollectionStore) ListCollectionMembers(ctx context.Context, orgID int64, collectionID int64, pageSize int32, pageToken string) ([]*pb.CollectionMember, string, error) {
	cursor, err := decodeMemberCursor(pageToken)
	if err != nil {
		return nil, "", err
	}

	fetchLimit := pageSize + 1

	type memberRow struct {
		ID               int64
		DeviceIdentifier string
		CreatedAt        time.Time
		SlotRow          sql.NullInt32
		SlotCol          sql.NullInt32
	}

	var rows []memberRow

	if cursor == nil {
		sqlRows, err := s.GetQueries(ctx).ListDeviceSetMembersPaginated(ctx, sqlc.ListDeviceSetMembersPaginatedParams{
			DeviceSetID: collectionID,
			OrgID:       orgID,
			Limit:       fetchLimit,
		})
		if err != nil {
			return nil, "", fleeterror.NewInternalErrorf("failed to list collection members: %v", err)
		}
		for _, r := range sqlRows {
			rows = append(rows, memberRow{r.ID, r.DeviceIdentifier, r.CreatedAt, r.SlotRow, r.SlotCol})
		}
	} else {
		sqlRows, err := s.GetQueries(ctx).ListDeviceSetMembersPaginatedAfter(ctx, sqlc.ListDeviceSetMembersPaginatedAfterParams{
			DeviceSetID:     collectionID,
			OrgID:           orgID,
			Limit:           fetchLimit,
			CursorCreatedAt: cursor.CreatedAt,
			CursorID:        cursor.ID,
		})
		if err != nil {
			return nil, "", fleeterror.NewInternalErrorf("failed to list collection members: %v", err)
		}
		for _, r := range sqlRows {
			rows = append(rows, memberRow{r.ID, r.DeviceIdentifier, r.CreatedAt, r.SlotRow, r.SlotCol})
		}
	}

	var nextPageToken string
	if len(rows) > int(pageSize) {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		nextPageToken = encodeMemberCursor(&memberCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}

	result := make([]*pb.CollectionMember, len(rows))
	for i, row := range rows {
		member := &pb.CollectionMember{
			DeviceIdentifier: row.DeviceIdentifier,
			AddedAt:          timestamppb.New(row.CreatedAt),
		}
		if row.SlotRow.Valid && row.SlotCol.Valid {
			member.MemberDetails = &pb.CollectionMember_Rack{
				Rack: &pb.RackMemberDetails{
					SlotPosition: &pb.RackSlotPosition{
						Row:    row.SlotRow.Int32,
						Column: row.SlotCol.Int32,
					},
				},
			}
		}
		result[i] = member
	}
	return result, nextPageToken, nil
}

func (s *SQLCollectionStore) GetDeviceCollections(ctx context.Context, orgID int64, deviceIdentifier string, collectionType pb.CollectionType) ([]*pb.DeviceCollection, error) {
	if collectionType == pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		rows, err := s.GetQueries(ctx).GetDeviceDeviceSets(ctx, sqlc.GetDeviceDeviceSetsParams{
			DeviceIdentifier: deviceIdentifier,
			OrgID:            orgID,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get device collections: %v", err)
		}

		result := make([]*pb.DeviceCollection, len(rows))
		for i, row := range rows {
			result[i] = newDeviceCollection(row.ID, row.Type, row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt)
		}
		return result, nil
	}

	rows, err := s.GetQueries(ctx).GetDeviceDeviceSetsByType(ctx, sqlc.GetDeviceDeviceSetsByTypeParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
		Type:             protoDeviceSetTypeToSQL(collectionType),
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get device collections by type: %v", err)
	}

	result := make([]*pb.DeviceCollection, len(rows))
	for i, row := range rows {
		result[i] = newDeviceCollection(row.ID, row.Type, row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt)
	}
	return result, nil
}

func (s *SQLCollectionStore) GetGroupRefsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string][]interfaces.DeviceGroupRef, error) {
	if len(deviceIdentifiers) == 0 {
		return make(map[string][]interfaces.DeviceGroupRef), nil
	}

	rows, err := s.GetQueries(ctx).GetGroupRefsForDevices(ctx, sqlc.GetGroupRefsForDevicesParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get group refs: %v", err)
	}

	result := make(map[string][]interfaces.DeviceGroupRef)
	for _, row := range rows {
		result[row.DeviceIdentifier] = append(result[row.DeviceIdentifier], interfaces.DeviceGroupRef{
			ID:    row.ID,
			Label: row.Label,
		})
	}
	return result, nil
}

func (s *SQLCollectionStore) GetRackDetailsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]interfaces.DeviceRackDetails, error) {
	if len(deviceIdentifiers) == 0 {
		return make(map[string]interfaces.DeviceRackDetails), nil
	}

	rows, err := s.GetQueries(ctx).GetRackDetailsForDevices(ctx, sqlc.GetRackDetailsForDevicesParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get rack details: %v", err)
	}

	result := make(map[string]interfaces.DeviceRackDetails)
	for _, row := range rows {
		var buildingID *int64
		if row.BuildingID.Valid {
			buildingID = &row.BuildingID.Int64
		}
		result[row.DeviceIdentifier] = interfaces.DeviceRackDetails{
			ID:            row.RackID,
			Label:         row.Label,
			Position:      row.Position,
			BuildingID:    buildingID,
			BuildingLabel: row.BuildingLabel,
		}
	}
	return result, nil
}

func (s *SQLCollectionStore) SetRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string, row, column int32, orgID int64) error {
	err := s.GetQueries(ctx).SetRackSlotPosition(ctx, sqlc.SetRackSlotPositionParams{
		DeviceSetID:      collectionID,
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
		Row:              row,
		Col:              column,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to set rack slot position: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) ClearRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string, orgID int64) error {
	err := s.GetQueries(ctx).ClearRackSlotPosition(ctx, sqlc.ClearRackSlotPositionParams{
		DeviceSetID:      collectionID,
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to clear rack slot position: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) GetRackSlots(ctx context.Context, collectionID int64, orgID int64) ([]*pb.RackSlot, error) {
	rows, err := s.GetQueries(ctx).GetRackSlots(ctx, sqlc.GetRackSlotsParams{
		DeviceSetID: collectionID,
		OrgID:       orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get rack slots: %v", err)
	}

	result := make([]*pb.RackSlot, len(rows))
	for i, row := range rows {
		result[i] = &pb.RackSlot{
			DeviceIdentifier: row.DeviceIdentifier,
			Position: &pb.RackSlotPosition{
				Row:    row.Row,
				Column: row.Col,
			},
		}
	}
	return result, nil
}

func (s *SQLCollectionStore) GetRackSlotStatuses(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64][]*pb.RackSlotStatus, error) {
	if len(collectionIDs) == 0 {
		return make(map[int64][]*pb.RackSlotStatus), nil
	}

	// Generate all (row, col) positions for each rack and LEFT JOIN with
	// slot assignments + device status to produce SlotDeviceStatus per position.
	// Uses the same bucket logic as GetMinerStateCountsByCollections.
	query := `WITH rack_dims AS (
    SELECT dcr.device_set_id, dcr.rows, dcr.columns
    FROM device_set_rack dcr
    JOIN device_set dc ON dcr.device_set_id = dc.id
    WHERE dcr.device_set_id = ANY($2::bigint[])
      AND dc.org_id = $1
      AND dc.deleted_at IS NULL
),
all_positions AS (
    SELECT rd.device_set_id, r.row_num, c.col_num
    FROM rack_dims rd
    CROSS JOIN LATERAL generate_series(0, rd.rows - 1) AS r(row_num)
    CROSS JOIN LATERAL generate_series(0, rd.columns - 1) AS c(col_num)
),
slot_devices AS (
    SELECT rs.device_set_id, rs.row, rs.col,
           dcm.device_identifier,
           ds.status AS device_status,
           dp.pairing_status,
           CASE WHEN open_errors.device_id IS NOT NULL THEN true ELSE false END AS has_errors
    FROM rack_slot rs
    JOIN device_set dc ON rs.device_set_id = dc.id AND dc.org_id = $1 AND dc.deleted_at IS NULL
    JOIN device_set_membership dcm ON rs.device_set_id = dcm.device_set_id AND rs.device_id = dcm.device_id
    JOIN device d ON dcm.device_id = d.id AND d.deleted_at IS NULL
    JOIN device_pairing dp ON d.id = dp.device_id
        AND ` + actionablePairingStatusesExpr("dp") + `
    LEFT JOIN device_status ds ON d.id = ds.device_id
    LEFT JOIN (
        SELECT DISTINCT device_id
        FROM errors
        WHERE errors.org_id = $1
          AND errors.closed_at IS NULL
          AND errors.severity IN (1, 2, 3, 4)
          AND errors.device_id IN (SELECT device_id FROM rack_slot WHERE device_set_id = ANY($2::bigint[]))
    ) open_errors ON d.id = open_errors.device_id
    WHERE rs.device_set_id = ANY($2::bigint[])
)
SELECT ap.device_set_id, ap.row_num AS row, ap.col_num AS col,
    CASE
        -- SlotDeviceStatus enum values (collection.v1.SlotDeviceStatus):
        -- 1 = EMPTY, 2 = HEALTHY, 3 = NEEDS_ATTENTION, 4 = OFFLINE, 5 = SLEEPING
        WHEN sd.device_identifier IS NULL THEN 1
        WHEN sd.device_status = 'OFFLINE' OR sd.device_status IS NULL THEN 4
        WHEN sd.device_status IN ('MAINTENANCE', 'INACTIVE') THEN 5
        WHEN sd.device_status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED')
             OR sd.pairing_status IN ('AUTHENTICATION_NEEDED')
             OR sd.has_errors THEN 3
        ELSE 2
    END AS status
FROM all_positions ap
LEFT JOIN slot_devices sd ON sd.device_set_id = ap.device_set_id
    AND sd.row = ap.row_num AND sd.col = ap.col_num
ORDER BY ap.device_set_id, ap.row_num, ap.col_num`

	rows, err := s.conn.QueryContext(ctx, query, orgID, pq.Array(collectionIDs))
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get rack slot statuses: %v", err)
	}
	defer rows.Close()

	result := make(map[int64][]*pb.RackSlotStatus)
	for rows.Next() {
		var collectionID int64
		var row, col, status int32
		if err := rows.Scan(&collectionID, &row, &col, &status); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to scan rack slot status: %v", err)
		}
		result[collectionID] = append(result[collectionID], &pb.RackSlotStatus{
			Row:    row,
			Column: col,
			Status: pb.SlotDeviceStatus(status),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to iterate rack slot statuses: %v", err)
	}

	return result, nil
}

func (s *SQLCollectionStore) ListRackZones(ctx context.Context, orgID int64) ([]string, error) {
	rows, err := s.GetQueries(ctx).ListRackZones(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list rack zones: %v", err)
	}

	zones := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Valid {
			zones = append(zones, row.String)
		}
	}
	return zones, nil
}

func (s *SQLCollectionStore) ListRackZoneRefs(ctx context.Context, orgID int64) ([]interfaces.ZoneRefRow, error) {
	rows, err := s.GetQueries(ctx).ListRackZoneRefs(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list rack zone refs: %v", err)
	}
	refs := make([]interfaces.ZoneRefRow, 0, len(rows))
	for _, row := range rows {
		zone := ""
		if row.Zone.Valid {
			zone = row.Zone.String
		}
		refs = append(refs, interfaces.ZoneRefRow{
			BuildingID:    row.BuildingID,
			BuildingLabel: row.BuildingLabel,
			SiteID:        row.SiteID,
			SiteLabel:     row.SiteLabel,
			Zone:          zone,
		})
	}
	return refs, nil
}

func (s *SQLCollectionStore) ListRackTypes(ctx context.Context, orgID int64) ([]*pb.RackType, error) {
	rows, err := s.GetQueries(ctx).ListRackTypes(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list rack types: %v", err)
	}

	rackTypes := make([]*pb.RackType, len(rows))
	for i, row := range rows {
		rackTypes[i] = &pb.RackType{Rows: row.Rows, Columns: row.Columns, RackCount: row.RackCount}
	}
	return rackTypes, nil
}

// safeInt32ToInt16 converts int32 to int16 with clamping to avoid overflow.
func safeInt32ToInt16(v int32) int16 {
	if v > math.MaxInt16 {
		return math.MaxInt16
	}
	if v < math.MinInt16 {
		return math.MinInt16
	}
	return int16(v) // #nosec G115 -- bounds checked above
}

// Type conversion helpers

func protoDeviceSetTypeToSQL(ct pb.CollectionType) sqlc.DeviceSetType {
	switch ct {
	case pb.CollectionType_COLLECTION_TYPE_GROUP:
		return sqlc.DeviceSetTypeGroup
	case pb.CollectionType_COLLECTION_TYPE_RACK:
		return sqlc.DeviceSetTypeRack
	case pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED:
		// Callers should validate type before reaching this point.
		// Default to group to avoid panicking on unvalidated input.
		return sqlc.DeviceSetTypeGroup
	default:
		return sqlc.DeviceSetTypeGroup
	}
}

func sqlDeviceSetTypeToProto(ct sqlc.DeviceSetType) pb.CollectionType {
	switch ct {
	case sqlc.DeviceSetTypeGroup:
		return pb.CollectionType_COLLECTION_TYPE_GROUP
	case sqlc.DeviceSetTypeRack:
		return pb.CollectionType_COLLECTION_TYPE_RACK
	default:
		return pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED
	}
}

// Row conversion helpers

func fromNullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func newDeviceCollection(id int64, ct sqlc.DeviceSetType, label string, description sql.NullString, deviceCount int32, createdAt, updatedAt time.Time) *pb.DeviceCollection {
	return &pb.DeviceCollection{
		Id:          id,
		Type:        sqlDeviceSetTypeToProto(ct),
		Label:       label,
		Description: fromNullString(description),
		DeviceCount: deviceCount,
		CreatedAt:   timestamppb.New(createdAt),
		UpdatedAt:   timestamppb.New(updatedAt),
	}
}

func collectionPlacementRefs(siteID sql.NullInt64, siteLabel string, buildingID sql.NullInt64, buildingLabel string) *commonpb.PlacementRefs {
	var placement *commonpb.PlacementRefs
	if siteID.Valid {
		placement = &commonpb.PlacementRefs{
			Site: &commonpb.ResourceRef{
				Id:    siteID.Int64,
				Label: siteLabel,
			},
		}
	}
	if buildingID.Valid {
		if placement == nil {
			placement = &commonpb.PlacementRefs{}
		}
		placement.Building = &commonpb.ResourceRef{
			Id:    buildingID.Int64,
			Label: buildingLabel,
		}
	}
	return placement
}

func (s *SQLCollectionStore) GetDeviceIdentifiersByDeviceSetID(ctx context.Context, deviceSetID, orgID int64) ([]string, error) {
	ids, err := s.GetQueries(ctx).GetDeviceIdentifiersByDeviceSetID(ctx, sqlc.GetDeviceIdentifiersByDeviceSetIDParams{
		DeviceSetID: deviceSetID,
		OrgID:       orgID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get device identifiers by device set ID: %v", err)
	}
	return ids, nil
}
