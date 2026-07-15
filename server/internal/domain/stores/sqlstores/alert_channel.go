package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
)

type SQLAlertChannelStore struct {
	SQLConnectionManager
}

func NewSQLAlertChannelStore(conn *sql.DB, opts ...ConnectionManagerOption) *SQLAlertChannelStore {
	return &SQLAlertChannelStore{SQLConnectionManager: NewSQLConnectionManager(conn, opts...)}
}

var _ alerts.ChannelStore = (*SQLAlertChannelStore)(nil)
var _ alerts.DeviceIdentityLookup = (*SQLAlertChannelStore)(nil)

func alertChannelToRecord(row sqlc.AlertChannel) alerts.ChannelRecord {
	return alerts.ChannelRecord{
		ID:              row.ID,
		OrganizationID:  row.OrgID,
		Name:            row.Name,
		Kind:            alerts.ChannelKind(row.Kind),
		EncryptedConfig: row.EncryptedConfig,
		ValidationState: alerts.ValidationState(row.ValidationState),
		ValidatedAt:     nullTimeToPtr(row.ValidatedAt),
		ValidationError: row.ValidationError,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func (s *SQLAlertChannelStore) Insert(ctx context.Context, rec alerts.ChannelRecord) (alerts.ChannelRecord, error) {
	row, err := s.GetQueries(ctx).InsertAlertChannel(ctx, sqlc.InsertAlertChannelParams{
		OrgID:           rec.OrganizationID,
		Name:            rec.Name,
		Kind:            string(rec.Kind),
		EncryptedConfig: rec.EncryptedConfig,
		ValidationState: string(rec.ValidationState),
	})
	if err != nil {
		return alerts.ChannelRecord{}, err
	}
	return alertChannelToRecord(row), nil
}

func (s *SQLAlertChannelStore) Update(ctx context.Context, rec alerts.ChannelRecord) (alerts.ChannelRecord, error) {
	row, err := s.GetQueries(ctx).UpdateAlertChannel(ctx, sqlc.UpdateAlertChannelParams{
		ID:              rec.ID,
		OrgID:           rec.OrganizationID,
		Name:            rec.Name,
		Kind:            string(rec.Kind),
		EncryptedConfig: rec.EncryptedConfig,
		ValidationState: string(rec.ValidationState),
		ValidatedAt:     ptrToNullTime(rec.ValidatedAt),
		ValidationError: rec.ValidationError,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return alerts.ChannelRecord{}, alerts.ErrNotFound
	}
	if err != nil {
		return alerts.ChannelRecord{}, err
	}
	return alertChannelToRecord(row), nil
}

func (s *SQLAlertChannelStore) Get(ctx context.Context, orgID, id int64) (alerts.ChannelRecord, error) {
	row, err := s.GetQueries(ctx).GetAlertChannel(ctx, sqlc.GetAlertChannelParams{ID: id, OrgID: orgID})
	if errors.Is(err, sql.ErrNoRows) {
		return alerts.ChannelRecord{}, alerts.ErrNotFound
	}
	if err != nil {
		return alerts.ChannelRecord{}, err
	}
	return alertChannelToRecord(row), nil
}

func (s *SQLAlertChannelStore) GetByName(ctx context.Context, orgID int64, name string) (alerts.ChannelRecord, error) {
	row, err := s.GetQueries(ctx).GetAlertChannelByName(ctx, sqlc.GetAlertChannelByNameParams{OrgID: orgID, Name: name})
	if errors.Is(err, sql.ErrNoRows) {
		return alerts.ChannelRecord{}, alerts.ErrNotFound
	}
	if err != nil {
		return alerts.ChannelRecord{}, err
	}
	return alertChannelToRecord(row), nil
}

func (s *SQLAlertChannelStore) List(ctx context.Context, orgID int64) ([]alerts.ChannelRecord, error) {
	rows, err := s.GetQueries(ctx).ListAlertChannels(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.ChannelRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, alertChannelToRecord(row))
	}
	return out, nil
}

func (s *SQLAlertChannelStore) SoftDelete(ctx context.Context, orgID, id int64) error {
	n, err := s.GetQueries(ctx).SoftDeleteAlertChannel(ctx, sqlc.SoftDeleteAlertChannelParams{ID: id, OrgID: orgID})
	if err != nil {
		return err
	}
	if n == 0 {
		return alerts.ErrNotFound
	}
	return nil
}

func (s *SQLAlertChannelStore) DeviceIdentities(ctx context.Context, orgID int64, deviceIDs []string) (map[string]alerts.DeviceIdentity, error) {
	out := map[string]alerts.DeviceIdentity{}
	if len(deviceIDs) == 0 {
		return out, nil
	}
	rows, err := s.GetQueries(ctx).GetDeviceIdentities(ctx, sqlc.GetDeviceIdentitiesParams{OrgID: orgID, DeviceIds: deviceIDs})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.DeviceIdentifier] = alerts.DeviceIdentity{Name: row.DeviceName, MAC: row.DeviceMac}
	}
	return out, nil
}
