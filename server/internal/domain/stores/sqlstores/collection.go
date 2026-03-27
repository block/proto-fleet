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

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/proto-at-block/proto-fleet/server/generated/sqlc"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
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
	row, err := s.GetQueries(ctx).CreateCollection(ctx, sqlc.CreateCollectionParams{
		OrgID:       orgID,
		Type:        protoCollectionTypeToSQL(collectionType),
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
		Type:        sqlCollectionTypeToProto(row.Type),
		Label:       row.Label,
		Description: fromNullString(row.Description),
		DeviceCount: 0,
		CreatedAt:   timestamppb.New(row.CreatedAt),
		UpdatedAt:   timestamppb.New(row.UpdatedAt),
	}, nil
}

func (s *SQLCollectionStore) CreateRackExtension(ctx context.Context, collectionID int64, location string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error {
	err := s.GetQueries(ctx).CreateRackExtension(ctx, sqlc.CreateRackExtensionParams{
		CollectionID: collectionID,
		Location:     toNullString(location),
		Rows:         rows,
		Columns:      columns,
		OrderIndex:   safeInt32ToInt16(orderIndex),
		CoolingType:  safeInt32ToInt16(coolingType),
		OrgID:        orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to create rack extension: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) GetCollection(ctx context.Context, orgID int64, collectionID int64) (*pb.DeviceCollection, error) {
	row, err := s.GetQueries(ctx).GetCollection(ctx, sqlc.GetCollectionParams{
		ID:    collectionID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("collection not found: %d", collectionID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get collection: %v", err)
	}

	return newDeviceCollection(row.ID, row.Type, row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt), nil
}

func (s *SQLCollectionStore) GetRackInfo(ctx context.Context, collectionID int64, orgID int64) (*pb.RackInfo, error) {
	row, err := s.GetQueries(ctx).GetRackInfo(ctx, sqlc.GetRackInfoParams{
		CollectionID: collectionID,
		OrgID:        orgID,
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
	}
	if row.Location.Valid {
		rackInfo.Location = row.Location.String
	}
	return rackInfo, nil
}

// getRackInfoBatch fetches rack info for multiple collection IDs in a single query.
func (s *SQLCollectionStore) getRackInfoBatch(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]*pb.RackInfo, error) {
	if len(collectionIDs) == 0 {
		return make(map[int64]*pb.RackInfo), nil
	}

	rows, err := s.GetQueries(ctx).GetRackInfoBatch(ctx, sqlc.GetRackInfoBatchParams{
		OrgID:         orgID,
		CollectionIds: collectionIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to batch-fetch rack info: %v", err)
	}

	result := make(map[int64]*pb.RackInfo, len(collectionIDs))
	for _, row := range rows {
		ri := &pb.RackInfo{Rows: row.Rows, Columns: row.Columns, OrderIndex: pb.RackOrderIndex(row.OrderIndex), CoolingType: pb.RackCoolingType(row.CoolingType)}
		if row.Location.Valid {
			ri.Location = row.Location.String
		}
		result[row.CollectionID] = ri
	}
	return result, nil
}

func (s *SQLCollectionStore) UpdateCollection(ctx context.Context, orgID int64, collectionID int64, label, description *string) error {
	q := s.GetQueries(ctx)

	var err error
	switch {
	case label != nil && description != nil:
		err = q.UpdateCollectionLabelAndDescription(ctx, sqlc.UpdateCollectionLabelAndDescriptionParams{
			Label:       *label,
			Description: toNullString(*description),
			ID:          collectionID,
			OrgID:       orgID,
		})
	case label != nil:
		err = q.UpdateCollectionLabel(ctx, sqlc.UpdateCollectionLabelParams{
			Label: *label,
			ID:    collectionID,
			OrgID: orgID,
		})
	case description != nil:
		err = q.UpdateCollectionDescription(ctx, sqlc.UpdateCollectionDescriptionParams{
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

func (s *SQLCollectionStore) UpdateRackInfo(ctx context.Context, collectionID int64, location string, rows, columns int32, orderIndex, coolingType int32, orgID int64) error {
	err := s.GetQueries(ctx).UpdateRackInfo(ctx, sqlc.UpdateRackInfoParams{
		Location:     toNullString(location),
		Rows:         rows,
		Columns:      columns,
		OrderIndex:   safeInt32ToInt16(orderIndex),
		CoolingType:  safeInt32ToInt16(coolingType),
		CollectionID: collectionID,
		OrgID:        orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update rack info: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) SoftDeleteCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error) {
	return s.GetQueries(ctx).SoftDeleteCollection(ctx, sqlc.SoftDeleteCollectionParams{
		ID:    collectionID,
		OrgID: orgID,
	})
}

func (s *SQLCollectionStore) ListCollections(ctx context.Context, orgID int64, collectionType pb.CollectionType, pageSize int32, pageToken string, sort *interfaces.SortConfig, errorComponentTypes []int32, locations []string) ([]*pb.DeviceCollection, string, int32, error) {
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
	countQuery, countArgs := buildCollectionCountQuery(orgID, collectionType, errorComponentTypes, locations)
	if err := s.conn.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to count collections: %v", err)
	}

	// Build list query
	fetchLimit := pageSize + 1
	query, args := buildCollectionListQuery(orgID, collectionType, cursor, sortField, sortDir, fetchLimit, errorComponentTypes, locations)

	sqlRows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fleeterror.NewInternalErrorf("failed to list collections: %v", err)
	}
	defer sqlRows.Close()

	type collectionRow struct {
		ID          int64
		Type        string
		Label       string
		Description sql.NullString
		DeviceCount int32
		CreatedAt   time.Time
		UpdatedAt   time.Time
		Location    sql.NullString
	}

	var rows []collectionRow
	for sqlRows.Next() {
		var r collectionRow
		if err := sqlRows.Scan(&r.ID, &r.Type, &r.Label, &r.Description, &r.CreatedAt, &r.UpdatedAt, &r.DeviceCount, &r.Location); err != nil {
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
		if sortField == collectionSortFieldLocation && last.Location.Valid {
			loc := last.Location.String
			cur.Location = &loc
		}
		nextPageToken = encodeCollectionCursor(cur)
	}

	result := make([]*pb.DeviceCollection, len(rows))
	var rackIDs []int64
	for i, row := range rows {
		result[i] = newDeviceCollection(row.ID, sqlc.CollectionType(row.Type), row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt)
		if sqlc.CollectionType(row.Type) == sqlc.CollectionTypeRack {
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
	return s.GetQueries(ctx).CollectionBelongsToOrg(ctx, sqlc.CollectionBelongsToOrgParams{
		ID:    collectionID,
		OrgID: orgID,
	})
}

func (s *SQLCollectionStore) GetCollectionType(ctx context.Context, orgID int64, collectionID int64) (pb.CollectionType, error) {
	sqlType, err := s.GetQueries(ctx).GetCollectionType(ctx, sqlc.GetCollectionTypeParams{
		ID:    collectionID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, fleeterror.NewNotFoundErrorf("collection not found: %d", collectionID)
		}
		return pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED, fleeterror.NewInternalErrorf("failed to get collection type: %v", err)
	}
	return sqlCollectionTypeToProto(sqlType), nil
}

func (s *SQLCollectionStore) GetCollectionTypes(ctx context.Context, orgID int64, collectionIDs []int64) (map[int64]pb.CollectionType, error) {
	if len(collectionIDs) == 0 {
		return make(map[int64]pb.CollectionType), nil
	}

	rows, err := s.GetQueries(ctx).GetCollectionTypesBatch(ctx, sqlc.GetCollectionTypesBatchParams{
		OrgID:         orgID,
		CollectionIds: collectionIDs,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get collection types: %v", err)
	}

	result := make(map[int64]pb.CollectionType, len(collectionIDs))
	for _, row := range rows {
		result[row.ID] = sqlCollectionTypeToProto(row.Type)
	}
	return result, nil
}

func (s *SQLCollectionStore) AddDevicesToCollection(ctx context.Context, orgID int64, collectionID int64, deviceIdentifiers []string) (int64, error) {
	count, err := s.GetQueries(ctx).AddDevicesToCollection(ctx, sqlc.AddDevicesToCollectionParams{
		OrgID:             orgID,
		CollectionID:      collectionID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to add devices to collection: %v", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) RemoveAllDevicesFromCollection(ctx context.Context, orgID int64, collectionID int64) (int64, error) {
	count, err := s.GetQueries(ctx).RemoveAllDevicesFromCollection(ctx, sqlc.RemoveAllDevicesFromCollectionParams{
		CollectionID: collectionID,
		OrgID:        orgID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to remove all devices from collection: %v", err)
	}
	return count, nil
}

func (s *SQLCollectionStore) RemoveDevicesFromCollection(ctx context.Context, orgID int64, collectionID int64, deviceIdentifiers []string) (int64, error) {
	count, err := s.GetQueries(ctx).RemoveDevicesFromCollection(ctx, sqlc.RemoveDevicesFromCollectionParams{
		CollectionID:      collectionID,
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to remove devices from collection: %v", err)
	}
	return count, nil
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
		sqlRows, err := s.GetQueries(ctx).ListCollectionMembersPaginated(ctx, sqlc.ListCollectionMembersPaginatedParams{
			CollectionID: collectionID,
			OrgID:        orgID,
			Limit:        fetchLimit,
		})
		if err != nil {
			return nil, "", fleeterror.NewInternalErrorf("failed to list collection members: %v", err)
		}
		for _, r := range sqlRows {
			rows = append(rows, memberRow{r.ID, r.DeviceIdentifier, r.CreatedAt, r.SlotRow, r.SlotCol})
		}
	} else {
		sqlRows, err := s.GetQueries(ctx).ListCollectionMembersPaginatedAfter(ctx, sqlc.ListCollectionMembersPaginatedAfterParams{
			CollectionID:    collectionID,
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
		rows, err := s.GetQueries(ctx).GetDeviceCollections(ctx, sqlc.GetDeviceCollectionsParams{
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

	rows, err := s.GetQueries(ctx).GetDeviceCollectionsByType(ctx, sqlc.GetDeviceCollectionsByTypeParams{
		DeviceIdentifier: deviceIdentifier,
		OrgID:            orgID,
		Type:             protoCollectionTypeToSQL(collectionType),
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

func (s *SQLCollectionStore) GetGroupLabelsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string][]string, error) {
	if len(deviceIdentifiers) == 0 {
		return make(map[string][]string), nil
	}

	rows, err := s.GetQueries(ctx).GetGroupLabelsForDevices(ctx, sqlc.GetGroupLabelsForDevicesParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get group labels: %v", err)
	}

	result := make(map[string][]string)
	for _, row := range rows {
		result[row.DeviceIdentifier] = append(result[row.DeviceIdentifier], row.Label)
	}
	return result, nil
}

func (s *SQLCollectionStore) GetRackLabelsForDevices(ctx context.Context, orgID int64, deviceIdentifiers []string) (map[string]string, error) {
	if len(deviceIdentifiers) == 0 {
		return make(map[string]string), nil
	}

	rows, err := s.GetQueries(ctx).GetRackLabelsForDevices(ctx, sqlc.GetRackLabelsForDevicesParams{
		OrgID:             orgID,
		DeviceIdentifiers: deviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get rack labels: %v", err)
	}

	result := make(map[string]string)
	for _, row := range rows {
		result[row.DeviceIdentifier] = row.Label
	}
	return result, nil
}

func (s *SQLCollectionStore) SetRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string, row, column int32, orgID int64) error {
	err := s.GetQueries(ctx).SetRackSlotPosition(ctx, sqlc.SetRackSlotPositionParams{
		CollectionID:     collectionID,
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
		CollectionID:     collectionID,
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
		CollectionID: collectionID,
		OrgID:        orgID,
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
    SELECT dcr.collection_id, dcr.rows, dcr.columns
    FROM device_collection_rack dcr
    JOIN device_collection dc ON dcr.collection_id = dc.id
    WHERE dcr.collection_id = ANY($2::bigint[])
      AND dc.org_id = $1
      AND dc.deleted_at IS NULL
),
all_positions AS (
    SELECT rd.collection_id, r.row_num, c.col_num
    FROM rack_dims rd
    CROSS JOIN LATERAL generate_series(0, rd.rows - 1) AS r(row_num)
    CROSS JOIN LATERAL generate_series(0, rd.columns - 1) AS c(col_num)
),
slot_devices AS (
    SELECT rs.collection_id, rs.row, rs.col,
           dcm.device_identifier,
           ds.status AS device_status,
           dp.pairing_status,
           CASE WHEN open_errors.device_id IS NOT NULL THEN true ELSE false END AS has_errors
    FROM rack_slot rs
    JOIN device_collection dc ON rs.collection_id = dc.id AND dc.org_id = $1 AND dc.deleted_at IS NULL
    JOIN device_collection_membership dcm ON rs.collection_id = dcm.collection_id AND rs.device_id = dcm.device_id
    JOIN device d ON dcm.device_id = d.id AND d.deleted_at IS NULL
    JOIN device_pairing dp ON d.id = dp.device_id
        AND dp.pairing_status IN ('PAIRED', 'AUTHENTICATION_NEEDED')
    LEFT JOIN device_status ds ON d.id = ds.device_id
    LEFT JOIN (
        SELECT DISTINCT device_id
        FROM errors
        WHERE errors.org_id = $1
          AND errors.closed_at IS NULL
          AND errors.severity IN (1, 2, 3)
          AND errors.device_id IN (SELECT device_id FROM rack_slot WHERE collection_id = ANY($2::bigint[]))
    ) open_errors ON d.id = open_errors.device_id
    WHERE rs.collection_id = ANY($2::bigint[])
)
SELECT ap.collection_id, ap.row_num AS row, ap.col_num AS col,
    CASE
        -- SlotDeviceStatus enum values (collection.v1.SlotDeviceStatus):
        -- 1 = EMPTY, 2 = HEALTHY, 3 = NEEDS_ATTENTION, 4 = OFFLINE, 5 = SLEEPING
        WHEN sd.device_identifier IS NULL THEN 1
        WHEN sd.device_status = 'OFFLINE' OR sd.device_status IS NULL THEN 4
        WHEN sd.device_status IN ('MAINTENANCE', 'INACTIVE') THEN 5
        WHEN sd.device_status IN ('ERROR', 'NEEDS_MINING_POOL')
             OR sd.pairing_status = 'AUTHENTICATION_NEEDED'
             OR sd.has_errors THEN 3
        ELSE 2
    END AS status
FROM all_positions ap
LEFT JOIN slot_devices sd ON sd.collection_id = ap.collection_id
    AND sd.row = ap.row_num AND sd.col = ap.col_num
ORDER BY ap.collection_id, ap.row_num, ap.col_num`

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

func (s *SQLCollectionStore) ListRackLocations(ctx context.Context, orgID int64) ([]string, error) {
	rows, err := s.GetQueries(ctx).ListRackLocations(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list rack locations: %v", err)
	}

	locations := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Valid {
			locations = append(locations, row.String)
		}
	}
	return locations, nil
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

func protoCollectionTypeToSQL(ct pb.CollectionType) sqlc.CollectionType {
	switch ct {
	case pb.CollectionType_COLLECTION_TYPE_GROUP:
		return sqlc.CollectionTypeGroup
	case pb.CollectionType_COLLECTION_TYPE_RACK:
		return sqlc.CollectionTypeRack
	case pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED:
		// Callers should validate type before reaching this point.
		// Default to group to avoid panicking on unvalidated input.
		return sqlc.CollectionTypeGroup
	default:
		return sqlc.CollectionTypeGroup
	}
}

func sqlCollectionTypeToProto(ct sqlc.CollectionType) pb.CollectionType {
	switch ct {
	case sqlc.CollectionTypeGroup:
		return pb.CollectionType_COLLECTION_TYPE_GROUP
	case sqlc.CollectionTypeRack:
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

func newDeviceCollection(id int64, ct sqlc.CollectionType, label string, description sql.NullString, deviceCount int32, createdAt, updatedAt time.Time) *pb.DeviceCollection {
	return &pb.DeviceCollection{
		Id:          id,
		Type:        sqlCollectionTypeToProto(ct),
		Label:       label,
		Description: fromNullString(description),
		DeviceCount: deviceCount,
		CreatedAt:   timestamppb.New(createdAt),
		UpdatedAt:   timestamppb.New(updatedAt),
	}
}
