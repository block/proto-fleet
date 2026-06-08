package mqttingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
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
	BrokerTransport         string
	MQTTUsername            string
	MQTTPasswordEncrypted   string
	ContractedCurtailmentKw int32
	// CurtailMode is 'FIXED_KW' or 'FULL_FLEET'.
	CurtailMode string
	// PayloadFormat selects the source's decoder.
	PayloadFormat string
	// ScopeType is 'whole_org', 'site', or 'device_list'.
	ScopeType              string
	ScopeSiteID            *int64
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
	// LastProcessedTargets records every target value already processed at
	// LastTargetAt so same-second QoS redeliveries cannot replay an old target.
	LastProcessedTargets []Target
	LastReceivedAt       time.Time
	LastReceivedBroker   string
	LastEdgeAt           time.Time
	LastEdgeEventUUID    string
	PendingEdge          *PendingEdge
	// LastEmptyFullFleetWatchdogRef is the watchdog external_reference window
	// whose FULL_FLEET dispatch completed with no targets.
	LastEmptyFullFleetWatchdogRef string
}

// PendingEdge is durable retry state for a side effect that was owed or started
// but not yet settled into the source-state row.
type PendingEdge struct {
	Direction      EdgeDirection
	Target         Target
	TargetAt       time.Time
	ReceivedAt     time.Time
	ReceivedBroker string
	PriorEdgeAt    time.Time
	RetryAt        time.Time
}

// StateUpdate replaces a source state row. Zero values map to SQL NULL, which
// lets callers clear pending-edge fields after settlement.
type StateUpdate struct {
	SourceConfigID                int64
	LastTarget                    Target
	LastTargetAt                  time.Time
	LastProcessedTarget           Target
	LastProcessedTargets          []Target
	LastReceivedAt                time.Time
	LastReceivedBroker            string
	LastEdgeAt                    time.Time
	LastEdgeEventUUID             string
	PendingEdge                   *PendingEdge
	LastEmptyFullFleetWatchdogRef string
}

// Store is the data-access interface the subscriber depends on.
type Store interface {
	ListEnabledSources(ctx context.Context) ([]SourceConfig, error)
	GetSourceState(ctx context.Context, sourceConfigID int64) (SourceState, error)
	UpsertSourceState(ctx context.Context, update StateUpdate) error
	// UserCanIngestCurtailment gates service users before emergency curtailment.
	UserCanIngestCurtailment(ctx context.Context, userID, orgID int64) (bool, error)
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
		SourceConfigID:                update.SourceConfigID,
		LastTarget:                    nullStringFromTarget(update.LastTarget),
		LastTargetAt:                  nullTimeFrom(update.LastTargetAt),
		LastProcessedTarget:           nullStringFromTarget(update.LastProcessedTarget),
		LastProcessedTargets:          stringsFromTargets(update.LastProcessedTargets),
		LastReceivedAt:                nullTimeFrom(update.LastReceivedAt),
		LastReceivedBroker:            nullStringFrom(update.LastReceivedBroker),
		LastEdgeAt:                    nullTimeFrom(update.LastEdgeAt),
		LastEdgeEventUuid:             nullUUIDFrom(update.LastEdgeEventUUID),
		LastEmptyFullFleetWatchdogRef: nullStringFrom(update.LastEmptyFullFleetWatchdogRef),
	}
	if update.PendingEdge != nil {
		params.PendingDirection = nullStringFrom(update.PendingEdge.Direction.String())
		params.PendingTarget = nullStringFromTarget(update.PendingEdge.Target)
		params.PendingTargetAt = nullTimeFrom(update.PendingEdge.TargetAt)
		params.PendingReceivedAt = nullTimeFrom(update.PendingEdge.ReceivedAt)
		params.PendingReceivedBroker = nullStringFrom(update.PendingEdge.ReceivedBroker)
		params.PendingPriorEdgeAt = nullTimeFrom(update.PendingEdge.PriorEdgeAt)
		params.PendingRetryAt = nullTimeFrom(update.PendingEdge.RetryAt)
	}
	if err := s.queries.UpsertMQTTSourceState(ctx, params); err != nil {
		return fmt.Errorf("upsert mqtt source state: %w", err)
	}
	return nil
}

func (s *sqlcStore) UserCanIngestCurtailment(ctx context.Context, userID, orgID int64) (bool, error) {
	effective, err := authz.LoadEffectiveTx(ctx, s.queries, userID, orgID)
	if err != nil {
		return false, fmt.Errorf("load effective permissions: %w", err)
	}
	return effective.Has(authz.PermCurtailmentIngest, authz.ResourceContext{}), nil
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
		BrokerTransport:         stringOrDefault(r.BrokerTransport, brokerTransportTCP),
		MQTTUsername:            r.MqttUsername,
		MQTTPasswordEncrypted:   r.MqttPasswordEnc,
		ContractedCurtailmentKw: r.ContractedCurtailmentKw.Int32,
		CurtailMode:             r.CurtailMode,
		PayloadFormat:           r.PayloadFormat,
		ScopeType:               r.ScopeType,
		ScopeSiteID:             int64PtrFromNull(r.ScopeSiteID),
		ScopeDeviceIdentifiers:  r.ScopeDeviceIdentifiers,
		StalenessThreshold:      time.Duration(int32OrDefault(r.StalenessThresholdSec, defaultStalenessThresholdSec)) * time.Second,
		MinCurtailedDuration:    time.Duration(int32OrDefault(r.MinCurtailedDurationSec, defaultMinCurtailedDurationSec)) * time.Second,
		Enabled:                 r.Enabled,
	}
}

func sourceStateFromRow(r sqlc.CurtailmentMqttSourceState) SourceState {
	return SourceState{
		SourceConfigID:       r.SourceConfigID,
		LastTarget:           targetFromNullString(r.LastTarget),
		LastTargetAt:         timeFromNullTime(r.LastTargetAt),
		LastProcessedTarget:  targetFromNullString(r.LastProcessedTarget),
		LastProcessedTargets: targetsFromStrings(r.LastProcessedTargets),
		LastReceivedAt:       timeFromNullTime(r.LastReceivedAt),
		LastReceivedBroker:   stringFromNullString(r.LastReceivedBroker),
		LastEdgeAt:           timeFromNullTime(r.LastEdgeAt),
		LastEdgeEventUUID:    stringFromNullUUID(r.LastEdgeEventUuid),
		PendingEdge: pendingEdgeFromRow(
			r.PendingDirection,
			r.PendingTarget,
			r.PendingTargetAt,
			r.PendingReceivedAt,
			r.PendingReceivedBroker,
			r.PendingPriorEdgeAt,
			r.PendingRetryAt,
		),
		LastEmptyFullFleetWatchdogRef: stringFromNullString(r.LastEmptyFullFleetWatchdogRef),
	}
}
