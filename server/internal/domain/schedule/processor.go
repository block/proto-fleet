package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/authn"
	"github.com/robfig/cron/v3"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/schedule/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	reconcileInterval     = 60 * time.Second
	endOfWindowInterval   = 30 * time.Second
	revertPerformanceMode = commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
	schedulerActorName    = "scheduler"
	oneTimeRetryDelay     = time.Second
)

// CommandDispatcher is the subset of command.Service the processor needs.
//
// Each method returns a *command.CommandResult so the processor can read the
// per-call list of devices the registered command preflight filters chose
// to skip — primarily the schedule-conflict filter, which today only the
// schedule processor cared about but now lives at the command-service layer
// and runs for every caller (manual API, future curtailment, …). The
// processor uses the skipped slice to emit `schedule_conflict_skip` activity
// for both the normal dispatch and the end-of-window revert paths.
type CommandDispatcher interface {
	SetPowerTarget(ctx context.Context, selector *commandpb.DeviceSelector, mode commandpb.PerformanceMode) (*command.CommandResult, error)
	Reboot(ctx context.Context, selector *commandpb.DeviceSelector) (*command.CommandResult, error)
	StopMining(ctx context.Context, selector *commandpb.DeviceSelector) (*command.CommandResult, error)
}

// jobEntry tracks a registered job and a fingerprint of the schedule's timing
// fields so that edits can be detected during reconciliation. For recurring
// schedules entryID holds the cron.EntryID; for one-time schedules timer holds
// a *time.Timer that fires the callback.
type jobEntry struct {
	entryID     cron.EntryID
	timer       *time.Timer
	isOneTime   bool
	fingerprint string
}

type Processor struct {
	cron            *cron.Cron
	procStore       interfaces.ScheduleProcessorStore
	targetStore     interfaces.ScheduleTargetStore
	collectionStore interfaces.CollectionStore
	commandSvc      CommandDispatcher
	activitySvc     *activity.Service
	now             func() time.Time

	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	jobs   map[int64]jobEntry
}

func NewProcessor(
	procStore interfaces.ScheduleProcessorStore,
	targetStore interfaces.ScheduleTargetStore,
	collectionStore interfaces.CollectionStore,
	commandSvc CommandDispatcher,
	activitySvc *activity.Service,
) *Processor {
	return &Processor{
		procStore:       procStore,
		targetStore:     targetStore,
		collectionStore: collectionStore,
		commandSvc:      commandSvc,
		activitySvc:     activitySvc,
		now:             time.Now,
		jobs:            make(map[int64]jobEntry),
	}
}

// scheduleFingerprint returns a string derived from the schedule's timing fields.
// A change in fingerprint means the job must be re-registered.
func scheduleFingerprint(sched *pb.Schedule) string {
	parts := []string{
		sched.ScheduleType.String(),
		sched.StartDate,
		sched.StartTime,
		sched.Timezone,
	}
	if rec := sched.Recurrence; rec != nil {
		parts = append(parts, rec.Frequency.String())
		for _, d := range rec.DaysOfWeek {
			parts = append(parts, d.String())
		}
		if rec.DayOfMonth != nil {
			parts = append(parts, fmt.Sprintf("%d", *rec.DayOfMonth))
		}
	}
	return strings.Join(parts, "|")
}

func (p *Processor) Start(_ context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	p.cron = cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))

	if err := p.recoverStaleRunning(ctx); err != nil {
		cancel()
		return fmt.Errorf("schedule processor startup: %w", err)
	}
	if err := p.syncSchedules(ctx); err != nil {
		cancel()
		return fmt.Errorf("schedule processor startup: %w", err)
	}

	p.cron.Start()

	p.wg.Add(2)
	go p.reconcileLoop(ctx)
	go p.endOfWindowLoop(ctx)

	slog.Info("schedule processor started")
	return nil
}

func (p *Processor) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.cron != nil {
		cronCtx := p.cron.Stop()
		<-cronCtx.Done()
	}
	p.wg.Wait()
	p.mu.Lock()
	for _, entry := range p.jobs {
		if entry.isOneTime && entry.timer != nil {
			entry.timer.Stop()
		}
	}
	p.mu.Unlock()
	slog.Info("schedule processor stopped")
	return nil
}

func (p *Processor) reconcileLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.syncSchedules(ctx); err != nil {
				slog.Error("reconciliation failed, will retry next cycle", "error", err)
			}
		}
	}
}

func (p *Processor) endOfWindowLoop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(endOfWindowInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkEndOfWindow(ctx)
		}
	}
}

// recoverStaleRunning resets schedules stuck in "running" from a previous crash.
// Power-target schedules with end_time and a non-nil last_run_at are legitimately
// running (checkEndOfWindow handles them). All others — including window schedules
// with nil last_run_at (crash before updateAfterRun) — should be reset to active.
func (p *Processor) recoverStaleRunning(ctx context.Context) error {
	schedules, err := p.procStore.GetActiveSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules for stale recovery: %w", err)
	}
	for _, sw := range schedules {
		if sw.Schedule.Status != pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING {
			continue
		}
		legitimateWindow := sw.Schedule.Action == pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET &&
			sw.Schedule.EndTime != "" && sw.Schedule.LastRunAt != nil
		if !legitimateWindow {
			slog.Info("resetting stale running schedule on startup", "schedule_id", sw.Schedule.Id)
			if err := p.procStore.RevertScheduleToActive(ctx, sw.Schedule.Id); err != nil {
				return fmt.Errorf("failed to reset stale running schedule %d: %w", sw.Schedule.Id, err)
			}
		}
	}
	return nil
}

// syncSchedules loads active/running schedules from the DB, diffs against
// registered jobs, and adds/removes/updates as needed.
func (p *Processor) syncSchedules(ctx context.Context) error {
	schedules, err := p.procStore.GetActiveSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load active schedules: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	activeIDs := make(map[int64]struct{}, len(schedules))
	for _, sw := range schedules {
		activeIDs[sw.Schedule.Id] = struct{}{}

		fp := scheduleFingerprint(sw.Schedule)
		if entry, exists := p.jobs[sw.Schedule.Id]; exists {
			if entry.fingerprint == fp {
				continue // unchanged
			}
			p.removeJobLocked(sw.Schedule.Id)
		}

		if err := p.registerJob(ctx, sw.Schedule); err != nil {
			slog.Error("failed to register job", "schedule_id", sw.Schedule.Id, "error", err)
		}
	}

	for id := range p.jobs {
		if _, active := activeIDs[id]; !active {
			p.removeJobLocked(id)
		}
	}
	return nil
}

func (p *Processor) registerJob(ctx context.Context, sched *pb.Schedule) error {
	scheduleID := sched.Id

	if sched.ScheduleType == pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME {
		t, err := ParseScheduleTime(sched.StartDate, sched.StartTime, sched.Timezone)
		if err != nil {
			return fmt.Errorf("failed to parse one-time schedule time: %w", err)
		}
		delay := time.Until(t)
		if delay < oneTimeRetryDelay {
			delay = oneTimeRetryDelay
		}
		timer := time.AfterFunc(delay, func() { p.executeSchedule(ctx, scheduleID) })
		p.jobs[scheduleID] = jobEntry{timer: timer, isOneTime: true, fingerprint: scheduleFingerprint(sched)}
		return nil
	}

	rec := sched.Recurrence
	if rec == nil {
		return fmt.Errorf("recurring schedule %d missing recurrence", sched.Id)
	}
	cronExpr, err := ToCronExpression(rec.Frequency, sched.StartTime, sched.Timezone, rec.DaysOfWeek, rec.DayOfMonth)
	if err != nil {
		return fmt.Errorf("failed to build cron expression: %w", err)
	}

	entryID, err := p.cron.AddFunc(cronExpr, func() { p.executeSchedule(ctx, scheduleID) })
	if err != nil {
		return fmt.Errorf("failed to register cron job: %w", err)
	}

	p.jobs[scheduleID] = jobEntry{entryID: entryID, fingerprint: scheduleFingerprint(sched)}
	return nil
}

// executeSchedule is called when a job fires.
func (p *Processor) executeSchedule(ctx context.Context, scheduleID int64) {
	slog.Info("executing schedule", "schedule_id", scheduleID)

	rows, err := p.procStore.SetScheduleRunning(ctx, scheduleID)
	if err != nil {
		slog.Error("failed to set schedule running", "schedule_id", scheduleID, "error", err)
		return
	}
	if rows == 0 {
		slog.Info("schedule no longer active, skipping execution", "schedule_id", scheduleID)
		return
	}

	sw, err := p.procStore.GetScheduleByID(ctx, scheduleID)
	if err != nil {
		slog.Error("failed to re-read schedule after status transition", "schedule_id", scheduleID, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, scheduleID); rerr != nil {
			slog.Error("failed to revert schedule after read failure", "schedule_id", scheduleID, "error", rerr)
			return
		}
		p.removeJob(scheduleID)
		return
	}

	sched := sw.Schedule
	orgID := sw.OrgID
	now := p.now()

	// Guard against cron firing before the configured start_date.
	if sched.StartDate != "" {
		startDate, err := parseDateInLocation(sched.StartDate, sched.Timezone)
		if err == nil && now.Before(startDate) {
			slog.Info("schedule start_date not reached, skipping execution", "schedule_id", scheduleID)
			if rerr := p.procStore.RevertScheduleToActive(ctx, scheduleID); rerr != nil {
				slog.Error("failed to revert schedule before start_date", "schedule_id", scheduleID, "error", rerr)
			}
			return
		}
	}

	if sched.EndDate != "" {
		deadline, err := parseDateInLocation(sched.EndDate, sched.Timezone)
		if err == nil && now.After(endOfDay(deadline)) {
			p.transitionToCompleted(ctx, sched, orgID, now)
			return
		}
	}

	deviceIdentifiers, err := p.resolveTargets(ctx, sched, orgID)
	if err != nil {
		slog.Error("failed to resolve targets", "schedule_id", scheduleID, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, scheduleID); rerr != nil {
			slog.Error("failed to revert schedule after target resolution failure", "schedule_id", scheduleID, "error", rerr)
			return
		}
		p.removeJob(scheduleID)
		return
	}

	if len(deviceIdentifiers) == 0 {
		slog.Info("no target devices resolved, skipping dispatch", "schedule_id", scheduleID)
		p.updateAfterRun(ctx, sched, orgID, now)
		return
	}

	cmdCtx := schedulerContext(ctx, sched, orgID)
	selector := &commandpb.DeviceSelector{
		SelectionType: &commandpb.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: deviceIdentifiers,
			},
		},
	}

	// Dispatch through commandSvc — schedule-conflict filtering runs at the
	// command-service layer for every caller now (see ScheduleConflictFilter).
	// We read the skipped slice back to preserve the schedule_conflict_skip
	// activity row.
	result, err := p.dispatch(cmdCtx, sched, selector)
	if err != nil {
		slog.Error("failed to dispatch command", "schedule_id", scheduleID, "action", sched.Action, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, scheduleID); rerr != nil {
			slog.Error("failed to revert schedule after dispatch failure", "schedule_id", scheduleID, "error", rerr)
			return
		}
		// Remove the job so syncSchedules re-registers it. This is necessary
		// for one-time schedules whose timer has already fired and won't retrigger.
		p.removeJob(scheduleID)
		return
	}

	if skipped := countConflictSkips(result); skipped > 0 {
		p.logConflictSkip(ctx, sched, orgID, skipped)
	}

	dispatched := 0
	if result != nil {
		dispatched = result.DispatchedCount
		if dispatched == 0 && len(result.Skipped) > 0 {
			slog.Info("all miners overridden by preflight filters", "schedule_id", scheduleID)
		}
	}

	p.updateAfterRun(ctx, sched, orgID, now)
	p.logExecution(ctx, sched, orgID, dispatched)
}

// countConflictSkips returns how many devices the schedule-conflict filter
// excluded from this dispatch. result may be nil for actions that don't fan
// out to commandSvc (none today, but defensive); other filters' skips do not
// count.
func countConflictSkips(result *command.CommandResult) int {
	if result == nil {
		return 0
	}
	n := 0
	for _, s := range result.Skipped {
		if s.FilterName == "schedule_conflict" {
			n++
		}
	}
	return n
}

func (p *Processor) dispatch(ctx context.Context, sched *pb.Schedule, selector *commandpb.DeviceSelector) (*command.CommandResult, error) {
	switch sched.Action {
	case pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET:
		mode := commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
		if sched.ActionConfig != nil {
			switch sched.ActionConfig.Mode {
			case pb.PowerTargetMode_POWER_TARGET_MODE_MAX:
				mode = commandpb.PerformanceMode_PERFORMANCE_MODE_MAXIMUM_HASHRATE
			case pb.PowerTargetMode_POWER_TARGET_MODE_DEFAULT:
				mode = commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
			case pb.PowerTargetMode_POWER_TARGET_MODE_UNSPECIFIED:
				mode = commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
			}
		}
		return p.commandSvc.SetPowerTarget(ctx, selector, mode)

	case pb.ScheduleAction_SCHEDULE_ACTION_REBOOT:
		return p.commandSvc.Reboot(ctx, selector)

	case pb.ScheduleAction_SCHEDULE_ACTION_SLEEP:
		return p.commandSvc.StopMining(ctx, selector)

	case pb.ScheduleAction_SCHEDULE_ACTION_UNSPECIFIED:
		return nil, fmt.Errorf("unspecified schedule action for schedule %d", sched.Id)

	default:
		return nil, fmt.Errorf("unsupported schedule action %v for schedule %d", sched.Action, sched.Id)
	}
}

// expandTargets converts a slice of ScheduleTarget into deduplicated device identifiers.
func (p *Processor) expandTargets(ctx context.Context, targets []*pb.ScheduleTarget, orgID int64) ([]string, error) {
	seen := make(map[string]struct{})
	var identifiers []string

	for _, t := range targets {
		switch t.TargetType {
		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_MINER:
			if _, dup := seen[t.TargetId]; !dup {
				seen[t.TargetId] = struct{}{}
				identifiers = append(identifiers, t.TargetId)
			}

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_RACK:
			rackID, err := strconv.ParseInt(t.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid rack target_id %q: %w", t.TargetId, err)
			}
			rackDevices, err := p.collectionStore.GetDeviceIdentifiersByDeviceSetID(ctx, rackID, orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve rack %d devices: %w", rackID, err)
			}
			for _, d := range rackDevices {
				if _, dup := seen[d]; !dup {
					seen[d] = struct{}{}
					identifiers = append(identifiers, d)
				}
			}

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_GROUP:
			groupID, err := strconv.ParseInt(t.TargetId, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid group target_id %q: %w", t.TargetId, err)
			}
			groupDevices, err := p.collectionStore.GetDeviceIdentifiersByDeviceSetID(ctx, groupID, orgID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve group %d devices: %w", groupID, err)
			}
			for _, d := range groupDevices {
				if _, dup := seen[d]; !dup {
					seen[d] = struct{}{}
					identifiers = append(identifiers, d)
				}
			}

		case pb.ScheduleTargetType_SCHEDULE_TARGET_TYPE_UNSPECIFIED:
			slog.Warn("unspecified target type", "target_id", t.TargetId)
		}
	}

	return identifiers, nil
}

func (p *Processor) resolveTargets(ctx context.Context, sched *pb.Schedule, orgID int64) ([]string, error) {
	targets, err := p.targetStore.GetScheduleTargets(ctx, orgID, sched.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedule targets: %w", err)
	}
	return p.expandTargets(ctx, targets, orgID)
}

func (p *Processor) updateAfterRun(ctx context.Context, sched *pb.Schedule, orgID int64, now time.Time) {
	lastRun := now.Unix()
	nextRun, err := ComputeNextRun(sched, now)
	if err != nil {
		slog.Error("failed to compute next run, keeping active", "schedule_id", sched.Id, "error", err)
		if uerr := p.procStore.UpdateScheduleAfterRun(ctx, sched.Id, &lastRun, nil, statusActive); uerr != nil {
			slog.Error("failed to update schedule after run", "schedule_id", sched.Id, "error", uerr)
		}
		return
	}

	var status string
	var nextRunPtr *int64

	hasPowerTargetWindow := sched.Action == pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET && sched.EndTime != ""

	if nextRun == nil && !hasPowerTargetWindow {
		status = statusCompleted
	} else if hasPowerTargetWindow {
		status = statusRunning
		if nextRun != nil {
			nru := nextRun.Unix()
			nextRunPtr = &nru
		}
	} else {
		status = statusActive
		nru := nextRun.Unix()
		nextRunPtr = &nru
	}

	if err := p.procStore.UpdateScheduleAfterRun(ctx, sched.Id, &lastRun, nextRunPtr, status); err != nil {
		slog.Error("failed to update schedule after run, reverting to active", "schedule_id", sched.Id, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, sched.Id); rerr != nil {
			slog.Error("failed to revert schedule after update failure", "schedule_id", sched.Id, "error", rerr)
			return
		}
		p.removeJob(sched.Id)
		return
	}

	if status == statusCompleted {
		p.removeJob(sched.Id)
		p.logCompleted(ctx, sched, orgID)
	}
}

func (p *Processor) transitionToCompleted(ctx context.Context, sched *pb.Schedule, orgID int64, now time.Time) {
	lastRun := now.Unix()
	if err := p.procStore.UpdateScheduleAfterRun(ctx, sched.Id, &lastRun, nil, statusCompleted); err != nil {
		slog.Error("failed to transition schedule to completed, reverting to active", "schedule_id", sched.Id, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, sched.Id); rerr != nil {
			slog.Error("failed to revert schedule after completion failure", "schedule_id", sched.Id, "error", rerr)
			return
		}
		p.removeJob(sched.Id)
		return
	}
	p.removeJob(sched.Id)
	p.logCompleted(ctx, sched, orgID)
	slog.Info("schedule completed (past end_date)", "schedule_id", sched.Id)
}

func (p *Processor) revertToActive(ctx context.Context, sched *pb.Schedule, now time.Time) error {
	nextRun, err := ComputeNextRun(sched, now)
	if err != nil {
		slog.Error("failed to compute next run during revert", "schedule_id", sched.Id, "error", err)
		if uerr := p.procStore.UpdateScheduleAfterRun(ctx, sched.Id, nil, nil, statusActive); uerr != nil {
			slog.Error("failed to revert schedule to active", "schedule_id", sched.Id, "error", uerr)
			return uerr
		}
		return nil
	}

	var nextRunPtr *int64
	status := statusActive
	if nextRun != nil {
		nru := nextRun.Unix()
		nextRunPtr = &nru
	} else {
		status = statusCompleted
	}
	if err := p.procStore.UpdateScheduleAfterRun(ctx, sched.Id, nil, nextRunPtr, status); err != nil {
		slog.Error("failed to revert schedule to active", "schedule_id", sched.Id, "error", err)
		return err
	}
	if status == statusCompleted {
		p.removeJob(sched.Id)
	}
	return nil
}

// checkEndOfWindow handles power-target schedules whose time window has expired.
func (p *Processor) checkEndOfWindow(ctx context.Context) {
	schedules, err := p.procStore.GetActiveSchedules(ctx)
	if err != nil {
		slog.Error("failed to load schedules for end-of-window check", "error", err)
		return
	}

	now := p.now()
	for _, sw := range schedules {
		sched := sw.Schedule

		if sched.Status != pb.ScheduleStatus_SCHEDULE_STATUS_RUNNING {
			continue
		}
		if sched.Action != pb.ScheduleAction_SCHEDULE_ACTION_SET_POWER_TARGET {
			continue
		}
		if sched.EndTime == "" {
			continue
		}

		loc, err := time.LoadLocation(sched.Timezone)
		if err != nil {
			slog.Error("invalid timezone on running schedule", "schedule_id", sched.Id, "timezone", sched.Timezone)
			continue
		}

		localNow := now.In(loc)

		if sched.LastRunAt == nil {
			continue
		}
		lastRunLocal := sched.LastRunAt.AsTime().In(loc)

		startTime, err := time.Parse("15:04", sched.StartTime)
		if err != nil {
			slog.Error("invalid start_time on running schedule", "schedule_id", sched.Id, "start_time", sched.StartTime)
			continue
		}

		endTime, err := time.Parse("15:04", sched.EndTime)
		if err != nil {
			slog.Error("invalid end_time on running schedule", "schedule_id", sched.Id, "end_time", sched.EndTime)
			continue
		}

		endBoundary := time.Date(lastRunLocal.Year(), lastRunLocal.Month(), lastRunLocal.Day(),
			endTime.Hour(), endTime.Minute(), 0, 0, loc)

		// Cross-midnight window (e.g., 22:00->06:00): end is on the following calendar day.
		endMinutes := endTime.Hour()*60 + endTime.Minute()
		startMinutes := startTime.Hour()*60 + startTime.Minute()
		if endMinutes <= startMinutes {
			endBoundary = endBoundary.AddDate(0, 0, 1)
		}

		if !localNow.After(endBoundary) {
			continue
		}

		slog.Info("power target window expired, reverting", "schedule_id", sched.Id)

		deviceIdentifiers, err := p.resolveTargets(ctx, sched, sw.OrgID)
		if err != nil {
			slog.Error("failed to resolve targets for revert", "schedule_id", sched.Id, "error", err)
			continue
		}

		// commandSvc applies the schedule-conflict filter for us; we consume
		// the skipped slice and log it the same way the normal dispatch path
		// does. This makes audit symmetric — pre-pre-work, the revert path
		// silently dropped overlapping miners.
		if len(deviceIdentifiers) > 0 {
			cmdCtx := schedulerContext(ctx, sched, sw.OrgID)
			selector := &commandpb.DeviceSelector{
				SelectionType: &commandpb.DeviceSelector_IncludeDevices{
					IncludeDevices: &commonpb.DeviceIdentifierList{
						DeviceIdentifiers: deviceIdentifiers,
					},
				},
			}
			result, err := p.commandSvc.SetPowerTarget(cmdCtx, selector, revertPerformanceMode)
			if err != nil {
				slog.Error("failed to dispatch revert command, will retry next cycle", "schedule_id", sched.Id, "error", err)
				continue
			}
			if skipped := countConflictSkips(result); skipped > 0 {
				p.logConflictSkip(ctx, sched, sw.OrgID, skipped)
			}
		}

		if err := p.revertToActive(ctx, sched, now); err != nil {
			continue
		}
		p.logRevert(ctx, sched, sw.OrgID)
	}
}

func (p *Processor) removeJob(scheduleID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeJobLocked(scheduleID)
}

// removeJobLocked removes a job while p.mu is already held.
func (p *Processor) removeJobLocked(scheduleID int64) {
	if entry, ok := p.jobs[scheduleID]; ok {
		if entry.isOneTime {
			if entry.timer != nil {
				entry.timer.Stop()
			}
		} else {
			p.cron.Remove(entry.entryID)
		}
		delete(p.jobs, scheduleID)
	}
}

// schedulerContext creates a context with synthetic session info so the command
// service can create batch logs and resolve devices using the schedule's org.
//
// Source carries this schedule's id and priority so the command-service-level
// schedule-conflict filter can apply scheduler-priority semantics (only
// strictly higher-priority running schedules block) instead of the manual
// fallback (every running schedule blocks). Without Source the filter would
// treat every scheduler-origin call as if it were a manual call and block
// against itself.
func schedulerContext(parent context.Context, sched *pb.Schedule, orgID int64) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      schedulerActorName,
		UserID:         sched.CreatedBy,
		OrganizationID: orgID,
		ExternalUserID: schedulerActorName,
		Username:       schedulerActorName,
		// Mark the session as scheduler-driven so downstream activity logging
		// tags both the initiated and completed rows with ActorScheduler.
		Actor: session.ActorScheduler,
		Source: session.Source{
			ScheduleID:       sched.Id,
			SchedulePriority: sched.Priority,
		},
	})
}

func (p *Processor) logExecution(ctx context.Context, sched *pb.Schedule, orgID int64, deviceCount int) {
	if p.activitySvc == nil {
		return
	}
	actor := schedulerActorName
	p.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySchedule,
		Type:           "schedule_executed",
		Description:    fmt.Sprintf("Schedule %q executed (%v) on %d devices", sched.Name, sched.Action, deviceCount),
		ActorType:      activitymodels.ActorScheduler,
		UserID:         &actor,
		Username:       &actor,
		OrganizationID: &orgID,
	})
}

func (p *Processor) logRevert(ctx context.Context, sched *pb.Schedule, orgID int64) {
	if p.activitySvc == nil {
		return
	}
	actor := schedulerActorName
	p.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySchedule,
		Type:           "schedule_window_ended",
		Description:    fmt.Sprintf("Schedule %q power target window ended, reverted to default", sched.Name),
		ActorType:      activitymodels.ActorScheduler,
		UserID:         &actor,
		Username:       &actor,
		OrganizationID: &orgID,
	})
}

func (p *Processor) logCompleted(ctx context.Context, sched *pb.Schedule, orgID int64) {
	if p.activitySvc == nil {
		return
	}
	actor := schedulerActorName
	p.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySchedule,
		Type:           "schedule_completed",
		Description:    fmt.Sprintf("Schedule %q completed (no future runs remain)", sched.Name),
		ActorType:      activitymodels.ActorScheduler,
		UserID:         &actor,
		Username:       &actor,
		OrganizationID: &orgID,
	})
}

func (p *Processor) logConflictSkip(ctx context.Context, sched *pb.Schedule, orgID int64, skipped int) {
	if p.activitySvc == nil {
		return
	}
	actor := schedulerActorName
	p.activitySvc.Log(ctx, activitymodels.Event{
		Category:       activitymodels.CategorySchedule,
		Type:           "schedule_conflict_skip",
		Description:    fmt.Sprintf("Schedule %q skipped %d miners overridden by higher-priority schedule", sched.Name, skipped),
		ActorType:      activitymodels.ActorScheduler,
		UserID:         &actor,
		Username:       &actor,
		OrganizationID: &orgID,
	})
}
