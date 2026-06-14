package reconciler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/command"
	rollout "github.com/block/proto-fleet/server/internal/domain/firmwarerollout"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

const (
	defaultTickInterval     = 5 * time.Second
	defaultShutdownDeadline = 10 * time.Second
	defaultRefreshLimit     = int32(1000)
	defaultRunnableLimit    = int32(100)
)

type CommandDispatcher interface {
	FirmwareUpdate(ctx context.Context, selector *commandpb.DeviceSelector, firmwareFileID string) (*command.CommandResult, error)
}

type Config struct {
	TickInterval     time.Duration
	ShutdownDeadline time.Duration
}

type Reconciler struct {
	cfg  Config
	conn *sql.DB
	cmd  CommandDispatcher
	now  func() time.Time

	mu         sync.Mutex
	running    bool
	stopCancel context.CancelFunc
	workCancel context.CancelFunc
	wg         sync.WaitGroup
}

func New(cfg Config, conn *sql.DB, cmd CommandDispatcher) *Reconciler {
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = defaultTickInterval
	}
	if cfg.ShutdownDeadline <= 0 {
		cfg.ShutdownDeadline = defaultShutdownDeadline
	}
	return &Reconciler{cfg: cfg, conn: conn, cmd: cmd, now: time.Now}
}

func (r *Reconciler) Start(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	r.running = true
	stopCtx, stopCancel := context.WithCancel(context.Background())
	workCtx, workCancel := context.WithCancel(context.Background())
	r.stopCancel = stopCancel
	r.workCancel = workCancel
	r.wg.Add(1)
	go r.tickLoop(stopCtx, workCtx)
	slog.Info("firmware rollout reconciler started", "tick_interval", r.cfg.TickInterval)
	return nil
}

func (r *Reconciler) Stop() error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = false
	stopCancel := r.stopCancel
	workCancel := r.workCancel
	r.stopCancel = nil
	r.workCancel = nil
	r.mu.Unlock()

	if workCancel != nil {
		watchdog := time.AfterFunc(r.cfg.ShutdownDeadline, workCancel)
		defer watchdog.Stop()
	}
	if stopCancel != nil {
		stopCancel()
	}
	r.wg.Wait()
	if workCancel != nil {
		workCancel()
	}
	slog.Info("firmware rollout reconciler stopped")
	return nil
}

func (r *Reconciler) tickLoop(stopCtx, workCtx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCtx.Done():
			return
		case <-ticker.C:
			r.safeTick(workCtx)
		}
	}
}

func (r *Reconciler) safeTick(ctx context.Context) {
	start := r.now()
	tickUUID := uuid.New()
	activeCount := int32(0)
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("firmware rollout reconciler recovered panic", "panic", rec)
		}
		r.upsertHeartbeat(context.Background(), start, tickUUID, activeCount)
	}()

	r.refreshDispatched(ctx)
	activeCount = r.processRunnable(ctx)
}

func (r *Reconciler) refreshDispatched(ctx context.Context) {
	q := sqlc.New(db.NewRetryDB(r.conn))
	rows, err := q.ListFirmwareRolloutDispatchesToRefresh(ctx, defaultRefreshLimit)
	if err != nil {
		slog.Error("firmware rollout reconciler failed to list dispatched targets", "error", err)
		return
	}
	for _, row := range rows {
		if !row.LastCommandBatchUuid.Valid {
			continue
		}
		result, err := q.GetFirmwareRolloutCommandResult(ctx, sqlc.GetFirmwareRolloutCommandResultParams{
			Uuid:             row.LastCommandBatchUuid.String,
			DeviceIdentifier: row.DeviceIdentifier,
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			slog.Warn("firmware rollout reconciler failed to read command result", "device_identifier", row.DeviceIdentifier, "error", err)
			continue
		}
		state := "succeeded"
		status := "succeeded"
		var errorInfo sql.NullString
		if result.Status == sqlc.DeviceCommandStatusEnumFAILED {
			state = "failed"
			status = "failed"
			errorInfo = result.ErrorInfo
		}
		if err := q.MarkFirmwareRolloutTargetTerminal(ctx, sqlc.MarkFirmwareRolloutTargetTerminalParams{
			RolloutID:            row.RolloutID,
			DeviceIdentifier:     row.DeviceIdentifier,
			CurrentAttemptNumber: row.CurrentAttemptNumber,
			State:                state,
			LastError:            errorInfo,
		}); err != nil {
			slog.Warn("firmware rollout reconciler failed to mark target terminal", "device_identifier", row.DeviceIdentifier, "error", err)
			continue
		}
		if err := q.MarkFirmwareRolloutAttemptTerminal(ctx, sqlc.MarkFirmwareRolloutAttemptTerminalParams{
			RolloutID:        row.RolloutID,
			DeviceIdentifier: row.DeviceIdentifier,
			AttemptNumber:    row.CurrentAttemptNumber,
			Status:           status,
			ErrorInfo:        errorInfo,
			FinishedAt:       sql.NullTime{Time: result.UpdatedAt, Valid: true},
		}); err != nil {
			slog.Warn("firmware rollout reconciler failed to mark attempt terminal", "device_identifier", row.DeviceIdentifier, "error", err)
		}
	}
}

func (r *Reconciler) processRunnable(ctx context.Context) int32 {
	q := sqlc.New(db.NewRetryDB(r.conn))
	rollouts, err := q.ListRunnableFirmwareRollouts(ctx, defaultRunnableLimit)
	if err != nil {
		slog.Error("firmware rollout reconciler failed to list runnable rollouts", "error", err)
		return 0
	}
	for _, row := range rollouts {
		r.processRollout(ctx, row)
	}
	return int32(len(rollouts)) //nolint:gosec // limited by query
}

func (r *Reconciler) processRollout(ctx context.Context, row sqlc.FirmwareRollout) {
	q := sqlc.New(db.NewRetryDB(r.conn))
	hasWork, err := q.FirmwareRolloutHasPendingOrInProgressTargets(ctx, row.ID)
	if err != nil {
		slog.Warn("firmware rollout reconciler failed to check rollout work", "rollout_id", row.RolloutUuid, "error", err)
		return
	}
	if !hasWork {
		r.terminalize(ctx, row)
		return
	}
	if row.LastBatchAt.Valid && r.now().Before(row.LastBatchAt.Time.Add(time.Duration(row.BatchIntervalSec)*time.Second)) {
		return
	}
	claimed, err := q.ClaimFirmwareRolloutTargetsForDispatch(ctx, sqlc.ClaimFirmwareRolloutTargetsForDispatchParams{
		RolloutID: row.ID,
		Limit:     row.BatchSize,
	})
	if err != nil {
		slog.Warn("firmware rollout reconciler failed to claim targets", "rollout_id", row.RolloutUuid, "error", err)
		return
	}
	if len(claimed) == 0 {
		return
	}
	ids := make([]string, len(claimed))
	for i, item := range claimed {
		ids[i] = item.DeviceIdentifier
	}
	cmdCtx := command.WithCommandActivitySuppressed(rollout.SessionForReconciler(ctx, row.OrgID, row.CreatedBy))
	result, err := r.cmd.FirmwareUpdate(cmdCtx, &commandpb.DeviceSelector{
		SelectionType: &commandpb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: ids},
		},
	}, row.FirmwareFileID)
	if err != nil {
		r.markDispatchFailure(ctx, claimed, err.Error())
		return
	}
	dispatched := map[string]struct{}{}
	for _, id := range result.DispatchedDeviceIdentifiers {
		dispatched[id] = struct{}{}
	}
	if result.BatchIdentifier != "" {
		if err := q.TouchFirmwareRolloutBatchDispatch(ctx, row.ID); err != nil {
			slog.Warn("firmware rollout reconciler failed to touch batch dispatch", "rollout_id", row.RolloutUuid, "error", err)
		}
	}
	for _, item := range claimed {
		if _, ok := dispatched[item.DeviceIdentifier]; !ok {
			r.markSingleDispatchFailure(ctx, item, "device was skipped by command preflight filters")
			continue
		}
		batchID := sql.NullString{String: result.BatchIdentifier, Valid: result.BatchIdentifier != ""}
		if err := q.MarkFirmwareRolloutAttemptDispatched(ctx, sqlc.MarkFirmwareRolloutAttemptDispatchedParams{
			RolloutID:        item.RolloutID,
			DeviceIdentifier: item.DeviceIdentifier,
			AttemptNumber:    item.AttemptNumber,
			CommandBatchUuid: batchID,
		}); err != nil {
			slog.Warn("firmware rollout reconciler failed to mark attempt dispatched", "device_identifier", item.DeviceIdentifier, "error", err)
			continue
		}
		if err := q.MarkFirmwareRolloutTargetDispatched(ctx, sqlc.MarkFirmwareRolloutTargetDispatchedParams{
			RolloutID:            item.RolloutID,
			DeviceIdentifier:     item.DeviceIdentifier,
			LastCommandBatchUuid: batchID,
		}); err != nil {
			slog.Warn("firmware rollout reconciler failed to mark target dispatched", "device_identifier", item.DeviceIdentifier, "error", err)
		}
	}
}

func (r *Reconciler) terminalize(ctx context.Context, row sqlc.FirmwareRollout) {
	q := sqlc.New(db.NewRetryDB(r.conn))
	hasFailures, err := q.FirmwareRolloutHasFailedTargets(ctx, row.ID)
	if err != nil {
		slog.Warn("firmware rollout reconciler failed to check rollout failures", "rollout_id", row.RolloutUuid, "error", err)
		return
	}
	state := rollout.StateCompleted
	if hasFailures {
		state = rollout.StateCompletedWithFailures
	}
	if _, err := q.MarkFirmwareRolloutTerminal(ctx, sqlc.MarkFirmwareRolloutTerminalParams{
		ID:    row.ID,
		OrgID: row.OrgID,
		State: state,
	}); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("firmware rollout reconciler failed to mark rollout terminal", "rollout_id", row.RolloutUuid, "error", err)
	}
}

func (r *Reconciler) markDispatchFailure(ctx context.Context, claimed []sqlc.ClaimFirmwareRolloutTargetsForDispatchRow, message string) {
	for _, item := range claimed {
		r.markSingleDispatchFailure(ctx, item, message)
	}
}

func (r *Reconciler) markSingleDispatchFailure(ctx context.Context, item sqlc.ClaimFirmwareRolloutTargetsForDispatchRow, message string) {
	q := sqlc.New(db.NewRetryDB(r.conn))
	errorInfo := sql.NullString{String: message, Valid: message != ""}
	if err := q.MarkFirmwareRolloutDispatchFailed(ctx, sqlc.MarkFirmwareRolloutDispatchFailedParams{
		RolloutID:            item.RolloutID,
		DeviceIdentifier:     item.DeviceIdentifier,
		CurrentAttemptNumber: item.AttemptNumber,
		LastError:            errorInfo,
	}); err != nil {
		slog.Warn("firmware rollout reconciler failed to mark dispatch failure", "device_identifier", item.DeviceIdentifier, "error", err)
	}
	if err := q.MarkFirmwareRolloutAttemptFailed(ctx, sqlc.MarkFirmwareRolloutAttemptFailedParams{
		RolloutID:        item.RolloutID,
		DeviceIdentifier: item.DeviceIdentifier,
		AttemptNumber:    item.AttemptNumber,
		ErrorInfo:        errorInfo,
	}); err != nil {
		slog.Warn("firmware rollout reconciler failed to mark attempt failure", "device_identifier", item.DeviceIdentifier, "error", err)
	}
}

func (r *Reconciler) upsertHeartbeat(ctx context.Context, start time.Time, tickUUID uuid.UUID, activeCount int32) {
	duration := int32(r.now().Sub(start).Milliseconds()) //nolint:gosec // tick durations fit in int32
	if err := sqlc.New(db.NewRetryDB(r.conn)).UpsertFirmwareRolloutHeartbeat(ctx, sqlc.UpsertFirmwareRolloutHeartbeatParams{
		LastTickAt:         start,
		LastTickUuid:       tickUUID,
		LastTickDurationMs: sql.NullInt32{Int32: duration, Valid: true},
		ActiveRolloutCount: activeCount,
	}); err != nil {
		slog.Warn("firmware rollout reconciler failed to upsert heartbeat", "error", err)
	}
}
