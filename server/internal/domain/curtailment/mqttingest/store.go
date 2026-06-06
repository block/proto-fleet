package mqttingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
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
	CreatedAt              time.Time
	UpdatedAt              time.Time
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

// SettingsStore is the CRUD/read surface for operator-managed MQTT sources.
type SettingsStore interface {
	ListSourceConfigsByOrg(ctx context.Context, orgID int64) ([]SourceConfig, error)
	ListSourceStatesByOrg(ctx context.Context, orgID int64) ([]SourceState, error)
	GetSourceConfigByOrg(ctx context.Context, orgID, sourceID int64) (SourceConfig, error)
	CreateSourceConfig(ctx context.Context, source SourceConfig) (SourceConfig, error)
	UpdateSourceConfig(ctx context.Context, source SourceConfig) (SourceConfig, error)
	SetSourceConfigEnabled(ctx context.Context, orgID, sourceID int64, enabled bool) (SourceConfig, error)
	DeleteDisabledSourceConfig(ctx context.Context, orgID, sourceID int64) error
	UserCanIngestCurtailment(ctx context.Context, userID, orgID int64) (bool, error)
}

// ErrSourceStateNotFound means cold start.
var ErrSourceStateNotFound = errors.New("mqttingest: source state not found")

// ErrSourceConfigNotFound means a settings source does not exist in the org.
var ErrSourceConfigNotFound = errors.New("mqttingest: source config not found")

// ErrSourceConfigNameExists means a source name is already used in the org.
var ErrSourceConfigNameExists = errors.New("mqttingest: source config name exists")

// ErrSourceConfigDeleteBlocked means a source must be disabled before delete.
var ErrSourceConfigDeleteBlocked = errors.New("mqttingest: enabled source cannot be deleted")

const mqttSourceConfigOrgNameConstraint = "uq_curtailment_mqtt_source_config_org_name"

type sqlcStore struct {
	queries *sqlc.Queries
}

// NewSQLCStore returns a Store backed by sqlc.
func NewSQLCStore(queries *sqlc.Queries) Store {
	return &sqlcStore{queries: queries}
}

// NewSQLCSettingsStore returns a settings CRUD store backed by sqlc.
func NewSQLCSettingsStore(queries *sqlc.Queries) SettingsStore {
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

func (s *sqlcStore) ListSourceConfigsByOrg(ctx context.Context, orgID int64) ([]SourceConfig, error) {
	rows, err := s.queries.ListMQTTSourceConfigsByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list mqtt source configs: %w", err)
	}
	out := make([]SourceConfig, len(rows))
	for i, r := range rows {
		out[i] = sourceConfigFromRow(r)
	}
	return out, nil
}

func (s *sqlcStore) ListSourceStatesByOrg(ctx context.Context, orgID int64) ([]SourceState, error) {
	rows, err := s.queries.ListMQTTSourceStatesByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list mqtt source states: %w", err)
	}
	out := make([]SourceState, len(rows))
	for i, r := range rows {
		out[i] = sourceStateFromRow(r)
	}
	return out, nil
}

func (s *sqlcStore) GetSourceConfigByOrg(ctx context.Context, orgID, sourceID int64) (SourceConfig, error) {
	row, err := s.queries.GetMQTTSourceConfigByOrg(ctx, sqlc.GetMQTTSourceConfigByOrgParams{
		ID:             sourceID,
		OrganizationID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SourceConfig{}, ErrSourceConfigNotFound
		}
		return SourceConfig{}, fmt.Errorf("get mqtt source config: %w", err)
	}
	return sourceConfigFromRow(row), nil
}

func (s *sqlcStore) CreateSourceConfig(ctx context.Context, source SourceConfig) (SourceConfig, error) {
	row, err := s.queries.InsertMQTTSourceConfig(ctx, insertSourceConfigParams(source))
	if err != nil {
		return SourceConfig{}, sourceConfigPersistError("insert mqtt source config", err)
	}
	return sourceConfigFromRow(row), nil
}

func (s *sqlcStore) UpdateSourceConfig(ctx context.Context, source SourceConfig) (SourceConfig, error) {
	params := updateSourceConfigParams(source)
	row, err := s.queries.UpdateMQTTSourceConfig(ctx, params)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SourceConfig{}, ErrSourceConfigNotFound
		}
		return SourceConfig{}, sourceConfigPersistError("update mqtt source config", err)
	}
	return sourceConfigFromRow(row), nil
}

func (s *sqlcStore) SetSourceConfigEnabled(ctx context.Context, orgID, sourceID int64, enabled bool) (SourceConfig, error) {
	row, err := s.queries.SetMQTTSourceConfigEnabled(ctx, sqlc.SetMQTTSourceConfigEnabledParams{
		ID:             sourceID,
		OrganizationID: orgID,
		Enabled:        enabled,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SourceConfig{}, ErrSourceConfigNotFound
		}
		return SourceConfig{}, fmt.Errorf("set mqtt source config enabled: %w", err)
	}
	return sourceConfigFromRow(row), nil
}

func (s *sqlcStore) DeleteDisabledSourceConfig(ctx context.Context, orgID, sourceID int64) error {
	rows, err := s.queries.DeleteDisabledMQTTSourceConfigByOrg(ctx, sqlc.DeleteDisabledMQTTSourceConfigByOrgParams{
		ID:             sourceID,
		OrganizationID: orgID,
	})
	if err != nil {
		return fmt.Errorf("delete mqtt source config: %w", err)
	}
	if rows > 0 {
		return nil
	}
	current, getErr := s.GetSourceConfigByOrg(ctx, orgID, sourceID)
	if getErr != nil {
		return getErr
	}
	if current.Enabled {
		return ErrSourceConfigDeleteBlocked
	}
	return ErrSourceConfigNotFound
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

func sourceConfigPersistError(prefix string, err error) error {
	if isSourceConfigNameUniqueViolation(err) {
		return ErrSourceConfigNameExists
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func isSourceConfigNameUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == db.PGUniqueViolation &&
		pgErr.ConstraintName == mqttSourceConfigOrgNameConstraint
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
		CreatedAt:               r.CreatedAt,
		UpdatedAt:               r.UpdatedAt,
	}
}

func insertSourceConfigParams(source SourceConfig) sqlc.InsertMQTTSourceConfigParams {
	return sqlc.InsertMQTTSourceConfigParams{
		OrganizationID:          source.OrganizationID,
		ServiceUserID:           source.ServiceUserID,
		SourceName:              source.SourceName,
		Topic:                   source.Topic,
		BrokerPrimaryHost:       source.BrokerPrimaryHost,
		BrokerSecondaryHost:     source.BrokerSecondaryHost,
		BrokerPort:              nullInt32FromDefault(source.BrokerPort, defaultBrokerPort),
		BrokerTransport:         source.BrokerTransport,
		MqttUsername:            source.MQTTUsername,
		MqttPasswordEnc:         source.MQTTPasswordEncrypted,
		ContractedCurtailmentKw: nullPositiveInt32From(source.ContractedCurtailmentKw),
		CurtailMode:             source.CurtailMode,
		PayloadFormat:           source.PayloadFormat,
		ScopeType:               source.ScopeType,
		ScopeSiteID:             nullInt64Ptr(source.ScopeSiteID),
		ScopeDeviceIdentifiers:  nilIfEmptyStrings(source.ScopeDeviceIdentifiers),
		StalenessThresholdSec:   nullDurationSecondsFromDefault(source.StalenessThreshold, defaultStalenessThresholdSec),
		MinCurtailedDurationSec: nullDurationSecondsFromDefault(source.MinCurtailedDuration, defaultMinCurtailedDurationSec),
		Enabled:                 source.Enabled,
	}
}

func updateSourceConfigParams(source SourceConfig) sqlc.UpdateMQTTSourceConfigParams {
	return sqlc.UpdateMQTTSourceConfigParams{
		ID:                      source.ID,
		OrganizationID:          source.OrganizationID,
		ServiceUserID:           source.ServiceUserID,
		SourceName:              source.SourceName,
		Topic:                   source.Topic,
		BrokerPrimaryHost:       source.BrokerPrimaryHost,
		BrokerSecondaryHost:     source.BrokerSecondaryHost,
		BrokerPort:              nullInt32FromDefault(source.BrokerPort, defaultBrokerPort),
		BrokerTransport:         source.BrokerTransport,
		MqttUsername:            source.MQTTUsername,
		MqttPasswordEnc:         source.MQTTPasswordEncrypted,
		ContractedCurtailmentKw: nullPositiveInt32From(source.ContractedCurtailmentKw),
		CurtailMode:             source.CurtailMode,
		PayloadFormat:           source.PayloadFormat,
		ScopeType:               source.ScopeType,
		ScopeSiteID:             nullInt64Ptr(source.ScopeSiteID),
		ScopeDeviceIdentifiers:  nilIfEmptyStrings(source.ScopeDeviceIdentifiers),
		StalenessThresholdSec:   nullDurationSecondsFromDefault(source.StalenessThreshold, defaultStalenessThresholdSec),
		MinCurtailedDurationSec: nullDurationSecondsFromDefault(source.MinCurtailedDuration, defaultMinCurtailedDurationSec),
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

func nullInt32FromDefault(v, def int32) sql.NullInt32 {
	if v == 0 || v == def {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: v, Valid: true}
}

func nullPositiveInt32From(v int32) sql.NullInt32 {
	if v <= 0 {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: v, Valid: true}
}

func nullInt64Ptr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullDurationSecondsFromDefault(v time.Duration, def int32) sql.NullInt32 {
	if v <= 0 {
		return sql.NullInt32{}
	}
	const maxInt32 = int64(1<<31 - 1)
	seconds := int64(v / time.Second)
	if seconds > maxInt32 {
		seconds = maxInt32
	}
	if seconds == int64(def) {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(seconds), Valid: true} // #nosec G115 -- bounds-checked above
}

func nilIfEmptyStrings(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	return v
}
