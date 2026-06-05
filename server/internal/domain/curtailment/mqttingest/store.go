package mqttingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
)

// SourceConfig is one MQTT source row in domain form.
type SourceConfig struct {
	ID                      int64
	OrganizationID          int64
	ServiceUserID           int64
	SourceName              string
	Topic                   string
	BrokerPrimaryHost       string
	BrokerSecondaryHost     string
	BrokerPort              int32
	MQTTUsername            string
	MQTTPasswordEncrypted   string
	ContractedCurtailmentKw int32
	// CurtailMode is 'FIXED_KW' or 'FULL_FLEET'.
	CurtailMode string
	// PayloadFormat selects the source's decoder.
	PayloadFormat string
	// ScopeType is 'whole_org' or 'device_list'.
	ScopeType              string
	ScopeDeviceIdentifiers []string
	StalenessThreshold     time.Duration
	MinCurtailedDuration   time.Duration
	Enabled                bool
}

// SourceState is the persisted state for one source.
type SourceState struct {
	SourceConfigID int64
	LastTarget     Target
	LastTargetAt   time.Time
	// LastProcessedTarget pairs with LastTargetAt for duplicate suppression.
	LastProcessedTarget Target
	LastReceivedAt      time.Time
	LastReceivedBroker  string
	LastEdgeAt          time.Time
	LastEdgeEventUUID   string
}

// WatchdogRow is the projection ListSourcesForWatchdog returns.
type WatchdogRow struct {
	SourceConfigID     int64
	SourceName         string
	OrganizationID     int64
	StalenessThreshold time.Duration
	LastTarget         Target
	LastReceivedAt     time.Time
	LastEdgeEventUUID  string
}

// StateUpdate patches source state; nil pointers leave columns unchanged.
type StateUpdate struct {
	SourceConfigID      int64
	LastTarget          *Target
	LastTargetAt        *time.Time
	LastProcessedTarget *Target
	LastReceivedAt      *time.Time
	LastReceivedBroker  *string
	LastEdgeAt          *time.Time
	LastEdgeEventUUID   *string
}

// Store is the data-access interface the subscriber depends on.
type Store interface {
	ListEnabledSources(ctx context.Context) ([]SourceConfig, error)
	GetSourceState(ctx context.Context, sourceConfigID int64) (SourceState, error)
	UpsertSourceState(ctx context.Context, update StateUpdate) error
	ListSourcesForWatchdog(ctx context.Context) ([]WatchdogRow, error)
	// UserBelongsToOrg gates service users before emergency curtailment.
	UserBelongsToOrg(ctx context.Context, userID, orgID int64) (bool, error)
}

// ErrSourceStateNotFound means cold start.
var ErrSourceStateNotFound = errors.New("mqttingest: source state not found")

type sqlcStore struct {
	queries *sqlc.Queries
}

// NewSQLCStore returns a Store backed by sqlc.
func NewSQLCStore(queries *sqlc.Queries) Store {
	return &sqlcStore{queries: queries}
}

func (s *sqlcStore) ListEnabledSources(ctx context.Context) ([]SourceConfig, error) {
	rows, err := s.queries.ListEnabledMQTTSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled mqtt sources: %w", err)
	}
	out := make([]SourceConfig, len(rows))
	for i, r := range rows {
		out[i] = sourceConfigFromRow(r)
	}
	return out, nil
}

func (s *sqlcStore) GetSourceState(ctx context.Context, sourceConfigID int64) (SourceState, error) {
	row, err := s.queries.GetMQTTSourceStateByID(ctx, sourceConfigID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SourceState{}, ErrSourceStateNotFound
		}
		return SourceState{}, fmt.Errorf("get mqtt source state: %w", err)
	}
	return sourceStateFromRow(row), nil
}

func (s *sqlcStore) UpsertSourceState(ctx context.Context, update StateUpdate) error {
	params := sqlc.UpsertMQTTSourceStateParams{
		SourceConfigID: update.SourceConfigID,
	}
	if update.LastTarget != nil {
		params.LastTarget = nullStringFromTarget(*update.LastTarget)
	}
	if update.LastTargetAt != nil {
		params.LastTargetAt = nullTimeFrom(*update.LastTargetAt)
	}
	if update.LastProcessedTarget != nil {
		params.LastProcessedTarget = nullStringFromTarget(*update.LastProcessedTarget)
	}
	if update.LastReceivedAt != nil {
		params.LastReceivedAt = nullTimeFrom(*update.LastReceivedAt)
	}
	if update.LastReceivedBroker != nil {
		params.LastReceivedBroker = nullStringFrom(*update.LastReceivedBroker)
	}
	if update.LastEdgeAt != nil {
		params.LastEdgeAt = nullTimeFrom(*update.LastEdgeAt)
	}
	if update.LastEdgeEventUUID != nil {
		params.LastEdgeEventUuid = nullUUIDFrom(*update.LastEdgeEventUUID)
	}
	if err := s.queries.UpsertMQTTSourceState(ctx, params); err != nil {
		return fmt.Errorf("upsert mqtt source state: %w", err)
	}
	return nil
}

func (s *sqlcStore) ListSourcesForWatchdog(ctx context.Context) ([]WatchdogRow, error) {
	rows, err := s.queries.ListMQTTSourcesForWatchdog(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mqtt sources for watchdog: %w", err)
	}
	out := make([]WatchdogRow, len(rows))
	for i, r := range rows {
		out[i] = WatchdogRow{
			SourceConfigID:     r.SourceConfigID,
			SourceName:         r.SourceName,
			OrganizationID:     r.OrganizationID,
			StalenessThreshold: time.Duration(int32OrDefault(r.StalenessThresholdSec, defaultStalenessThresholdSec)) * time.Second,
			LastTarget:         targetFromNullString(r.LastTarget),
			LastReceivedAt:     timeFromNullTime(r.LastReceivedAt),
			LastEdgeEventUUID:  stringFromNullUUID(r.LastEdgeEventUuid),
		}
	}
	return out, nil
}

func (s *sqlcStore) UserBelongsToOrg(ctx context.Context, userID, orgID int64) (bool, error) {
	// GetUserRoleName alone would pass a soft-deleted user whose org link
	// remains, so verify the user row first.
	if _, err := s.queries.GetUserById(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("get user: %w", err)
	}
	if _, err := s.queries.GetUserRoleName(ctx, sqlc.GetUserRoleNameParams{
		UserID:         userID,
		OrganizationID: orgID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("get user role in org: %w", err)
	}
	return true, nil
}

const (
	defaultBrokerPort              int32 = 1883
	defaultStalenessThresholdSec   int32 = 240
	defaultMinCurtailedDurationSec int32 = 600
)

func sourceConfigFromRow(r sqlc.CurtailmentMqttSourceConfig) SourceConfig {
	return SourceConfig{
		ID:                      r.ID,
		OrganizationID:          r.OrganizationID,
		ServiceUserID:           r.ServiceUserID,
		SourceName:              r.SourceName,
		Topic:                   r.Topic,
		BrokerPrimaryHost:       r.BrokerPrimaryHost,
		BrokerSecondaryHost:     r.BrokerSecondaryHost,
		BrokerPort:              int32OrDefault(r.BrokerPort, defaultBrokerPort),
		MQTTUsername:            r.MqttUsername,
		MQTTPasswordEncrypted:   r.MqttPasswordEnc,
		ContractedCurtailmentKw: r.ContractedCurtailmentKw.Int32,
		CurtailMode:             r.CurtailMode,
		PayloadFormat:           r.PayloadFormat,
		ScopeType:               r.ScopeType,
		ScopeDeviceIdentifiers:  r.ScopeDeviceIdentifiers,
		StalenessThreshold:      time.Duration(int32OrDefault(r.StalenessThresholdSec, defaultStalenessThresholdSec)) * time.Second,
		MinCurtailedDuration:    time.Duration(int32OrDefault(r.MinCurtailedDurationSec, defaultMinCurtailedDurationSec)) * time.Second,
		Enabled:                 r.Enabled,
	}
}

func sourceStateFromRow(r sqlc.CurtailmentMqttSourceState) SourceState {
	return SourceState{
		SourceConfigID:      r.SourceConfigID,
		LastTarget:          targetFromNullString(r.LastTarget),
		LastTargetAt:        timeFromNullTime(r.LastTargetAt),
		LastProcessedTarget: targetFromNullString(r.LastProcessedTarget),
		LastReceivedAt:      timeFromNullTime(r.LastReceivedAt),
		LastReceivedBroker:  stringFromNullString(r.LastReceivedBroker),
		LastEdgeAt:          timeFromNullTime(r.LastEdgeAt),
		LastEdgeEventUUID:   stringFromNullUUID(r.LastEdgeEventUuid),
	}
}
