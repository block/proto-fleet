package firmwarerollout

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/authn"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/firmwarerollout/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

const (
	StateDraft                 = "draft"
	StateRunning               = "running"
	StatePaused                = "paused"
	StateCompleted             = "completed"
	StateCompletedWithFailures = "completed_with_failures"
	StateCanceled              = "canceled"

	TargetStateFailed = "failed"

	scopeDeviceList = "device_list"
	scopeAllDevices = "all_devices"

	defaultPageSize        = 50
	maxRolloutPageSize     = 100
	maxTargetPageSize      = 500
	defaultBatchSize       = int32(25)
	defaultIntervalSeconds = int32(300)
)

type Service struct {
	conn        *sql.DB
	files       *files.Service
	deviceStore stores.DeviceStore
	activity    *activity.Service
}

type RolloutDetail struct {
	Rollout sqlc.FirmwareRollout
	Counts  sqlc.GetFirmwareRolloutCountsRow
}

type TargetPage struct {
	Targets       []sqlc.ListFirmwareRolloutTargetsRow
	NextPageToken string
}

type RolloutPage struct {
	Rollouts      []RolloutDetail
	NextPageToken string
}

func NewService(conn *sql.DB, filesService *files.Service, deviceStore stores.DeviceStore, activitySvc *activity.Service) *Service {
	return &Service{conn: conn, files: filesService, deviceStore: deviceStore, activity: activitySvc}
}

func (s *Service) Create(ctx context.Context, req *pb.CreateFirmwareRolloutRequest) (*RolloutDetail, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, fleeterror.NewInvalidArgumentError("name is required")
	}
	if _, err := s.files.GetFirmwareFilePath(req.GetFirmwareFileId()); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid firmware_file_id: %v", err)
	}
	selector := req.GetDeviceSelector()
	if selector == nil {
		return nil, fleeterror.NewInvalidArgumentError("device_selector is required")
	}
	scopeType, scopeJSON, err := encodeScope(selector)
	if err != nil {
		return nil, err
	}
	batchSize := req.GetBatchSize()
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	intervalSec := req.GetBatchIntervalSeconds()
	if intervalSec <= 0 {
		intervalSec = defaultIntervalSeconds
	}

	row, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.FirmwareRollout, error) {
		return q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
			RolloutUuid:      uuid.New(),
			OrgID:            info.OrganizationID,
			Name:             name,
			FirmwareFileID:   req.GetFirmwareFileId(),
			BatchSize:        batchSize,
			BatchIntervalSec: intervalSec,
			ScopeType:        scopeType,
			ScopeJsonb:       scopeJSON,
			CreatedBy:        info.UserID,
		})
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to create firmware rollout: %v", err)
	}
	s.logEvent(ctx, row.ID, "created", "Created firmware rollout", map[string]any{
		"rollout_id":       row.RolloutUuid.String(),
		"firmware_file_id": row.FirmwareFileID,
	})
	return s.detailForRow(ctx, row)
}

func (s *Service) Start(ctx context.Context, rolloutID string) (*RolloutDetail, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	rollout, err := s.getRow(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		return nil, err
	}
	if rollout.State != StateDraft {
		return nil, fleeterror.NewFailedPreconditionError("firmware rollout must be draft to start")
	}
	targets, err := s.resolveTargets(ctx, info.OrganizationID, rollout.ScopeJsonb)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("no devices matched selector")
	}
	row, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.FirmwareRollout, error) {
		started, err := q.StartFirmwareRollout(ctx, sqlc.StartFirmwareRolloutParams{
			ID:          rollout.ID,
			OrgID:       info.OrganizationID,
			TargetCount: int32(len(targets)), //nolint:gosec // bounded by fleet size
		})
		if err != nil {
			return sqlc.FirmwareRollout{}, err
		}
		for _, id := range targets {
			if err := q.InsertFirmwareRolloutTarget(ctx, sqlc.InsertFirmwareRolloutTargetParams{
				RolloutID:        rollout.ID,
				DeviceIdentifier: id,
			}); err != nil {
				return sqlc.FirmwareRollout{}, err
			}
		}
		return started, nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewFailedPreconditionError("firmware rollout must be draft to start")
		}
		return nil, fleeterror.NewInternalErrorf("failed to start firmware rollout: %v", err)
	}
	s.logEvent(ctx, row.ID, "started", "Started firmware rollout", map[string]any{"target_count": len(targets)})
	return s.detailForRow(ctx, row)
}

func (s *Service) Pause(ctx context.Context, rolloutID string) (*RolloutDetail, error) {
	row, err := s.transitionByUUID(ctx, rolloutID, "pause", func(ctx context.Context, q *sqlc.Queries, id uuid.UUID, orgID int64) (sqlc.FirmwareRollout, error) {
		return q.PauseFirmwareRollout(ctx, sqlc.PauseFirmwareRolloutParams{RolloutUuid: id, OrgID: orgID})
	})
	if err != nil {
		return nil, err
	}
	s.logEvent(ctx, row.ID, "paused", "Paused firmware rollout", nil)
	return s.detailForRow(ctx, row)
}

func (s *Service) Resume(ctx context.Context, rolloutID string) (*RolloutDetail, error) {
	row, err := s.transitionByUUID(ctx, rolloutID, "resume", func(ctx context.Context, q *sqlc.Queries, id uuid.UUID, orgID int64) (sqlc.FirmwareRollout, error) {
		return q.ResumeFirmwareRollout(ctx, sqlc.ResumeFirmwareRolloutParams{RolloutUuid: id, OrgID: orgID})
	})
	if err != nil {
		return nil, err
	}
	s.logEvent(ctx, row.ID, "resumed", "Resumed firmware rollout", nil)
	return s.detailForRow(ctx, row)
}

func (s *Service) Cancel(ctx context.Context, rolloutID string) (*RolloutDetail, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	id, err := uuid.Parse(rolloutID)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid rollout_id")
	}
	row, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.FirmwareRollout, error) {
		row, err := q.CancelFirmwareRollout(ctx, sqlc.CancelFirmwareRolloutParams{RolloutUuid: id, OrgID: info.OrganizationID})
		if err != nil {
			return sqlc.FirmwareRollout{}, err
		}
		_, err = q.BulkCancelPendingFirmwareRolloutTargets(ctx, row.ID)
		return row, err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewFailedPreconditionError("firmware rollout is not cancelable")
		}
		return nil, fleeterror.NewInternalErrorf("failed to cancel firmware rollout: %v", err)
	}
	s.logEvent(ctx, row.ID, "canceled", "Canceled firmware rollout", nil)
	return s.detailForRow(ctx, row)
}

func (s *Service) RetryFailed(ctx context.Context, rolloutID string) (*RolloutDetail, int32, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, 0, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	row, err := s.getRow(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		return nil, 0, err
	}
	if row.State != StateCompletedWithFailures && row.State != StatePaused {
		return nil, 0, fleeterror.NewFailedPreconditionError("firmware rollout has no retryable failed targets")
	}
	var retried int64
	updated, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.FirmwareRollout, error) {
		count, err := q.ResetFailedFirmwareRolloutTargetsForRetry(ctx, row.ID)
		if err != nil {
			return sqlc.FirmwareRollout{}, err
		}
		retried = count
		if count == 0 {
			return sqlc.FirmwareRollout{}, fleeterror.NewFailedPreconditionError("firmware rollout has no failed targets to retry")
		}
		return q.ReopenFirmwareRolloutForRetry(ctx, sqlc.ReopenFirmwareRolloutForRetryParams{ID: row.ID, OrgID: row.OrgID})
	})
	if err != nil {
		return nil, 0, err
	}
	s.logEvent(ctx, row.ID, "retry_started", "Retried failed firmware rollout targets", map[string]any{"retried_count": retried})
	detail, err := s.detailForRow(ctx, updated)
	return detail, int32(retried), err //nolint:gosec // bounded by target count
}

func (s *Service) List(ctx context.Context, pageSize int32, pageToken string) (*RolloutPage, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	limit := normalizePageSize(pageSize, defaultPageSize, maxRolloutPageSize) + 1
	cursorTime, cursorID, err := decodeRolloutCursor(pageToken)
	if err != nil {
		return nil, err
	}
	var cursorTimeArg sql.NullTime
	var cursorIDArg sql.NullInt64
	if !cursorTime.IsZero() {
		cursorTimeArg = sql.NullTime{Time: cursorTime, Valid: true}
		cursorIDArg = sql.NullInt64{Int64: cursorID, Valid: true}
	}
	rows, err := sqlc.New(db.NewRetryDB(s.conn)).ListFirmwareRolloutsByOrg(ctx, sqlc.ListFirmwareRolloutsByOrgParams{
		OrgID:           info.OrganizationID,
		CursorCreatedAt: cursorTimeArg,
		CursorID:        cursorIDArg,
		PageSize:        limit,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list firmware rollouts: %v", err)
	}
	next := ""
	if len(rows) == int(limit) {
		last := rows[limit-1]
		next = encodeRolloutCursor(last.CreatedAt, last.ID)
		rows = rows[:limit-1]
	}
	out := make([]RolloutDetail, 0, len(rows))
	for _, row := range rows {
		detail, err := s.detailForRow(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, *detail)
	}
	return &RolloutPage{Rollouts: out, NextPageToken: next}, nil
}

func (s *Service) Get(ctx context.Context, rolloutID string) (*RolloutDetail, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	row, err := s.getRow(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		return nil, err
	}
	return s.detailForRow(ctx, row)
}

func (s *Service) ListTargets(ctx context.Context, rolloutID string, pageSize int32, pageToken string, stateFilter string) (*TargetPage, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	row, err := s.getRow(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeTargetCursor(pageToken)
	if err != nil {
		return nil, err
	}
	var cursorArg sql.NullString
	if cursor != "" {
		cursorArg = sql.NullString{String: cursor, Valid: true}
	}
	var stateArg sql.NullString
	if stateFilter != "" {
		stateArg = sql.NullString{String: stateFilter, Valid: true}
	}
	limit := normalizePageSize(pageSize, defaultPageSize, maxTargetPageSize) + 1
	rows, err := sqlc.New(db.NewRetryDB(s.conn)).ListFirmwareRolloutTargets(ctx, sqlc.ListFirmwareRolloutTargetsParams{
		RolloutID:              row.ID,
		StateFilter:            stateArg,
		CursorDeviceIdentifier: cursorArg,
		PageSize:               limit,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list firmware rollout targets: %v", err)
	}
	next := ""
	if len(rows) == int(limit) {
		last := rows[limit-1]
		next = encodeTargetCursor(last.DeviceIdentifier)
		rows = rows[:limit-1]
	}
	return &TargetPage{Targets: rows, NextPageToken: next}, nil
}

func (s *Service) ListEvents(ctx context.Context, rolloutID string) ([]sqlc.FirmwareRolloutEvent, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	row, err := s.getRow(ctx, info.OrganizationID, rolloutID)
	if err != nil {
		return nil, err
	}
	events, err := sqlc.New(db.NewRetryDB(s.conn)).ListFirmwareRolloutEvents(ctx, row.ID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to list firmware rollout events: %v", err)
	}
	return events, nil
}

func (s *Service) getRow(ctx context.Context, orgID int64, rolloutID string) (sqlc.FirmwareRollout, error) {
	id, err := uuid.Parse(rolloutID)
	if err != nil {
		return sqlc.FirmwareRollout{}, fleeterror.NewInvalidArgumentError("invalid rollout_id")
	}
	row, err := sqlc.New(db.NewRetryDB(s.conn)).GetFirmwareRolloutByUUID(ctx, sqlc.GetFirmwareRolloutByUUIDParams{RolloutUuid: id, OrgID: orgID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.FirmwareRollout{}, fleeterror.NewNotFoundErrorf("firmware rollout not found: %s", rolloutID)
		}
		return sqlc.FirmwareRollout{}, fleeterror.NewInternalErrorf("failed to get firmware rollout: %v", err)
	}
	return row, nil
}

func (s *Service) detailForRow(ctx context.Context, row sqlc.FirmwareRollout) (*RolloutDetail, error) {
	counts, err := sqlc.New(db.NewRetryDB(s.conn)).GetFirmwareRolloutCounts(ctx, row.ID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to get firmware rollout counts: %v", err)
	}
	return &RolloutDetail{Rollout: row, Counts: counts}, nil
}

func (s *Service) transitionByUUID(ctx context.Context, rolloutID, verb string, fn func(context.Context, *sqlc.Queries, uuid.UUID, int64) (sqlc.FirmwareRollout, error)) (sqlc.FirmwareRollout, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return sqlc.FirmwareRollout{}, fleeterror.NewInternalErrorf("error getting session info: %v", err)
	}
	id, err := uuid.Parse(rolloutID)
	if err != nil {
		return sqlc.FirmwareRollout{}, fleeterror.NewInvalidArgumentError("invalid rollout_id")
	}
	row, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) (sqlc.FirmwareRollout, error) {
		return fn(ctx, q, id, info.OrganizationID)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlc.FirmwareRollout{}, fleeterror.NewFailedPreconditionErrorf("firmware rollout is not valid to %s", verb)
		}
		return sqlc.FirmwareRollout{}, fleeterror.NewInternalErrorf("failed to %s firmware rollout: %v", verb, err)
	}
	return row, nil
}

func (s *Service) resolveTargets(ctx context.Context, orgID int64, scopeJSON json.RawMessage) ([]string, error) {
	var selector commonpb.DeviceSelector
	if err := protojson.Unmarshal(scopeJSON, &selector); err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to decode firmware rollout scope: %v", err)
	}
	if list := selector.GetDeviceList(); list != nil {
		ids := dedupeStrings(list.DeviceIdentifiers)
		if len(ids) == 0 {
			return nil, nil
		}
		ok, err := s.deviceStore.AllDevicesBelongToOrg(ctx, ids, orgID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fleeterror.NewForbiddenErrorf("one or more selected devices do not belong to this organization")
		}
		return ids, nil
	}
	if selector.GetAllDevices() {
		return s.deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, nil)
	}
	return nil, fleeterror.NewInvalidArgumentError("unsupported device selector")
}

func (s *Service) logEvent(ctx context.Context, rolloutID int64, eventType, message string, metadata map[string]any) {
	info, _ := session.GetInfo(ctx)
	var userID sql.NullString
	var username sql.NullString
	actorType := "system"
	if info != nil {
		actorType = "user"
		userID = sql.NullString{String: info.ExternalUserID, Valid: info.ExternalUserID != ""}
		username = sql.NullString{String: info.Username, Valid: info.Username != ""}
	}
	raw, marshalErr := json.Marshal(metadataOrEmpty(metadata))
	if marshalErr != nil {
		raw = []byte("{}")
	}
	_ = sqlc.New(db.NewRetryDB(s.conn)).InsertFirmwareRolloutEvent(ctx, sqlc.InsertFirmwareRolloutEventParams{
		RolloutID: rolloutID,
		EventType: eventType,
		ActorType: actorType,
		UserID:    userID,
		Username:  username,
		Message:   message,
		Metadata:  raw,
	})
	if s.activity != nil && info != nil {
		rolloutIDCopy := fmt.Sprint(rolloutID)
		count := 1
		s.activity.Log(ctx, activitymodels.Event{
			Category:       activitymodels.CategoryFirmwareRollout,
			Type:           "firmware_rollout." + eventType,
			Description:    message,
			Result:         activitymodels.ResultSuccess,
			ScopeType:      ptr("firmware_rollout"),
			ScopeLabel:     &rolloutIDCopy,
			ScopeCount:     &count,
			ActorType:      activitymodels.ActorUser,
			UserID:         &info.ExternalUserID,
			Username:       &info.Username,
			OrganizationID: &info.OrganizationID,
			Metadata:       metadataOrEmpty(metadata),
		})
	}
}

func encodeScope(selector *commonpb.DeviceSelector) (string, json.RawMessage, error) {
	var scopeType string
	switch {
	case selector.GetDeviceList() != nil:
		scopeType = scopeDeviceList
	case selector.GetAllDevices():
		scopeType = scopeAllDevices
	default:
		return "", nil, fleeterror.NewInvalidArgumentError("unsupported device selector")
	}
	raw, err := protojson.Marshal(selector)
	if err != nil {
		return "", nil, fleeterror.NewInvalidArgumentErrorf("invalid device selector: %v", err)
	}
	return scopeType, raw, nil
}

func normalizePageSize(value, fallback, maxValue int32) int32 {
	if value <= 0 {
		return fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func encodeRolloutCursor(createdAt time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", createdAt.UnixNano(), id)))
}

func decodeRolloutCursor(token string) (time.Time, int64, error) {
	if token == "" {
		return time.Time{}, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, 0, fleeterror.NewInvalidArgumentError("invalid page_token")
	}
	var unixNano int64
	var id int64
	if _, err := fmt.Sscanf(string(raw), "%d:%d", &unixNano, &id); err != nil {
		return time.Time{}, 0, fleeterror.NewInvalidArgumentError("invalid page_token")
	}
	return time.Unix(0, unixNano), id, nil
}

func encodeTargetCursor(deviceIdentifier string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(deviceIdentifier))
}

func decodeTargetCursor(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fleeterror.NewInvalidArgumentError("invalid page_token")
	}
	return string(raw), nil
}

func metadataOrEmpty(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func ptr[T any](v T) *T {
	return &v
}

func SessionForReconciler(parent context.Context, orgID int64, userID int64) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      "firmware-rollout-reconciler",
		UserID:         userID,
		OrganizationID: orgID,
		ExternalUserID: "firmware-rollout-reconciler",
		Username:       "firmware-rollout-reconciler",
		Actor:          session.ActorFirmwareRollout,
	})
}
