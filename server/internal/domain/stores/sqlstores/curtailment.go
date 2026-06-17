package sqlstores

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sqlc-dev/pqtype"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// pgErrCodeForeignKeyViolation is PostgreSQL's SQLSTATE for foreign_key_violation.
const pgErrCodeForeignKeyViolation = "23503"

// Partial-unique-index names used to map a unique-violation into a typed
// sentinel (replay path or AlreadyExists) instead of leaking Internal.
const deviceNonTerminalUniqueIndex = "uq_curtailment_target_one_non_terminal_per_device"

const (
	idempotencyKeyUniqueIndex    = "uq_curtailment_event_idempotency"
	externalReferenceUniqueIndex = "uq_curtailment_event_external_ref"
	automationRuleOrgNameUnique  = "uq_curtailment_automation_rule_org_name"
)

func mapOrgConfigError(err error, orgID int64) error {
	if err == nil {
		return nil
	}
	// EnsureCurtailmentOrgConfig requires organization.deleted_at IS NULL;
	// ErrNoRows means soft-deleted/unknown.
	if errors.Is(err, sql.ErrNoRows) {
		return fleeterror.NewNotFoundErrorf("organization %d not found", orgID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeForeignKeyViolation {
		return fleeterror.NewNotFoundErrorf("organization %d not found", orgID)
	}
	return fleeterror.NewInternalErrorf("failed to get curtailment org config: %v", err)
}

var _ interfaces.CurtailmentStore = &SQLCurtailmentStore{}
var _ interfaces.ResponseProfileStore = &SQLCurtailmentStore{}
var _ interfaces.AutomationStore = &SQLCurtailmentStore{}

type SQLCurtailmentStore struct {
	SQLConnectionManager
}

func NewSQLCurtailmentStore(conn *sql.DB) *SQLCurtailmentStore {
	return &SQLCurtailmentStore{
		SQLConnectionManager: NewSQLConnectionManager(conn),
	}
}

func (s *SQLCurtailmentStore) GetOrgConfig(ctx context.Context, orgID int64) (*models.OrgConfig, error) {
	// Ensure-then-read seeds post-migration tenants. One retry covers a
	// READ COMMITTED race where the loser's snapshot missed the winner's
	// INSERT; the deletion case maps to NotFound via mapOrgConfigError.
	row, err := s.GetQueries(ctx).EnsureCurtailmentOrgConfig(ctx, orgID)
	if errors.Is(err, sql.ErrNoRows) {
		row, err = s.GetQueries(ctx).EnsureCurtailmentOrgConfig(ctx, orgID)
	}
	if err != nil {
		return nil, mapOrgConfigError(err, orgID)
	}
	return &models.OrgConfig{
		OrgID:                 row.OrgID,
		MaxDurationDefaultSec: row.MaxDurationDefaultSec,
		CandidateMinPowerW:    row.CandidateMinPowerW,
		PostEventCooldownSec:  row.PostEventCooldownSec,
	}, nil
}

func (s *SQLCurtailmentStore) UpdateOrgConfigPostEventCooldown(ctx context.Context, orgID int64, cooldownSec int32) (*models.OrgConfig, error) {
	if _, err := s.GetOrgConfig(ctx, orgID); err != nil {
		return nil, err
	}
	row, err := s.GetQueries(ctx).UpdateCurtailmentOrgConfigPostEventCooldown(ctx, sqlc.UpdateCurtailmentOrgConfigPostEventCooldownParams{
		OrgID:                orgID,
		PostEventCooldownSec: cooldownSec,
	})
	if err != nil {
		return nil, mapOrgConfigError(err, orgID)
	}
	return &models.OrgConfig{
		OrgID:                 row.OrgID,
		MaxDurationDefaultSec: row.MaxDurationDefaultSec,
		CandidateMinPowerW:    row.CandidateMinPowerW,
		PostEventCooldownSec:  row.PostEventCooldownSec,
	}, nil
}

func (s *SQLCurtailmentStore) ListActiveCurtailedDevices(ctx context.Context, orgID int64) ([]string, error) {
	devices, err := s.GetQueries(ctx).ListActiveCurtailedDevicesByOrg(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list active curtailed devices: %v", err)
	}
	return devices, nil
}

func (s *SQLCurtailmentStore) ListRecentlyResolvedCurtailedDevices(ctx context.Context, orgID int64, cooldownSec int32) ([]string, error) {
	devices, err := s.GetQueries(ctx).ListRecentlyResolvedCurtailedDevicesByOrg(ctx, sqlc.ListRecentlyResolvedCurtailedDevicesByOrgParams{
		OrgID:       orgID,
		CooldownSec: cooldownSec,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list recently resolved curtailed devices: %v", err)
	}
	return devices, nil
}

func (s *SQLCurtailmentStore) SiteBelongsToOrg(ctx context.Context, orgID, siteID int64) (bool, error) {
	belongs, err := s.GetQueries(ctx).SiteBelongsToOrg(ctx, sqlc.SiteBelongsToOrgParams{ID: siteID, OrgID: orgID})
	if err != nil {
		return false, fleeterror.NewInternalErrorf("failed to check site ownership: %v", err)
	}
	return belongs, nil
}

func (s *SQLCurtailmentStore) ListResponseProfiles(ctx context.Context, orgID int64) ([]*models.ResponseProfile, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentResponseProfilesByOrg(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment response profiles: %v", err)
	}
	out := make([]*models.ResponseProfile, len(rows))
	for i, row := range rows {
		out[i] = responseProfileFromRow(row)
	}
	return out, nil
}

func (s *SQLCurtailmentStore) GetResponseProfile(ctx context.Context, orgID, profileID int64) (*models.ResponseProfile, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentResponseProfileByOrg(ctx, sqlc.GetCurtailmentResponseProfileByOrgParams{
		ID:    profileID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment response profile not found: %d", profileID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment response profile: %v", err)
	}
	return responseProfileFromRow(row), nil
}

func (s *SQLCurtailmentStore) CreateResponseProfile(ctx context.Context, profile models.ResponseProfile) (*models.ResponseProfile, error) {
	row, err := db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (sqlc.CurtailmentResponseProfile, error) {
		if err := lockResponseProfileSitesForWrite(ctx, q, profile.OrgID, profile.SiteID); err != nil {
			return sqlc.CurtailmentResponseProfile{}, err
		}
		return q.InsertCurtailmentResponseProfile(ctx, insertResponseProfileParams(profile))
	})
	if err != nil {
		return nil, mapResponseProfileWriteError("create", err)
	}
	return responseProfileFromRow(row), nil
}

func (s *SQLCurtailmentStore) UpdateResponseProfile(ctx context.Context, profile models.ResponseProfile, expectedSiteID *int64) (*models.ResponseProfile, error) {
	row, err := db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (sqlc.CurtailmentResponseProfile, error) {
		if err := lockResponseProfileSitesForWrite(ctx, q, profile.OrgID, expectedSiteID, profile.SiteID); err != nil {
			return sqlc.CurtailmentResponseProfile{}, err
		}
		row, err := q.UpdateCurtailmentResponseProfile(ctx, updateResponseProfileParams(profile, expectedSiteID))
		if errors.Is(err, sql.ErrNoRows) {
			if _, getErr := q.GetCurtailmentResponseProfileByOrg(ctx, sqlc.GetCurtailmentResponseProfileByOrgParams{
				ID:    profile.ID,
				OrgID: profile.OrgID,
			}); errors.Is(getErr, sql.ErrNoRows) {
				return sqlc.CurtailmentResponseProfile{}, fleeterror.NewNotFoundErrorf("curtailment response profile not found: %d", profile.ID)
			} else if getErr != nil {
				return sqlc.CurtailmentResponseProfile{}, fleeterror.NewInternalErrorf("failed to get curtailment response profile after update conflict: %v", getErr)
			}
			return sqlc.CurtailmentResponseProfile{}, fleeterror.NewFailedPreconditionError("curtailment response profile changed before update; retry")
		}
		return row, err
	})
	if err != nil {
		return nil, mapResponseProfileWriteError("update", err)
	}
	return responseProfileFromRow(row), nil
}

func (s *SQLCurtailmentStore) DeleteResponseProfile(ctx context.Context, orgID, profileID int64, expectedSiteID *int64) error {
	count, err := s.CountAutomationRulesByResponseProfile(ctx, orgID, profileID)
	if err != nil {
		return err
	}
	if count > 0 {
		return fleeterror.NewFailedPreconditionError("curtailment response profile is referenced by an automation rule")
	}
	rows, err := s.GetQueries(ctx).DeleteCurtailmentResponseProfileByOrg(ctx, sqlc.DeleteCurtailmentResponseProfileByOrgParams{
		ID:             profileID,
		OrgID:          orgID,
		ExpectedSiteID: ptrToNullInt64(expectedSiteID),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to delete curtailment response profile: %v", err)
	}
	if rows == 0 {
		if _, getErr := s.GetQueries(ctx).GetCurtailmentResponseProfileByOrg(ctx, sqlc.GetCurtailmentResponseProfileByOrgParams{
			ID:    profileID,
			OrgID: orgID,
		}); errors.Is(getErr, sql.ErrNoRows) {
			return fleeterror.NewNotFoundErrorf("curtailment response profile not found: %d", profileID)
		} else if getErr != nil {
			return fleeterror.NewInternalErrorf("failed to get curtailment response profile after delete conflict: %v", getErr)
		}
		return fleeterror.NewFailedPreconditionError("curtailment response profile changed before delete; retry")
	}
	return nil
}

func (s *SQLCurtailmentStore) CountAutomationRulesByResponseProfile(ctx context.Context, orgID, profileID int64) (int64, error) {
	count, err := s.GetQueries(ctx).CountCurtailmentAutomationRulesByResponseProfile(ctx, sqlc.CountCurtailmentAutomationRulesByResponseProfileParams{
		OrgID:             orgID,
		ResponseProfileID: profileID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count curtailment automation rules by response profile: %v", err)
	}
	return count, nil
}

func (s *SQLCurtailmentStore) ListAutomationRules(ctx context.Context, orgID int64) ([]*models.AutomationRule, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentAutomationRulesByOrg(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment automation rules: %v", err)
	}
	out := make([]*models.AutomationRule, len(rows))
	for i, row := range rows {
		out[i] = automationRuleFromListRow(row)
	}
	return out, nil
}

func (s *SQLCurtailmentStore) GetAutomationRule(ctx context.Context, orgID, ruleID int64) (*models.AutomationRule, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentAutomationRuleByOrg(ctx, sqlc.GetCurtailmentAutomationRuleByOrgParams{
		ID:    ruleID,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment automation rule: %v", err)
	}
	return automationRuleFromGetRow(row), nil
}

func (s *SQLCurtailmentStore) ListEnabledAutomationRulesByMQTTSource(ctx context.Context, mqttSourceID int64) ([]*models.AutomationRule, error) {
	rows, err := s.GetQueries(ctx).ListEnabledCurtailmentAutomationRulesByMQTTSource(ctx, mqttSourceID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list enabled curtailment automation rules by MQTT source: %v", err)
	}
	out := make([]*models.AutomationRule, len(rows))
	for i, row := range rows {
		out[i] = automationRuleFromEnabledMQTTRow(row)
	}
	return out, nil
}

func (s *SQLCurtailmentStore) CreateAutomationRule(ctx context.Context, rule models.AutomationRule) (*models.AutomationRule, error) {
	inserted, err := s.GetQueries(ctx).InsertCurtailmentAutomationRule(ctx, sqlc.InsertCurtailmentAutomationRuleParams{
		OrgID:             rule.OrgID,
		RuleName:          rule.RuleName,
		TriggerType:       string(rule.TriggerType),
		MqttSourceID:      rule.MQTTSourceID,
		ResponseProfileID: rule.ResponseProfileID,
		Enabled:           rule.Enabled,
	})
	if err != nil {
		return nil, mapAutomationRuleWriteError("create", err)
	}
	return s.GetAutomationRule(ctx, inserted.OrgID, inserted.ID)
}

func (s *SQLCurtailmentStore) UpdateAutomationRule(ctx context.Context, rule models.AutomationRule) (*models.AutomationRule, error) {
	updated, err := s.GetQueries(ctx).UpdateCurtailmentAutomationRule(ctx, sqlc.UpdateCurtailmentAutomationRuleParams{
		ID:                rule.ID,
		OrgID:             rule.OrgID,
		RuleName:          rule.RuleName,
		MqttSourceID:      rule.MQTTSourceID,
		ResponseProfileID: rule.ResponseProfileID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, s.automationRuleLifecycleNoRowsError(ctx, "update", rule.OrgID, rule.ID)
		}
		return nil, mapAutomationRuleWriteError("update", err)
	}
	return s.GetAutomationRule(ctx, updated.OrgID, updated.ID)
}

func (s *SQLCurtailmentStore) SetAutomationRuleEnabled(ctx context.Context, orgID, ruleID int64, enabled bool) (*models.AutomationRule, error) {
	updated, err := s.GetQueries(ctx).SetCurtailmentAutomationRuleEnabled(ctx, sqlc.SetCurtailmentAutomationRuleEnabledParams{
		ID:      ruleID,
		OrgID:   orgID,
		Enabled: enabled,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if !enabled {
				return nil, s.automationRuleLifecycleNoRowsError(ctx, "disable", orgID, ruleID)
			}
			return nil, fleeterror.NewNotFoundErrorf("curtailment automation rule not found: %d", ruleID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to set curtailment automation rule enabled: %v", err)
	}
	return s.GetAutomationRule(ctx, updated.OrgID, updated.ID)
}

func (s *SQLCurtailmentStore) DeleteAutomationRule(ctx context.Context, orgID, ruleID int64) error {
	rows, err := s.GetQueries(ctx).DeleteCurtailmentAutomationRuleByOrg(ctx, sqlc.DeleteCurtailmentAutomationRuleByOrgParams{
		ID:    ruleID,
		OrgID: orgID,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to delete curtailment automation rule: %v", err)
	}
	if rows == 0 {
		return s.automationRuleLifecycleNoRowsError(ctx, "delete", orgID, ruleID)
	}
	return nil
}

func (s *SQLCurtailmentStore) automationRuleLifecycleNoRowsError(ctx context.Context, action string, orgID, ruleID int64) error {
	rule, err := s.GetAutomationRule(ctx, orgID, ruleID)
	if err != nil {
		return err
	}
	if err := s.nonTerminalAutomationEventError(ctx, action, rule); err != nil {
		return err
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"curtailment automation rule changed before %s; retry",
		action,
	)
}

func (s *SQLCurtailmentStore) nonTerminalAutomationEventError(ctx context.Context, action string, rule *models.AutomationRule) error {
	if rule == nil || rule.ActiveEventUUID == nil {
		return nil
	}
	event, err := s.GetEventByUUID(ctx, rule.OrgID, *rule.ActiveEventUUID)
	if err != nil {
		if fleeterror.IsNotFoundError(err) {
			return nil
		}
		return err
	}
	if event == nil || event.State.IsTerminal() {
		return nil
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"cannot %s curtailment automation rule while automation event %s is %s; restore or complete the event first",
		action,
		event.EventUUID,
		event.State,
	)
}

func (s *SQLCurtailmentStore) CountAutomationRulesByMQTTSource(ctx context.Context, orgID, sourceID int64) (int64, error) {
	count, err := s.GetQueries(ctx).CountCurtailmentAutomationRulesByMQTTSource(ctx, sqlc.CountCurtailmentAutomationRulesByMQTTSourceParams{
		OrgID:        orgID,
		MqttSourceID: sourceID,
	})
	if err != nil {
		return 0, fleeterror.NewInternalErrorf("failed to count curtailment automation rules by MQTT source: %v", err)
	}
	return count, nil
}

func (s *SQLCurtailmentStore) RecordAutomationSignal(ctx context.Context, ruleID int64, signal models.AutomationSignal, at time.Time) error {
	if err := s.GetQueries(ctx).UpsertCurtailmentAutomationSignalState(ctx, sqlc.UpsertCurtailmentAutomationSignalStateParams{
		RuleID:       ruleID,
		LastSignal:   sql.NullString{String: string(signal), Valid: true},
		LastSignalAt: sql.NullTime{Time: at, Valid: !at.IsZero()},
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to record curtailment automation signal: %v", err)
	}
	return nil
}

func (s *SQLCurtailmentStore) SetAutomationActiveEvent(ctx context.Context, ruleID int64, eventUUID uuid.UUID, at time.Time) error {
	if err := s.GetQueries(ctx).SetCurtailmentAutomationActiveEvent(ctx, sqlc.SetCurtailmentAutomationActiveEventParams{
		RuleID:          ruleID,
		ActiveEventUuid: uuid.NullUUID{UUID: eventUUID, Valid: eventUUID != uuid.Nil},
		LastStartedAt:   sql.NullTime{Time: at, Valid: !at.IsZero()},
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to set curtailment automation active event: %v", err)
	}
	return nil
}

func (s *SQLCurtailmentStore) ClearAutomationActiveEvent(ctx context.Context, ruleID int64, at time.Time) error {
	if err := s.GetQueries(ctx).ClearCurtailmentAutomationActiveEvent(ctx, sqlc.ClearCurtailmentAutomationActiveEventParams{
		RuleID:         ruleID,
		LastRestoredAt: sql.NullTime{Time: at, Valid: !at.IsZero()},
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to clear curtailment automation active event: %v", err)
	}
	return nil
}

func (s *SQLCurtailmentStore) RecordAutomationRestoreStarted(ctx context.Context, ruleID int64, at time.Time) error {
	if err := s.GetQueries(ctx).SetCurtailmentAutomationRestoreStarted(ctx, sqlc.SetCurtailmentAutomationRestoreStartedParams{
		RuleID:         ruleID,
		LastRestoredAt: sql.NullTime{Time: at, Valid: !at.IsZero()},
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to record curtailment automation restore start: %v", err)
	}
	return nil
}

func (s *SQLCurtailmentStore) RecordAutomationExecutionError(ctx context.Context, ruleID int64, message string, at time.Time) error {
	if err := s.GetQueries(ctx).SetCurtailmentAutomationExecutionError(ctx, sqlc.SetCurtailmentAutomationExecutionErrorParams{
		RuleID:      ruleID,
		LastError:   sql.NullString{String: message, Valid: message != ""},
		LastErrorAt: sql.NullTime{Time: at, Valid: !at.IsZero()},
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to record curtailment automation execution error: %v", err)
	}
	return nil
}

func automationRuleFromListRow(row sqlc.ListCurtailmentAutomationRulesByOrgRow) *models.AutomationRule {
	return automationRuleFromFields(
		row.ID,
		row.OrgID,
		row.RuleName,
		row.TriggerType,
		row.MqttSourceID,
		row.MqttSourceName,
		row.ResponseProfileID,
		row.ResponseProfileName,
		row.ResponseProfileSiteID,
		row.Enabled,
		row.LastSignal,
		row.LastSignalAt,
		row.ActiveEventUuid,
		row.LastStartedAt,
		row.LastRestoredAt,
		row.LastError,
		row.LastErrorAt,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func automationRuleFromGetRow(row sqlc.GetCurtailmentAutomationRuleByOrgRow) *models.AutomationRule {
	return automationRuleFromFields(
		row.ID,
		row.OrgID,
		row.RuleName,
		row.TriggerType,
		row.MqttSourceID,
		row.MqttSourceName,
		row.ResponseProfileID,
		row.ResponseProfileName,
		row.ResponseProfileSiteID,
		row.Enabled,
		row.LastSignal,
		row.LastSignalAt,
		row.ActiveEventUuid,
		row.LastStartedAt,
		row.LastRestoredAt,
		row.LastError,
		row.LastErrorAt,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func automationRuleFromEnabledMQTTRow(row sqlc.ListEnabledCurtailmentAutomationRulesByMQTTSourceRow) *models.AutomationRule {
	return automationRuleFromFields(
		row.ID,
		row.OrgID,
		row.RuleName,
		row.TriggerType,
		row.MqttSourceID,
		row.MqttSourceName,
		row.ResponseProfileID,
		row.ResponseProfileName,
		row.ResponseProfileSiteID,
		row.Enabled,
		row.LastSignal,
		row.LastSignalAt,
		row.ActiveEventUuid,
		row.LastStartedAt,
		row.LastRestoredAt,
		row.LastError,
		row.LastErrorAt,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func automationRuleFromFields(
	id int64,
	orgID int64,
	ruleName string,
	triggerType string,
	mqttSourceID int64,
	mqttSourceName string,
	responseProfileID int64,
	responseProfileName string,
	responseProfileSiteID sql.NullInt64,
	enabled bool,
	lastSignal sql.NullString,
	lastSignalAt sql.NullTime,
	activeEventUUID uuid.NullUUID,
	lastStartedAt sql.NullTime,
	lastRestoredAt sql.NullTime,
	lastError sql.NullString,
	lastErrorAt sql.NullTime,
	createdAt time.Time,
	updatedAt time.Time,
) *models.AutomationRule {
	return &models.AutomationRule{
		ID:                    id,
		OrgID:                 orgID,
		RuleName:              ruleName,
		TriggerType:           models.AutomationTriggerType(triggerType),
		MQTTSourceID:          mqttSourceID,
		MQTTSourceName:        mqttSourceName,
		ResponseProfileID:     responseProfileID,
		ResponseProfileName:   responseProfileName,
		ResponseProfileSiteID: nullInt64ToPtr(responseProfileSiteID),
		Enabled:               enabled,
		LastSignal:            nullAutomationSignalToPtr(lastSignal),
		LastSignalAt:          nullTimeToPtr(lastSignalAt),
		ActiveEventUUID:       nullUUIDToPtr(activeEventUUID),
		LastStartedAt:         nullTimeToPtr(lastStartedAt),
		LastRestoredAt:        nullTimeToPtr(lastRestoredAt),
		LastError:             nullStringToPtr(lastError),
		LastErrorAt:           nullTimeToPtr(lastErrorAt),
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
	}
}

func nullAutomationSignalToPtr(n sql.NullString) *models.AutomationSignal {
	if !n.Valid {
		return nil
	}
	v := models.AutomationSignal(n.String)
	return &v
}

func nullUUIDToPtr(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid {
		return nil
	}
	v := n.UUID
	return &v
}

func mapAutomationRuleWriteError(action string, err error) error {
	var fleetErr fleeterror.FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgErrCodeUniqueViolation:
			if pgErr.ConstraintName == automationRuleOrgNameUnique {
				return fleeterror.NewAlreadyExistsError("a curtailment automation rule with this name already exists")
			}
		case pgErrCodeForeignKeyViolation:
			return fleeterror.NewNotFoundError("organization, MQTT source, or response profile not found for curtailment automation rule")
		case "23514": // check_violation
			return fleeterror.NewInvalidArgumentError("curtailment automation rule violates persisted constraints")
		}
	}
	return fleeterror.NewInternalErrorf("failed to %s curtailment automation rule: %v", action, err)
}

func lockResponseProfileSitesForWrite(ctx context.Context, q *sqlc.Queries, orgID int64, siteIDs ...*int64) error {
	for _, siteID := range responseProfileSiteIDsForLock(siteIDs...) {
		if _, err := q.LockSiteForWrite(ctx, sqlc.LockSiteForWriteParams{ID: siteID, OrgID: orgID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fleeterror.NewNotFoundErrorf("site not found: %d", siteID)
			}
			return fleeterror.NewInternalErrorf("failed to lock site for curtailment response profile write: %v", err)
		}
	}
	return nil
}

func responseProfileSiteIDsForLock(siteIDs ...*int64) []int64 {
	seen := make(map[int64]struct{}, len(siteIDs))
	out := make([]int64, 0, len(siteIDs))
	for _, siteID := range siteIDs {
		if siteID == nil {
			continue
		}
		if _, ok := seen[*siteID]; ok {
			continue
		}
		seen[*siteID] = struct{}{}
		out = append(out, *siteID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// InsertEventWithTargets writes event + targets in one transaction.
func (s *SQLCurtailmentStore) InsertEventWithTargets(
	ctx context.Context,
	event models.InsertEventParams,
	targets []models.InsertTargetParams,
) (*models.InsertEventResult, error) {
	// A terminal event (a vacuously-COMPLETED FULL_FLEET start with nothing
	// eligible) legitimately has no targets; a non-terminal event with none is
	// a caller bug.
	if len(targets) == 0 && !event.State.IsTerminal() {
		return nil, fleeterror.NewInvalidArgumentError(
			"InsertEventWithTargets requires a non-empty targets slice for a non-terminal event",
		)
	}
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.InsertEventResult, error) {
		row, err := q.InsertCurtailmentEvent(ctx, sqlc.InsertCurtailmentEventParams{
			EventUuid:               event.EventUUID,
			OrgID:                   event.OrgID,
			State:                   string(event.State),
			Mode:                    string(event.Mode),
			Strategy:                string(event.Strategy),
			Level:                   string(event.Level),
			Priority:                string(event.Priority),
			LoopType:                string(event.LoopType),
			ScopeType:               string(event.ScopeType),
			ScopeJsonb:              event.ScopeJSON,
			ModeParamsJsonb:         event.ModeParamsJSON,
			CurtailBatchSize:        ptrToNullInt32(event.CurtailBatchSize),
			CurtailBatchIntervalSec: event.CurtailBatchIntervalSec,
			RestoreBatchSize:        event.RestoreBatchSize,
			RestoreBatchIntervalSec: event.RestoreBatchIntervalSec,
			MinCurtailedDurationSec: event.MinCurtailedDurationSec,
			MaxDurationSeconds:      ptrToNullInt32(event.MaxDurationSeconds),
			AllowUnbounded:          event.AllowUnbounded,
			IncludeMaintenance:      event.IncludeMaintenance,
			ForceIncludeMaintenance: event.ForceIncludeMaintenance,
			DecisionSnapshotJsonb:   event.DecisionSnapshotJSON,
			SourceActorType:         string(event.SourceActorType),
			SourceActorID:           ptrToNullString(event.SourceActorID),
			ExternalSource:          ptrToNullString(event.ExternalSource),
			ExternalReference:       ptrToNullString(event.ExternalReference),
			IdempotencyKey:          ptrToNullString(event.IdempotencyKey),
			Reason:                  event.Reason,
			ScheduledStartAt:        ptrToNullTime(event.ScheduledStartAt),
			EndedAt:                 ptrToNullTime(event.EndedAt),
			CreatedByUserID:         event.CreatedByUserID,
			EffectiveBatchSize:      sql.NullInt32{Int32: event.EffectiveBatchSize, Valid: true},
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUniqueViolation {
				switch pgErr.ConstraintName {
				case idempotencyKeyUniqueIndex, externalReferenceUniqueIndex:
					// Replay path: caller re-issues the matching lookup.
					return nil, interfaces.ErrCurtailmentReplayRaceLoss
				}
				// Unknown constraint: sanitize the response and log the
				// name server-side so it doesn't leak through %v.
				slog.Error("curtailment_event insert hit unknown unique constraint",
					"constraint", pgErr.ConstraintName, "org_id", event.OrgID, "event_uuid", event.EventUUID)
				return nil, fleeterror.NewAlreadyExistsError("curtailment event already exists")
			}
			return nil, fleeterror.NewInternalErrorf("failed to insert curtailment event: %v", err)
		}
		payload, err := buildBulkTargetPayload(targets)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf(
				"failed to encode curtailment target payload: %v", err,
			)
		}
		inserted, err := q.BulkInsertCurtailmentTargets(ctx, sqlc.BulkInsertCurtailmentTargetsParams{
			CurtailmentEventID: row.ID,
			TargetsJsonb:       payload,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUniqueViolation &&
				pgErr.ConstraintName == deviceNonTerminalUniqueIndex {
				// The device-exclusivity index rejected a target: another
				// non-terminal event already curtails one of these devices
				// (selector/insert race). Return a FleetError directly —
				// WithTransaction converts plain sentinels to Internal.
				return nil, fleeterror.NewAlreadyExistsError(
					"one or more selected devices are already in a non-terminal curtailment; retry",
				)
			}
			return nil, fleeterror.NewInternalErrorf("failed to bulk insert curtailment targets: %v", err)
		}
		if inserted != int64(len(targets)) {
			// jsonb_to_recordset silently drops rows that fail column-type
			// cast; bail so the tx rolls back instead of partial fanout.
			return nil, fleeterror.NewInternalErrorf(
				"bulk insert wrote %d targets, expected %d", inserted, len(targets),
			)
		}
		return &models.InsertEventResult{
			ID:        row.ID,
			EventUUID: row.EventUuid,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}, nil
	})
}

func (s *SQLCurtailmentStore) GetEventByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
		EventUuid: eventUUID,
		OrgID:     orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
	}
	return convertEventRow(row), nil
}

func (s *SQLCurtailmentStore) GetEventDetailByUUID(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventDetailByUUID(ctx, sqlc.GetCurtailmentEventDetailByUUIDParams{
		EventUuid: eventUUID,
		OrgID:     orgID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment event detail: %v", err)
	}
	return convertEventDetailRow(row), nil
}

func (s *SQLCurtailmentStore) ListActiveEvents(ctx context.Context, orgID int64) ([]*models.Event, error) {
	rows, err := s.GetQueries(ctx).ListActiveCurtailmentEvents(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list active curtailment events for org %d: %v", orgID, err)
	}
	events := make([]*models.Event, len(rows))
	for i, row := range rows {
		events[i] = convertActiveEventRow(row)
	}
	return events, nil
}

func (s *SQLCurtailmentStore) GetEventByIdempotencyKey(ctx context.Context, orgID int64, idempotencyKey string) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventByIdempotencyKey(ctx, sqlc.GetCurtailmentEventByIdempotencyKeyParams{
		OrgID:          orgID,
		IdempotencyKey: sql.NullString{String: idempotencyKey, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to look up curtailment event by idempotency_key: %v", err)
	}
	return convertEventRow(row), nil
}

func (s *SQLCurtailmentStore) GetEventByExternalReference(ctx context.Context, orgID int64, externalSource, externalReference string) (*models.Event, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentEventByExternalReference(ctx, sqlc.GetCurtailmentEventByExternalReferenceParams{
		OrgID:             orgID,
		ExternalSource:    sql.NullString{String: externalSource, Valid: true},
		ExternalReference: sql.NullString{String: externalReference, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fleeterror.NewInternalErrorf("failed to look up curtailment event by (external_source, external_reference): %v", err)
	}
	return convertEventRow(row), nil
}

const (
	curtailmentEventsDefaultPageSize  int32 = 50
	curtailmentEventsMaxPageSize      int32 = 200
	curtailmentTargetsDefaultPageSize int32 = 500
	curtailmentTargetsMaxPageSize     int32 = 1000
)

func (s *SQLCurtailmentStore) ListEvents(ctx context.Context, params interfaces.ListEventsParams) ([]*models.Event, string, error) {
	stateFilters := normalizeCurtailmentEventStateFilters(params.StateFilters)
	cursor, err := decodeCurtailmentEventCursor(params.PageToken)
	if err != nil {
		return nil, "", err
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = curtailmentEventsDefaultPageSize
	}
	if pageSize > curtailmentEventsMaxPageSize {
		pageSize = curtailmentEventsMaxPageSize
	}

	var cursorID int64
	if cursor != nil {
		if cursor.OrgID != params.OrgID || !curtailmentEventStateFiltersEqual(cursor.StateFilters, stateFilters) {
			return nil, "", fleeterror.NewInvalidArgumentError("page_token does not match org_id or state_filters")
		}
		cursorID = cursor.ID
	}

	rows, err := s.GetQueries(ctx).ListCurtailmentEventsForOrg(ctx, sqlc.ListCurtailmentEventsForOrgParams{
		OrgID:        params.OrgID,
		CursorID:     cursorID,
		StateFilters: eventStateFilterStrings(stateFilters),
		// Over-fetch by one so the caller knows whether another page remains.
		RowLimit: int64(pageSize) + 1,
	})
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("failed to list curtailment events: %v", err)
	}

	var nextToken string
	if int64(len(rows)) > int64(pageSize) {
		// Trim the over-fetched row; cursor points at the last id.
		rows = rows[:pageSize]
		nextToken = encodeCurtailmentEventCursor(&curtailmentEventCursor{
			ID:           rows[len(rows)-1].ID,
			OrgID:        params.OrgID,
			StateFilters: stateFilters,
		})
	}

	out := make([]*models.Event, len(rows))
	for i, row := range rows {
		out[i] = convertEventListRow(row)
	}
	return out, nextToken, nil
}

func eventStateFilterStrings(filters []models.EventState) []string {
	if len(filters) == 0 {
		return []string{}
	}

	out := make([]string, len(filters))
	for i, filter := range filters {
		out[i] = string(filter)
	}
	return out
}

func (s *SQLCurtailmentStore) UpdateOperatorFields(ctx context.Context, eventID, orgID int64, params interfaces.UpdateOperatorFieldsParams) (*models.Event, error) {
	row, err := s.GetQueries(ctx).UpdateCurtailmentEventOperatorFields(ctx, sqlc.UpdateCurtailmentEventOperatorFieldsParams{
		ID:                      eventID,
		OrgID:                   orgID,
		Reason:                  nullStringFromPtr(params.Reason),
		RestoreBatchSize:        nullInt32FromPtr(params.RestoreBatchSize),
		RestoreBatchIntervalSec: nullInt32FromPtr(params.RestoreBatchIntervalSec),
		MaxDurationSeconds:      nullInt32FromPtr(params.MaxDurationSeconds),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, interfaces.ErrCurtailmentEventStateRaceLoss
		}
		return nil, fleeterror.NewInternalErrorf("failed to update curtailment event: %v", err)
	}
	return convertEventRow(row), nil
}

func nullInt32FromPtr(p *int32) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *p, Valid: true}
}

// AdminTerminateEvent transactionally flips the event to targetState and
// sweeps non-terminal targets to RESTORE_FAILED with reason as last_error.
// Routes: same target_state → idempotent echo; different terminal state →
// StateConflict; any in-flight target → ActiveEvent (caller must Stop first).
//
// transitioned=false marks the idempotent-echo paths (initial-read or
// race-loss re-read) so the caller can suppress side effects.
type adminTerminateResult struct {
	event        *models.Event
	transitioned bool
}

func (s *SQLCurtailmentStore) AdminTerminateEvent(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
	targetState models.EventState,
	reason string,
) (*models.Event, bool, error) {
	result, err := db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (adminTerminateResult, error) {
		current, err := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
			EventUuid: eventUUID,
			OrgID:     orgID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return adminTerminateResult{}, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		if err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
		}

		currentState := models.EventState(current.State)
		if currentState == targetState {
			// Idempotent echo: event already in the requested terminal state.
			return adminTerminateResult{event: convertEventRow(current), transitioned: false}, nil
		}
		if currentState.IsTerminal() {
			return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateStateConflict
		}

		// In-flight gate: reject if any target still has an outstanding
		// Curtail. Subsumes the ACTIVE check and catches mid-dispatch
		// PENDING events.
		hasInFlight, err := q.CurtailmentEventHasInFlightTargets(ctx, current.ID)
		if err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to check in-flight targets: %v", err)
		}
		if hasInFlight {
			return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
		}

		updated, err := q.AdminTerminateCurtailmentEvent(ctx, sqlc.AdminTerminateCurtailmentEventParams{
			ID:          current.ID,
			OrgID:       orgID,
			TargetState: string(targetState),
		})
		if errors.Is(err, sql.ErrNoRows) {
			// Race: UPDATE matched 0 rows under the state guard. Re-read
			// and route by latest state for idempotent echo.
			latest, getErr := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
				EventUuid: eventUUID,
				OrgID:     orgID,
			})
			if getErr != nil {
				return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to re-read curtailment event after concurrent state change: %v", getErr)
			}
			latestState := models.EventState(latest.State)
			if latestState == targetState {
				// Idempotent echo: concurrent terminate landed first.
				return adminTerminateResult{event: convertEventRow(latest), transitioned: false}, nil
			}
			hasInFlight, gateErr := q.CurtailmentEventHasInFlightTargets(ctx, current.ID)
			if gateErr != nil {
				return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to check in-flight targets after terminate race: %v", gateErr)
			}
			if hasInFlight {
				return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
			}
			if latestState == models.EventStateActive {
				return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateActiveEvent
			}
			return adminTerminateResult{}, interfaces.ErrCurtailmentAdminTerminateStateConflict
		}
		if err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to terminate curtailment event: %v", err)
		}

		if err := q.SweepCurtailmentTargetsToRestoreFailed(ctx, sqlc.SweepCurtailmentTargetsToRestoreFailedParams{
			CurtailmentEventID: current.ID,
			LastError:          reason,
		}); err != nil {
			return adminTerminateResult{}, fleeterror.NewInternalErrorf("failed to sweep curtailment targets: %v", err)
		}

		return adminTerminateResult{event: convertEventRow(updated), transitioned: true}, nil
	})
	if err != nil {
		return nil, false, err
	}
	return result.event, result.transitioned, nil
}

func (s *SQLCurtailmentStore) ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentTargetsByEvent(ctx, sqlc.ListCurtailmentTargetsByEventParams{
		OrgID:     orgID,
		EventUuid: eventUUID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment targets: %v", err)
	}
	targets := make([]*models.Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, convertTargetRow(row))
	}
	return targets, nil
}

func (s *SQLCurtailmentStore) ListTargetsByEventPage(ctx context.Context, params interfaces.ListTargetsByEventPageParams) ([]*models.Target, string, error) {
	cursor, err := decodeCurtailmentTargetCursor(params.PageToken)
	if err != nil {
		return nil, "", err
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = curtailmentTargetsDefaultPageSize
	}
	if pageSize > curtailmentTargetsMaxPageSize {
		pageSize = curtailmentTargetsMaxPageSize
	}

	var cursorDeviceIdentifier string
	if cursor != nil {
		if cursor.OrgID != params.OrgID || cursor.EventUUID != params.EventUUID {
			return nil, "", fleeterror.NewInvalidArgumentError("target_page_token does not match org_id or event_uuid")
		}
		cursorDeviceIdentifier = cursor.DeviceIdentifier
	}

	rows, err := s.GetQueries(ctx).ListCurtailmentTargetsByEventPage(ctx, sqlc.ListCurtailmentTargetsByEventPageParams{
		OrgID:                  params.OrgID,
		EventUuid:              params.EventUUID,
		CursorDeviceIdentifier: cursorDeviceIdentifier,
		RowLimit:               int64(pageSize) + 1,
	})
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("failed to list curtailment target page: %v", err)
	}

	var nextToken string
	if int64(len(rows)) > int64(pageSize) {
		rows = rows[:pageSize]
		nextToken = encodeCurtailmentTargetCursor(&curtailmentTargetCursor{
			OrgID:            params.OrgID,
			EventUUID:        params.EventUUID,
			DeviceIdentifier: rows[len(rows)-1].DeviceIdentifier,
		})
	}

	targets := make([]*models.Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, convertTargetRow(row))
	}
	return targets, nextToken, nil
}

func (s *SQLCurtailmentStore) ListTargetSiteIDsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]int64, bool, error) {
	rows, err := s.GetQueries(ctx).ListCurtailmentTargetSiteCoverageByEvent(ctx, sqlc.ListCurtailmentTargetSiteCoverageByEventParams{
		OrgID:     orgID,
		EventUuid: eventUUID,
	})
	if err != nil {
		return nil, false, fleeterror.NewInternalErrorf("failed to list curtailment target site coverage: %v", err)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	siteIDs := make([]int64, 0, len(rows))
	complete := rows[0].TargetCount > 0 && rows[0].TargetCount == rows[0].MappedTargetCount
	for _, row := range rows {
		if row.SiteID <= 0 {
			complete = false
			continue
		}
		siteIDs = append(siteIDs, row.SiteID)
		if row.TargetCount != rows[0].TargetCount || row.MappedTargetCount != rows[0].MappedTargetCount {
			complete = false
		}
	}
	return siteIDs, complete, nil
}

func (s *SQLCurtailmentStore) GetTargetRollupByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) (*models.TargetRollup, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentTargetRollupByEvent(ctx, sqlc.GetCurtailmentTargetRollupByEventParams{
		OrgID:     orgID,
		EventUuid: eventUUID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment target rollup: %v", err)
	}
	return &models.TargetRollup{
		Pending:       row.Pending,
		Dispatched:    row.Dispatched,
		Confirmed:     row.Confirmed,
		Drifted:       row.Drifted,
		Resolved:      row.Resolved,
		Released:      row.Released,
		RestoreFailed: row.RestoreFailed,
		Total:         row.Total,
	}, nil
}

func (s *SQLCurtailmentStore) ListCandidates(ctx context.Context, params interfaces.ListCandidatesParams) ([]*models.Candidate, error) {
	params = normalizeListCandidatesParams(params)
	rows, err := s.GetQueries(ctx).ListCurtailmentCandidatesByOrg(ctx, sqlc.ListCurtailmentCandidatesByOrgParams{
		OrgID:             params.OrgID,
		SiteID:            ptrToNullInt64(params.SiteID),
		DeviceIdentifiers: params.DeviceIdentifiers,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list curtailment candidates: %v", err)
	}
	out := make([]*models.Candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, &models.Candidate{
			DeviceIdentifier: row.DeviceIdentifier,
			DriverName:       nullStringToPtr(row.DriverName),
			Model:            row.Model,
			DeviceStatus:     row.DeviceStatus,
			PairingStatus:    row.PairingStatus,
			LatestMetricsAt:  nullTimeToPtr(row.LatestMetricsAt),
			LatestPowerW:     nullFloat64ToPtr(row.LatestPowerW),
			LatestHashRateHS: nullFloat64ToPtr(row.LatestHashRateHs),
			AvgEfficiencyJH:  nullFloat64ToPtr(row.AvgEfficiency),
		})
	}
	return out, nil
}

func normalizeListCandidatesParams(params interfaces.ListCandidatesParams) interfaces.ListCandidatesParams {
	if len(params.DeviceIdentifiers) == 0 {
		params.DeviceIdentifiers = nil
	}
	return params
}

func (s *SQLCurtailmentStore) ListNonTerminalEvents(ctx context.Context) ([]*models.Event, error) {
	rows, err := s.GetQueries(ctx).ListNonTerminalCurtailmentEvents(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list non-terminal curtailment events: %v", err)
	}
	out := make([]*models.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, convertEventRow(row))
	}
	return out, nil
}

func (s *SQLCurtailmentStore) UpdateEventState(ctx context.Context, eventID int64, expectedState models.EventState, state models.EventState, startedAt *time.Time, endedAt *time.Time) error {
	rows, err := s.GetQueries(ctx).UpdateCurtailmentEventState(ctx, sqlc.UpdateCurtailmentEventStateParams{
		ID:            eventID,
		ExpectedState: string(expectedState),
		State:         string(state),
		StartedAt:     ptrToNullTime(startedAt),
		EndedAt:       ptrToNullTime(endedAt),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update curtailment event %d state: %v", eventID, err)
	}
	if rows == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) UpdateTargetState(ctx context.Context, eventID int64, deviceIdentifier string, params interfaces.UpdateCurtailmentTargetStateParams) error {
	rows, err := s.GetQueries(ctx).UpdateCurtailmentTargetState(ctx, sqlc.UpdateCurtailmentTargetStateParams{
		CurtailmentEventID:   eventID,
		DeviceIdentifier:     deviceIdentifier,
		State:                string(params.State),
		LastDispatchedAt:     ptrToNullTime(params.LastDispatchedAt),
		LastBatchUuid:        ptrToNullString(params.LastBatchUUID),
		ObservedPowerW:       ptrFloat64ToNullString(params.ObservedPowerW),
		ObservedAt:           ptrToNullTime(params.ObservedAt),
		ConfirmedAt:          ptrToNullTime(params.ConfirmedAt),
		RetryCount:           ptrToNullInt32(params.RetryCount),
		LastError:            ptrToNullString(params.LastError),
		ExpectedEventState:   ptrEventStateToNullString(params.ExpectedEventState),
		ExpectedDesiredState: ptrToNullString(params.ExpectedDesiredState),
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to update curtailment target (%d, %s) state: %v", eventID, deviceIdentifier, err)
	}
	if rows == 0 {
		// Zero rows: either the parent event advanced to terminal (EXISTS
		// guard) or expected_desired_state lost the race against a Stop.
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) BumpTargetRetry(ctx context.Context, eventID int64, deviceIdentifier string) error {
	rows, err := s.GetQueries(ctx).BumpCurtailmentTargetRetry(ctx, sqlc.BumpCurtailmentTargetRetryParams{
		CurtailmentEventID: eventID,
		DeviceIdentifier:   deviceIdentifier,
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to bump curtailment target retry (%d, %s): %v", eventID, deviceIdentifier, err)
	}
	if rows == 0 {
		return interfaces.ErrCurtailmentEventStateRaceLoss
	}
	return nil
}

func (s *SQLCurtailmentStore) UpsertHeartbeat(ctx context.Context, params interfaces.UpsertCurtailmentHeartbeatParams) error {
	if err := s.GetQueries(ctx).UpsertCurtailmentReconcilerHeartbeat(ctx, sqlc.UpsertCurtailmentReconcilerHeartbeatParams{
		LastTickAt:         params.LastTickAt,
		LastTickUuid:       params.LastTickUUID,
		LastTickDurationMs: ptrToNullInt32(params.LastTickDurationMS),
		ActiveEventCount:   params.ActiveEventCount,
	}); err != nil {
		return fleeterror.NewInternalErrorf("failed to upsert curtailment heartbeat: %v", err)
	}
	return nil
}

// BeginRestoreTransition runs the event-state flip + target reset in one tx.
// Pre-reads the event to distinguish "already restoring" (idempotent
// return) from "already terminal" (FailedPrecondition); the UPDATE's
// state guard catches concurrent transitions between pre-read and write.
func (s *SQLCurtailmentStore) BeginRestoreTransition(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
	params interfaces.BeginRestoreTransitionParams,
) (*models.Event, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Event, error) {
		current, err := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
			EventUuid: eventUUID,
			OrgID:     orgID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
		}

		state := models.EventState(current.State)
		if state == models.EventStateRestoring {
			// Idempotent re-Stop: leave targets alone.
			return convertEventRow(current), nil
		}
		if state.IsTerminal() {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"cannot stop curtailment event %s in terminal state %q",
				eventUUID, current.State,
			)
		}
		if err := guardAutomationDemandForRestore(ctx, q, orgID, eventUUID, params.AutomationDemandGuard); err != nil {
			return nil, err
		}

		updated, err := q.BeginCurtailmentRestoration(ctx, current.ID)
		if errors.Is(err, sql.ErrNoRows) {
			// Concurrent transition between pre-read and update: re-read and
			// route by the latest state so terminal races don't silently echo
			// success.
			latest, getErr := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
				EventUuid: eventUUID,
				OrgID:     orgID,
			})
			if getErr != nil {
				return nil, fleeterror.NewInternalErrorf("failed to re-read curtailment event after concurrent state change: %v", getErr)
			}
			latestState := models.EventState(latest.State)
			if latestState.IsTerminal() {
				return nil, fleeterror.NewFailedPreconditionErrorf(
					"cannot stop curtailment event %s in terminal state %q",
					eventUUID, latest.State,
				)
			}
			if latestState == models.EventStateRestoring {
				// Idempotent re-Stop: first call's sizing wins.
				return convertEventRow(latest), nil
			}
			return nil, fleeterror.NewInternalErrorf(
				"unexpected event state after concurrent transition: %q", latest.State,
			)
		}
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to begin curtailment restoration: %v", err)
		}

		if err := q.ResetCurtailmentTargetsForRestore(ctx, current.ID); err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to reset curtailment targets for restore: %v", err)
		}

		return convertEventRow(updated), nil
	})
}

func guardAutomationDemandForRestore(
	ctx context.Context,
	q *sqlc.Queries,
	orgID int64,
	eventUUID uuid.UUID,
	guard *interfaces.AutomationDemandGuard,
) error {
	if guard == nil {
		return nil
	}
	row, err := q.GetEnabledCurtailmentAutomationRuleByEvent(ctx, sqlc.GetEnabledCurtailmentAutomationRuleByEventParams{
		OrgID:             orgID,
		EventUuid:         uuid.NullUUID{UUID: eventUUID, Valid: eventUUID != uuid.Nil},
		ExternalReference: nullStringFromPtr(guard.ExternalReference),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fleeterror.NewInternalErrorf("failed to check curtailment automation demand before restore: %v", err)
	}
	if !row.LastSignal.Valid || models.AutomationSignal(row.LastSignal.String) != models.AutomationSignalOff {
		return nil
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"cannot restore automation-owned curtailment event %s while automation rule %q still has OFF asserted; use force=true to override",
		eventUUID,
		row.RuleName,
	)
}

// BeginRecurtailTransition flips a restoring event back to pending and resets
// restore targets for Curtail dispatch. Any target overlap rolls back so the
// watchdog can retry while the event remains restoring.
func (s *SQLCurtailmentStore) BeginRecurtailTransition(
	ctx context.Context,
	orgID int64,
	eventUUID uuid.UUID,
) (*models.Event, error) {
	return db.WithTransaction(ctx, s.conn.DB, func(q *sqlc.Queries) (*models.Event, error) {
		current, err := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
			EventUuid: eventUUID,
			OrgID:     orgID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundErrorf("curtailment event not found: %s", eventUUID)
		}
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to get curtailment event: %v", err)
		}

		state := models.EventState(current.State)
		if state.IsTerminal() {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"cannot re-curtail event %s in terminal state %q",
				eventUUID, current.State,
			)
		}
		if state != models.EventStateRestoring {
			return convertEventRow(current), nil
		}

		updated, err := q.ResumeCurtailmentFromRestoring(ctx, current.ID)
		if errors.Is(err, sql.ErrNoRows) {
			latest, getErr := q.GetCurtailmentEventByUUID(ctx, sqlc.GetCurtailmentEventByUUIDParams{
				EventUuid: eventUUID,
				OrgID:     orgID,
			})
			if getErr != nil {
				return nil, fleeterror.NewInternalErrorf("failed to re-read curtailment event after concurrent state change: %v", getErr)
			}
			if models.EventState(latest.State).IsTerminal() {
				return nil, fleeterror.NewFailedPreconditionErrorf(
					"cannot re-curtail event %s in terminal state %q",
					eventUUID, latest.State,
				)
			}
			return convertEventRow(latest), nil
		}
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to resume curtailment from restoring: %v", err)
		}

		reset, err := q.ResetCurtailmentTargetsForRecurtail(ctx, current.ID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUniqueViolation &&
				pgErr.ConstraintName == deviceNonTerminalUniqueIndex {
				return nil, fleeterror.NewAlreadyExistsError(
					"one or more curtailment targets are already in a non-terminal curtailment; retry",
				)
			}
			return nil, fleeterror.NewInternalErrorf("failed to reset curtailment targets for re-curtail: %v", err)
		}
		if reset.ResetCount != reset.TargetCount {
			return nil, fleeterror.NewAlreadyExistsErrorf(
				"re-curtail reset %d of %d targets; one or more targets are already in a non-terminal curtailment; retry",
				reset.ResetCount,
				reset.TargetCount,
			)
		}

		return convertEventRow(updated), nil
	})
}

func (s *SQLCurtailmentStore) GetHeartbeat(ctx context.Context) (*models.Heartbeat, error) {
	row, err := s.GetQueries(ctx).GetCurtailmentReconcilerHeartbeat(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("curtailment reconciler heartbeat row missing (migration seed should have created it)")
		}
		return nil, fleeterror.NewInternalErrorf("failed to get curtailment heartbeat: %v", err)
	}
	return &models.Heartbeat{
		ID:                 row.ID,
		LastTickAt:         row.LastTickAt,
		LastTickUUID:       row.LastTickUuid,
		LastTickDurationMS: nullInt32ToPtr(row.LastTickDurationMs),
		ActiveEventCount:   row.ActiveEventCount,
	}, nil
}

// convertEventRow maps a sqlc row to the domain Event so callers outside
// the store don't import sqlc-generated code.
func convertEventRow(row sqlc.CurtailmentEvent) *models.Event {
	return convertEventFields(
		row.ID,
		row.EventUuid,
		row.OrgID,
		row.State,
		row.Mode,
		row.Strategy,
		row.Level,
		row.Priority,
		row.LoopType,
		row.ScopeType,
		row.ScopeJsonb,
		row.ModeParamsJsonb,
		row.CurtailBatchSize,
		row.CurtailBatchIntervalSec,
		row.RestoreBatchSize,
		row.RestoreBatchIntervalSec,
		row.EffectiveBatchSize,
		row.MinCurtailedDurationSec,
		row.MaxDurationSeconds,
		row.AllowUnbounded,
		row.IncludeMaintenance,
		row.ForceIncludeMaintenance,
		row.DecisionSnapshotJsonb,
		row.SourceActorType,
		row.SourceActorID,
		row.ExternalSource,
		row.ExternalReference,
		row.IdempotencyKey,
		row.SupersedesEventID,
		row.Reason,
		row.ScheduledStartAt,
		row.StartedAt,
		row.EndedAt,
		row.CreatedByUserID,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func convertEventDetailRow(row sqlc.GetCurtailmentEventDetailByUUIDRow) *models.Event {
	return convertEventFields(
		row.ID,
		row.EventUuid,
		row.OrgID,
		row.State,
		row.Mode,
		row.Strategy,
		row.Level,
		row.Priority,
		row.LoopType,
		row.ScopeType,
		row.ScopeJsonb,
		row.ModeParamsJsonb,
		row.CurtailBatchSize,
		row.CurtailBatchIntervalSec,
		row.RestoreBatchSize,
		row.RestoreBatchIntervalSec,
		row.EffectiveBatchSize,
		row.MinCurtailedDurationSec,
		row.MaxDurationSeconds,
		row.AllowUnbounded,
		row.IncludeMaintenance,
		row.ForceIncludeMaintenance,
		row.DecisionSnapshotJsonb,
		row.SourceActorType,
		row.SourceActorID,
		row.ExternalSource,
		row.ExternalReference,
		row.IdempotencyKey,
		row.SupersedesEventID,
		row.Reason,
		row.ScheduledStartAt,
		row.StartedAt,
		row.EndedAt,
		row.CreatedByUserID,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func convertEventListRow(row sqlc.ListCurtailmentEventsForOrgRow) *models.Event {
	return convertEventFields(
		row.ID,
		row.EventUuid,
		row.OrgID,
		row.State,
		row.Mode,
		row.Strategy,
		row.Level,
		row.Priority,
		row.LoopType,
		row.ScopeType,
		row.ScopeJsonb,
		row.ModeParamsJsonb,
		row.CurtailBatchSize,
		row.CurtailBatchIntervalSec,
		row.RestoreBatchSize,
		row.RestoreBatchIntervalSec,
		row.EffectiveBatchSize,
		row.MinCurtailedDurationSec,
		row.MaxDurationSeconds,
		row.AllowUnbounded,
		row.IncludeMaintenance,
		row.ForceIncludeMaintenance,
		row.DecisionSnapshotJsonb,
		row.SourceActorType,
		row.SourceActorID,
		row.ExternalSource,
		row.ExternalReference,
		row.IdempotencyKey,
		row.SupersedesEventID,
		row.Reason,
		row.ScheduledStartAt,
		row.StartedAt,
		row.EndedAt,
		row.CreatedByUserID,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func convertActiveEventRow(row sqlc.ListActiveCurtailmentEventsRow) *models.Event {
	return convertEventFields(
		row.ID,
		row.EventUuid,
		row.OrgID,
		row.State,
		row.Mode,
		row.Strategy,
		row.Level,
		row.Priority,
		row.LoopType,
		row.ScopeType,
		row.ScopeJsonb,
		row.ModeParamsJsonb,
		row.CurtailBatchSize,
		row.CurtailBatchIntervalSec,
		row.RestoreBatchSize,
		row.RestoreBatchIntervalSec,
		row.EffectiveBatchSize,
		row.MinCurtailedDurationSec,
		row.MaxDurationSeconds,
		row.AllowUnbounded,
		row.IncludeMaintenance,
		row.ForceIncludeMaintenance,
		row.DecisionSnapshotJsonb,
		row.SourceActorType,
		row.SourceActorID,
		row.ExternalSource,
		row.ExternalReference,
		row.IdempotencyKey,
		row.SupersedesEventID,
		row.Reason,
		row.ScheduledStartAt,
		row.StartedAt,
		row.EndedAt,
		row.CreatedByUserID,
		row.CreatedAt,
		row.UpdatedAt,
	)
}

func convertEventFields(
	id int64,
	eventUUID uuid.UUID,
	orgID int64,
	state string,
	mode string,
	strategy string,
	level string,
	priority string,
	loopType string,
	scopeType string,
	scopeJSON []byte,
	modeParamsJSON []byte,
	curtailBatchSize sql.NullInt32,
	curtailBatchIntervalSec int32,
	restoreBatchSize int32,
	restoreBatchIntervalSec int32,
	effectiveBatchSize sql.NullInt32,
	minCurtailedDurationSec int32,
	maxDurationSeconds sql.NullInt32,
	allowUnbounded bool,
	includeMaintenance bool,
	forceIncludeMaintenance bool,
	decisionSnapshotJSON []byte,
	sourceActorType string,
	sourceActorID sql.NullString,
	externalSource sql.NullString,
	externalReference sql.NullString,
	idempotencyKey sql.NullString,
	supersedesEventID sql.NullInt64,
	reason string,
	scheduledStartAt sql.NullTime,
	startedAt sql.NullTime,
	endedAt sql.NullTime,
	createdByUserID int64,
	createdAt time.Time,
	updatedAt time.Time,
) *models.Event {
	return &models.Event{
		ID:                      id,
		EventUUID:               eventUUID,
		OrgID:                   orgID,
		State:                   models.EventState(state),
		Mode:                    models.Mode(mode),
		Strategy:                models.Strategy(strategy),
		Level:                   models.Level(level),
		Priority:                models.Priority(priority),
		LoopType:                models.LoopType(loopType),
		ScopeType:               models.ScopeType(scopeType),
		ScopeJSON:               scopeJSON,
		ModeParamsJSON:          modeParamsJSON,
		CurtailBatchSize:        nullInt32ToPtr(curtailBatchSize),
		CurtailBatchIntervalSec: curtailBatchIntervalSec,
		RestoreBatchSize:        restoreBatchSize,
		RestoreBatchIntervalSec: restoreBatchIntervalSec,
		EffectiveBatchSize:      nullInt32ToPtr(effectiveBatchSize),
		MinCurtailedDurationSec: minCurtailedDurationSec,
		MaxDurationSeconds:      nullInt32ToPtr(maxDurationSeconds),
		AllowUnbounded:          allowUnbounded,
		IncludeMaintenance:      includeMaintenance,
		ForceIncludeMaintenance: forceIncludeMaintenance,
		DecisionSnapshotJSON:    decisionSnapshotJSON,
		SourceActorType:         models.SourceActorType(sourceActorType),
		SourceActorID:           nullStringToPtr(sourceActorID),
		ExternalSource:          nullStringToPtr(externalSource),
		ExternalReference:       nullStringToPtr(externalReference),
		IdempotencyKey:          nullStringToPtr(idempotencyKey),
		SupersedesEventID:       nullInt64ToPtr(supersedesEventID),
		Reason:                  reason,
		ScheduledStartAt:        nullTimeToPtr(scheduledStartAt),
		StartedAt:               nullTimeToPtr(startedAt),
		EndedAt:                 nullTimeToPtr(endedAt),
		CreatedByUserID:         createdByUserID,
		CreatedAt:               createdAt,
		UpdatedAt:               updatedAt,
	}
}

// convertTargetRow maps a sqlc target row to the domain Target.
func convertTargetRow(row sqlc.CurtailmentTarget) *models.Target {
	return &models.Target{
		CurtailmentEventID:    row.CurtailmentEventID,
		DeviceIdentifier:      row.DeviceIdentifier,
		TargetType:            row.TargetType,
		State:                 models.TargetState(row.State),
		DesiredState:          row.DesiredState,
		BaselinePowerW:        nullStringToFloat64Ptr(row.BaselinePowerW),
		AddedAt:               row.AddedAt,
		ReleasedAt:            nullTimeToPtr(row.ReleasedAt),
		LastDispatchedAt:      nullTimeToPtr(row.LastDispatchedAt),
		LastBatchUUID:         nullStringToPtr(row.LastBatchUuid),
		ObservedPowerW:        nullStringToFloat64Ptr(row.ObservedPowerW),
		ObservedAt:            nullTimeToPtr(row.ObservedAt),
		ConfirmedAt:           nullTimeToPtr(row.ConfirmedAt),
		RetryCount:            row.RetryCount,
		LastError:             nullStringToPtr(row.LastError),
		SelectorRationaleJSON: nullRawMessageToBytes(row.SelectorRationaleJsonb),
		CurtailPhase: models.TargetPhaseSummary{
			Phase:        models.TargetPhaseCurtail,
			State:        models.TargetState(row.CurtailState),
			StartedAt:    &row.AddedAt,
			DispatchedAt: nullTimeToPtr(row.CurtailDispatchedAt),
			BatchUUID:    nullStringToPtr(row.CurtailBatchUuid),
			CompletedAt:  nullTimeToPtr(row.CurtailCompletedAt),
			RetryCount:   row.CurtailRetryCount,
			FailureCount: row.CurtailFailureCount,
			LastError:    nullStringToPtr(row.CurtailLastError),
		},
		RestorePhase: restorePhaseFromTargetRow(row),
	}
}

func restorePhaseFromTargetRow(row sqlc.CurtailmentTarget) *models.TargetPhaseSummary {
	if !row.RestoreState.Valid {
		return nil
	}
	return &models.TargetPhaseSummary{
		Phase:        models.TargetPhaseRestore,
		State:        models.TargetState(row.RestoreState.String),
		StartedAt:    nullTimeToPtr(row.RestoreStartedAt),
		DispatchedAt: nullTimeToPtr(row.RestoreDispatchedAt),
		BatchUUID:    nullStringToPtr(row.RestoreBatchUuid),
		CompletedAt:  nullTimeToPtr(row.RestoreCompletedAt),
		RetryCount:   row.RestoreRetryCount,
		FailureCount: row.RestoreFailureCount,
		LastError:    nullStringToPtr(row.RestoreLastError),
	}
}

// --- curtailment-specific conversion helpers ---
// (generic helpers moved to helpers.go so site/building/curtailment
// stores share one canonical implementation)

func nullInt32ToPtr(n sql.NullInt32) *int32 {
	if !n.Valid {
		return nil
	}
	v := n.Int32
	return &v
}

func ptrEventStateToNullString(p *models.EventState) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*p), Valid: true}
}

func nullFloat64ToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

// ptrFloat64ToNullString formats a *float64 for a NUMERIC column.
// database/sql sends NUMERIC values as strings; full precision preserves
// the three-decimal round-trip.
func ptrFloat64ToNullString(p *float64) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{
		String: strconv.FormatFloat(*p, 'f', -1, 64),
		Valid:  true,
	}
}

func nullStringToFloat64Ptr(n sql.NullString) *float64 {
	if !n.Valid {
		return nil
	}
	v, err := strconv.ParseFloat(n.String, 64)
	if err != nil {
		// Corruption or driver mismatch: log, return nil so the selector
		// treats it as unknown and ranks it last.
		slog.Warn("failed to parse NUMERIC string", "value", n.String, "err", err)
		return nil
	}
	return &v
}

func responseProfileFromRow(row sqlc.CurtailmentResponseProfile) *models.ResponseProfile {
	return &models.ResponseProfile{
		ID:                      row.ID,
		OrgID:                   row.OrgID,
		ProfileName:             row.ProfileName,
		SiteID:                  nullInt64ToPtr(row.SiteID),
		Mode:                    models.Mode(row.Mode),
		Strategy:                models.Strategy(row.Strategy),
		Level:                   models.Level(row.Level),
		Priority:                models.Priority(row.Priority),
		TargetKW:                nullStringToFloat64Ptr(row.TargetKw),
		ToleranceKW:             nullStringToFloat64Ptr(row.ToleranceKw),
		CurtailBatchSize:        nullInt32ToPtr(row.CurtailBatchSize),
		CurtailBatchIntervalSec: row.CurtailBatchIntervalSec,
		RestoreBatchSize:        row.RestoreBatchSize,
		RestoreBatchIntervalSec: row.RestoreBatchIntervalSec,
		IncludeMaintenance:      row.IncludeMaintenance,
		ForceIncludeMaintenance: row.ForceIncludeMaintenance,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

func insertResponseProfileParams(profile models.ResponseProfile) sqlc.InsertCurtailmentResponseProfileParams {
	return sqlc.InsertCurtailmentResponseProfileParams{
		OrgID:                   profile.OrgID,
		ProfileName:             profile.ProfileName,
		SiteID:                  ptrToNullInt64(profile.SiteID),
		Mode:                    string(profile.Mode),
		Strategy:                string(profile.Strategy),
		Level:                   string(profile.Level),
		Priority:                string(profile.Priority),
		TargetKw:                ptrFloat64ToNullString(profile.TargetKW),
		ToleranceKw:             ptrFloat64ToNullString(profile.ToleranceKW),
		CurtailBatchSize:        ptrToNullInt32(profile.CurtailBatchSize),
		CurtailBatchIntervalSec: profile.CurtailBatchIntervalSec,
		RestoreBatchSize:        profile.RestoreBatchSize,
		RestoreBatchIntervalSec: profile.RestoreBatchIntervalSec,
		IncludeMaintenance:      profile.IncludeMaintenance,
		ForceIncludeMaintenance: profile.ForceIncludeMaintenance,
	}
}

func updateResponseProfileParams(profile models.ResponseProfile, expectedSiteID *int64) sqlc.UpdateCurtailmentResponseProfileParams {
	return sqlc.UpdateCurtailmentResponseProfileParams{
		ID:                      profile.ID,
		OrgID:                   profile.OrgID,
		ExpectedSiteID:          ptrToNullInt64(expectedSiteID),
		ProfileName:             profile.ProfileName,
		SiteID:                  ptrToNullInt64(profile.SiteID),
		Mode:                    string(profile.Mode),
		Strategy:                string(profile.Strategy),
		Level:                   string(profile.Level),
		Priority:                string(profile.Priority),
		TargetKw:                ptrFloat64ToNullString(profile.TargetKW),
		ToleranceKw:             ptrFloat64ToNullString(profile.ToleranceKW),
		CurtailBatchSize:        ptrToNullInt32(profile.CurtailBatchSize),
		CurtailBatchIntervalSec: profile.CurtailBatchIntervalSec,
		RestoreBatchSize:        profile.RestoreBatchSize,
		RestoreBatchIntervalSec: profile.RestoreBatchIntervalSec,
		IncludeMaintenance:      profile.IncludeMaintenance,
		ForceIncludeMaintenance: profile.ForceIncludeMaintenance,
	}
}

func mapResponseProfileWriteError(action string, err error) error {
	var fleetErr fleeterror.FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr
	}
	if isUniqueViolation(err) {
		return fleeterror.NewAlreadyExistsError("a curtailment response profile with this name already exists")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgErrCodeForeignKeyViolation:
			return fleeterror.NewNotFoundError("organization or site not found for curtailment response profile")
		case "23514": // check_violation
			return fleeterror.NewInvalidArgumentError("curtailment response profile violates persisted constraints")
		}
	}
	return fleeterror.NewInternalErrorf("failed to %s curtailment response profile: %v", action, err)
}

// bulkInsertTargetRow is the per-target JSON shape consumed by
// BulkInsertCurtailmentTargets via jsonb_to_recordset. Field names match
// the recordset column definitions.
type bulkInsertTargetRow struct {
	DeviceIdentifier       string          `json:"device_identifier"`
	TargetType             string          `json:"target_type"`
	State                  string          `json:"state"`
	DesiredState           string          `json:"desired_state"`
	BaselinePowerW         *float64        `json:"baseline_power_w"`
	SelectorRationaleJsonb json.RawMessage `json:"selector_rationale_jsonb,omitempty"`
}

// buildBulkTargetPayload serializes targets into the JSONB array for
// BulkInsertCurtailmentTargets. baseline_power_w rides as JSON number;
// NUMERIC(12,3) holds float64 precision losslessly at fleet scale.
func buildBulkTargetPayload(targets []models.InsertTargetParams) ([]byte, error) {
	rows := make([]bulkInsertTargetRow, len(targets))
	for i, t := range targets {
		var rationale json.RawMessage
		if len(t.SelectorRationaleJSON) > 0 {
			rationale = json.RawMessage(t.SelectorRationaleJSON)
		}
		rows[i] = bulkInsertTargetRow{
			DeviceIdentifier:       t.DeviceIdentifier,
			TargetType:             t.TargetType,
			State:                  string(t.State),
			DesiredState:           t.DesiredState,
			BaselinePowerW:         t.BaselinePowerW,
			SelectorRationaleJsonb: rationale,
		}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("encode bulk target payload: %v", err)
	}
	return payload, nil
}

func nullRawMessageToBytes(n pqtype.NullRawMessage) []byte {
	if !n.Valid {
		return nil
	}
	return []byte(n.RawMessage)
}
