package sqlstores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/collection/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
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

func (s *SQLCollectionStore) CreateRackExtension(ctx context.Context, collectionID int64, location string, rows, columns int32) error {
	err := s.GetQueries(ctx).CreateRackExtension(ctx, sqlc.CreateRackExtensionParams{
		CollectionID: collectionID,
		Location:     toNullString(location),
		Rows:         rows,
		Columns:      columns,
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

func (s *SQLCollectionStore) GetRackInfo(ctx context.Context, collectionID int64) (*pb.RackInfo, error) {
	row, err := s.GetQueries(ctx).GetRackInfo(ctx, collectionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to get rack info: %v", err)
	}

	rackInfo := &pb.RackInfo{
		Rows:    row.Rows,
		Columns: row.Columns,
	}
	if row.Location.Valid {
		rackInfo.Location = &row.Location.String
	}
	return rackInfo, nil
}

func (s *SQLCollectionStore) UpdateCollection(ctx context.Context, orgID int64, collectionID int64, label, description *string) error {
	q := s.GetQueries(ctx)

	switch {
	case label != nil && description != nil:
		return q.UpdateCollectionLabelAndDescription(ctx, sqlc.UpdateCollectionLabelAndDescriptionParams{
			Label:       *label,
			Description: toNullString(*description),
			ID:          collectionID,
			OrgID:       orgID,
		})
	case label != nil:
		return q.UpdateCollectionLabel(ctx, sqlc.UpdateCollectionLabelParams{
			Label: *label,
			ID:    collectionID,
			OrgID: orgID,
		})
	case description != nil:
		return q.UpdateCollectionDescription(ctx, sqlc.UpdateCollectionDescriptionParams{
			Description: toNullString(*description),
			ID:          collectionID,
			OrgID:       orgID,
		})
	default:
		return nil
	}
}

func (s *SQLCollectionStore) UpdateRackInfo(ctx context.Context, collectionID int64, location string, rows, columns int32) error {
	err := s.GetQueries(ctx).UpdateRackInfo(ctx, sqlc.UpdateRackInfoParams{
		Location:     toNullString(location),
		Rows:         rows,
		Columns:      columns,
		CollectionID: collectionID,
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

func (s *SQLCollectionStore) ListCollections(ctx context.Context, orgID int64, collectionType pb.CollectionType, pageSize int32, pageToken string) ([]*pb.DeviceCollection, string, error) {
	cursor, err := decodeCollectionCursor(pageToken)
	if err != nil {
		return nil, "", err
	}

	// Fetch one extra row to determine if there's a next page
	fetchLimit := pageSize + 1

	type collectionRow struct {
		ID          int64
		Type        sqlc.CollectionType
		Label       string
		Description sql.NullString
		DeviceCount int32
		CreatedAt   time.Time
		UpdatedAt   time.Time
	}

	var rows []collectionRow

	if collectionType == pb.CollectionType_COLLECTION_TYPE_UNSPECIFIED {
		if cursor == nil {
			sqlRows, err := s.GetQueries(ctx).ListCollectionsPaginated(ctx, sqlc.ListCollectionsPaginatedParams{
				OrgID: orgID,
				Limit: fetchLimit,
			})
			if err != nil {
				return nil, "", fleeterror.NewInternalErrorf("failed to list collections: %v", err)
			}
			for _, r := range sqlRows {
				rows = append(rows, collectionRow{r.ID, r.Type, r.Label, r.Description, r.DeviceCount, r.CreatedAt, r.UpdatedAt})
			}
		} else {
			sqlRows, err := s.GetQueries(ctx).ListCollectionsPaginatedAfter(ctx, sqlc.ListCollectionsPaginatedAfterParams{
				OrgID:       orgID,
				Limit:       fetchLimit,
				CursorLabel: cursor.Label,
				CursorID:    cursor.ID,
			})
			if err != nil {
				return nil, "", fleeterror.NewInternalErrorf("failed to list collections: %v", err)
			}
			for _, r := range sqlRows {
				rows = append(rows, collectionRow{r.ID, r.Type, r.Label, r.Description, r.DeviceCount, r.CreatedAt, r.UpdatedAt})
			}
		}
	} else {
		sqlType := protoCollectionTypeToSQL(collectionType)
		if cursor == nil {
			sqlRows, err := s.GetQueries(ctx).ListCollectionsByTypePaginated(ctx, sqlc.ListCollectionsByTypePaginatedParams{
				OrgID: orgID,
				Type:  sqlType,
				Limit: fetchLimit,
			})
			if err != nil {
				return nil, "", fleeterror.NewInternalErrorf("failed to list collections by type: %v", err)
			}
			for _, r := range sqlRows {
				rows = append(rows, collectionRow{r.ID, r.Type, r.Label, r.Description, r.DeviceCount, r.CreatedAt, r.UpdatedAt})
			}
		} else {
			sqlRows, err := s.GetQueries(ctx).ListCollectionsByTypePaginatedAfter(ctx, sqlc.ListCollectionsByTypePaginatedAfterParams{
				OrgID:       orgID,
				Type:        sqlType,
				Limit:       fetchLimit,
				CursorLabel: cursor.Label,
				CursorID:    cursor.ID,
			})
			if err != nil {
				return nil, "", fleeterror.NewInternalErrorf("failed to list collections by type: %v", err)
			}
			for _, r := range sqlRows {
				rows = append(rows, collectionRow{r.ID, r.Type, r.Label, r.Description, r.DeviceCount, r.CreatedAt, r.UpdatedAt})
			}
		}
	}

	var nextPageToken string
	if len(rows) > int(pageSize) {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		nextPageToken = encodeCollectionCursor(&collectionCursor{Label: last.Label, ID: last.ID})
	}

	result := make([]*pb.DeviceCollection, len(rows))
	for i, row := range rows {
		result[i] = newDeviceCollection(row.ID, row.Type, row.Label, row.Description, row.DeviceCount, row.CreatedAt, row.UpdatedAt)
	}
	return result, nextPageToken, nil
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

func (s *SQLCollectionStore) SetRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string, row, column int32) error {
	err := s.GetQueries(ctx).SetRackSlotPosition(ctx, sqlc.SetRackSlotPositionParams{
		CollectionID:     collectionID,
		DeviceIdentifier: deviceIdentifier,
		Row:              row,
		Col:              column,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to set rack slot position: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) ClearRackSlotPosition(ctx context.Context, collectionID int64, deviceIdentifier string) error {
	err := s.GetQueries(ctx).ClearRackSlotPosition(ctx, sqlc.ClearRackSlotPositionParams{
		CollectionID:     collectionID,
		DeviceIdentifier: deviceIdentifier,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to clear rack slot position: %v", err)
	}
	return nil
}

func (s *SQLCollectionStore) GetRackSlots(ctx context.Context, collectionID int64) ([]*pb.RackSlot, error) {
	rows, err := s.GetQueries(ctx).GetRackSlots(ctx, collectionID)
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
