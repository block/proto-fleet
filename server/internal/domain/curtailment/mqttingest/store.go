package mqttingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
)

// SourceConfig is the domain shape of a single MQTT source row. The
// password stays encrypted here; the subscriber decrypts it only when
// connecting, so plaintext stays bounded to the worker.
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
	// CurtailMode is 'FIXED_KW' (shed ContractedCurtailmentKw) or 'FULL_FLEET'
	// (curtail every eligible device in scope) — matching the curtailment Mode
	// enum. ContractedCurtailmentKw is 0/unused for FULL_FLEET; the driver
	// builds the request mode from this.
	CurtailMode string
	// PayloadFormat selects the PayloadDecoder for this source's wire format
	// (e.g. 'target_timestamp'); resolved against the decoder registry at start.
	PayloadFormat string
	// ScopeType is 'whole_org' or 'device_list'; ScopeDeviceIdentifiers holds
	// the devices for 'device_list' (empty for 'whole_org'). The driver builds
	// the curtailment Scope from these.
	ScopeType              string
	ScopeDeviceIdentifiers []string
	StalenessThreshold     time.Duration
	MinCurtailedDuration   time.Duration
	Enabled                bool
}

// SourceState is the domain shape of a curtailment_mqtt_source_state
// row. Nullable columns surface as zero-value time.Time / TargetUnknown.
type SourceState struct {
	SourceConfigID int64
	LastTarget     Target
	LastTargetAt   time.Time
	// LastProcessedTarget is the target of the payload that last advanced
	// LastTargetAt (may differ from LastTarget after a debounced flip). The
	// dedup guard suppresses a redelivery (same stamp AND same target) while
	// still acting on a genuine same-second flip (wire stamps are
	// seconds-precision). Persisted so the guard survives a restart.
	LastProcessedTarget Target
	LastReceivedAt      time.Time
	LastReceivedBroker  string
	LastEdgeAt          time.Time
	LastEdgeEventUUID   string
}

// WatchdogRow is the projection ListSourcesForWatchdog returns —
// just the columns the watchdog needs, joined across config + state.
type WatchdogRow struct {
	SourceConfigID     int64
	SourceName         string
	OrganizationID     int64
	StalenessThreshold time.Duration
	LastTarget         Target
	LastReceivedAt     time.Time
	LastEdgeEventUUID  string
}

// StateUpdate is the patch shape the subscriber writes after each
// message receive or edge dispatch. Nil pointers leave the existing
// column value untouched (mirrors the sqlc COALESCE upsert behavior).
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

// Store is the data-access interface the subscriber depends on. The
// production impl wraps sqlc; tests inject a fake.
type Store interface {
	ListEnabledSources(ctx context.Context) ([]SourceConfig, error)
	GetSourceState(ctx context.Context, sourceConfigID int64) (SourceState, error)
	UpsertSourceState(ctx context.Context, update StateUpdate) error
	ListSourcesForWatchdog(ctx context.Context) ([]WatchdogRow, error)
	// UserBelongsToOrg reports whether userID is a (non-deleted) member of
	// orgID. The subscriber gates each source's service user through this
	// before starting its worker, so a misconfigured row can't drive
	// emergency curtailment for an org the user doesn't belong to.
	UserBelongsToOrg(ctx context.Context, userID, orgID int64) (bool, error)
}

// ErrSourceStateNotFound is returned by GetSourceState when no state
// row exists for the source — i.e., cold start. Callers treat this
// as "no observations yet" rather than an error.
var ErrSourceStateNotFound = errors.New("mqttingest: source state not found")

// sqlcStore is the production Store implementation backed by
// generated sqlc queries.
type sqlcStore struct {
	queries *sqlc.Queries
}

// NewSQLCStore returns a Store that reads/writes via the supplied
// sqlc.Queries handle. The caller owns transaction scoping.
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
	// Source-config defaults applied when a row leaves these columns NULL.
	// Kept in code (not as DB column defaults) so they're tunable without a
	// migration and reviewed alongside the logic that consumes them.
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
