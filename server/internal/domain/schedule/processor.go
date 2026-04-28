package schedule

import (
	"context"
	"fmt"
	"log/slog"
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
	scheduletargets "github.com/block/proto-fleet/server/internal/domain/schedule/targets"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	reconcileInterval     = 60 * time.Second
	endOfWindowInterval   = 30 * time.Second
	revertPerformanceMode = commandpb.PerformanceMode_PERFORMANCE_MODE_EFFICIENCY
	schedulerActorName    = "scheduler"
	oneTimeRetryDelay     = time.Second
	// shutdownDeadline bounds Stop(): drain-before-cancel is the graceful
	// path, the watchdog cancels workCtx if a DB/dispatch call wedges.
	// Sized within the fleetd-wide shutdownTimeout (10s).
	shutdownDeadline = 10 * time.Second
)

// shutdownDeadlineFn lets tests shrink the watchdog without affecting prod.
var shutdownDeadlineFn = func() time.Duration { return shutdownDeadline }

// CommandDispatcher is the subset of command.Service the processor needs.
// CommandResult carries preflight skips for schedule-level audit.
type CommandDispatcher interface {
	SetPowerTarget(ctx context.Context, selector *commandpb.DeviceSelector, mode commandpb.PerformanceMode) (*command.CommandResult, error)
	Reboot(ctx context.Context, selector *commandpb.DeviceSelector) (*command.CommandResult, error)
	StopMining(ctx context.Context, selector *commandpb.DeviceSelector) (*command.CommandResult, error)
}

// jobEntry tracks a registered cron/timer job and its timing fingerprint.
type jobEntry struct {
	entryID     cron.EntryID
	timer       *time.Timer
	isOneTime   bool
	fingerprint string
	generation  uint64
}

type Processor struct {
	cron            *cron.Cron
	procStore       interfaces.ScheduleProcessorStore
	targetStore     interfaces.ScheduleTargetStore
	collectionStore interfaces.CollectionStore
	commandSvc      CommandDispatcher
	activitySvc     *activity.Service
	now             func() time.Time

	stopCancel context.CancelFunc
	workCancel context.CancelFunc
	wg         sync.WaitGroup
	timerWG    sync.WaitGroup
	mu         sync.Mutex
	jobs       map[int64]jobEntry
	nextGen    uint64
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
	stopCtx, stopCancel := context.WithCancel(context.Background())
	workCtx, workCancel := context.WithCancel(context.Background())
	p.stopCancel = stopCancel
	p.workCancel = workCancel

	p.cron = cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))

	if err := p.recoverStaleRunning(workCtx); err != nil {
		stopCancel()
		workCancel()
		return fmt.Errorf("schedule processor startup: %w", err)
	}
	if err := p.syncSchedules(workCtx); err != nil {
		stopCancel()
		workCancel()
		return fmt.Errorf("schedule processor startup: %w", err)
	}

	p.cron.Start()

	p.wg.Add(2)
	go p.reconcileLoop(stopCtx, workCtx)
	go p.endOfWindowLoop(stopCtx, workCtx)

	slog.Info("schedule processor started")
	return nil
}

func (p *Processor) Stop() error {
	// Watchdog bounds total shutdown: workCancel is idempotent, so an early
	// fire just unblocks wedged in-flight calls; the drain sequence below
	// still runs to completion.
	if p.workCancel != nil {
		watchdog := time.AfterFunc(shutdownDeadlineFn(), p.workCancel)
		defer watchdog.Stop()
	}

	if p.stopCancel != nil {
		p.stopCancel()
	}
	p.wg.Wait()

	if p.cron != nil {
		cronCtx := p.cron.Stop()
		<-cronCtx.Done()
	}

	p.mu.Lock()
	for _, entry := range p.jobs {
		if entry.isOneTime && entry.timer != nil {
			if entry.timer.Stop() {
				p.timerWG.Done()
			}
		}
	}
	p.mu.Unlock()

	p.timerWG.Wait()

	if p.workCancel != nil {
		p.workCancel()
	}
	slog.Info("schedule processor stopped")
	return nil
}

func (p *Processor) reconcileLoop(stopCtx, workCtx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCtx.Done():
			return
		case <-ticker.C:
			if err := p.syncSchedules(workCtx); err != nil {
				slog.Error("reconciliation failed, will retry next cycle", "error", err)
			}
		}
	}
}

func (p *Processor) endOfWindowLoop(stopCtx, workCtx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(endOfWindowInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCtx.Done():
			return
		case <-ticker.C:
			p.checkEndOfWindow(workCtx)
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
	generation := p.nextJobGenerationLocked()

	if sched.ScheduleType == pb.ScheduleType_SCHEDULE_TYPE_ONE_TIME {
		t, err := ParseScheduleTime(sched.StartDate, sched.StartTime, sched.Timezone)
		if err != nil {
			return fmt.Errorf("failed to parse one-time schedule time: %w", err)
		}
		delay := time.Until(t)
		if delay < oneTimeRetryDelay {
			delay = oneTimeRetryDelay
		}
		p.timerWG.Add(1)
		timer := time.AfterFunc(delay, func() {
			defer p.timerWG.Done()
			p.executeSchedule(ctx, scheduleID)
		})
		p.jobs[scheduleID] = jobEntry{timer: timer, isOneTime: true, fingerprint: scheduleFingerprint(sched), generation: generation}
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

	p.jobs[scheduleID] = jobEntry{entryID: entryID, fingerprint: scheduleFingerprint(sched), generation: generation}
	return nil
}

func (p *Processor) nextJobGenerationLocked() uint64 {
	p.nextGen++
	return p.nextGen
}

// executeSchedule is called when a job fires.
func (p *Processor) executeSchedule(ctx context.Context, scheduleID int64) {
	gen, hasGen := p.currentJobGeneration(scheduleID)
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
		p.removeJobForRetry(scheduleID, gen, hasGen)
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
			p.transitionToCompletedWithGeneration(ctx, sched, orgID, now, gen, hasGen)
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
		p.removeJobForRetry(scheduleID, gen, hasGen)
		return
	}

	if len(deviceIdentifiers) == 0 {
		slog.Info("no target devices resolved, skipping dispatch", "schedule_id", scheduleID)
		p.updateAfterRunWithGeneration(ctx, sched, orgID, now, gen, hasGen)
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

	// commandSvc owns preflight filtering; the processor only records
	// schedule-level skip activity from the returned metadata.
	result, err := p.dispatch(cmdCtx, sched, selector)
	if err != nil {
		slog.Error("failed to dispatch command", "schedule_id", scheduleID, "action", sched.Action, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, scheduleID); rerr != nil {
			slog.Error("failed to revert schedule after dispatch failure", "schedule_id", scheduleID, "error", rerr)
			return
		}
		// Remove the job so syncSchedules re-registers it. This is necessary
		// for one-time schedules whose timer has already fired and won't retrigger.
		p.removeJobForRetry(scheduleID, gen, hasGen)
		return
	}

	conflictSkips := countConflictSkips(result)
	if conflictSkips > 0 {
		p.logConflictSkip(ctx, sched, orgID, conflictSkips)
	}

	dispatched := 0
	if result != nil {
		dispatched = result.DispatchedCount
		if dispatched == 0 && len(result.Skipped) > 0 {
			slog.Info("all miners overridden by preflight filters", "schedule_id", scheduleID)
		}
	}

	p.updateAfterRunWithGeneration(ctx, sched, orgID, now, gen, hasGen)
	// Fully filtered dispatches are already captured by schedule_conflict_skip.
	if dispatched > 0 || conflictSkips == 0 {
		p.logExecution(ctx, sched, orgID, dispatched)
	}
}

// countConflictSkips ignores other command preflight filters; schedule activity
// should only summarize schedule-priority conflicts.
func countConflictSkips(result *command.CommandResult) int {
	if result == nil {
		return 0
	}
	n := 0
	for _, s := range result.Skipped {
		if s.FilterName == command.ScheduleConflictFilterName {
			n++
		}
	}
	return n
}

func (p *Processor) currentJobGeneration(scheduleID int64) (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.jobs[scheduleID]
	if !ok {
		return 0, false
	}
	return entry.generation, true
}

func (p *Processor) removeJobIfCurrent(scheduleID int64, gen uint64, ok bool) {
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, exists := p.jobs[scheduleID]; exists && entry.generation == gen {
		p.removeJobLocked(scheduleID)
	}
}

func (p *Processor) removeJobForRetry(scheduleID int64, gen uint64, hasGen bool) {
	p.removeJobIfCurrent(scheduleID, gen, hasGen)
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
	return scheduletargets.Expand(ctx, p.collectionStore, targets, orgID, func(targetID string) {
		slog.Warn("unspecified target type", "target_id", targetID)
	})
}

func (p *Processor) resolveTargets(ctx context.Context, sched *pb.Schedule, orgID int64) ([]string, error) {
	targets, err := p.targetStore.GetScheduleTargets(ctx, orgID, sched.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedule targets: %w", err)
	}
	return p.expandTargets(ctx, targets, orgID)
}

func (p *Processor) updateAfterRun(ctx context.Context, sched *pb.Schedule, orgID int64, now time.Time) {
	gen, hasGen := p.currentJobGeneration(sched.Id)
	p.updateAfterRunWithGeneration(ctx, sched, orgID, now, gen, hasGen)
}

func (p *Processor) updateAfterRunWithGeneration(ctx context.Context, sched *pb.Schedule, orgID int64, now time.Time, gen uint64, hasGen bool) {
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
		p.removeJobForRetry(sched.Id, gen, hasGen)
		return
	}

	if status == statusCompleted {
		p.removeJob(sched.Id)
		p.logCompleted(ctx, sched, orgID)
	}
}

func (p *Processor) transitionToCompleted(ctx context.Context, sched *pb.Schedule, orgID int64, now time.Time) {
	gen, hasGen := p.currentJobGeneration(sched.Id)
	p.transitionToCompletedWithGeneration(ctx, sched, orgID, now, gen, hasGen)
}

func (p *Processor) transitionToCompletedWithGeneration(ctx context.Context, sched *pb.Schedule, orgID int64, now time.Time, gen uint64, hasGen bool) {
	lastRun := now.Unix()
	if err := p.procStore.UpdateScheduleAfterRun(ctx, sched.Id, &lastRun, nil, statusCompleted); err != nil {
		slog.Error("failed to transition schedule to completed, reverting to active", "schedule_id", sched.Id, "error", err)
		if rerr := p.procStore.RevertScheduleToActive(ctx, sched.Id); rerr != nil {
			slog.Error("failed to revert schedule after completion failure", "schedule_id", sched.Id, "error", rerr)
			return
		}
		p.removeJobForRetry(sched.Id, gen, hasGen)
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

		// commandSvc applies conflict filtering; mirror normal dispatch audit.
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
			if entry.timer != nil && entry.timer.Stop() {
				p.timerWG.Done()
			}
		} else {
			p.cron.Remove(entry.entryID)
		}
		delete(p.jobs, scheduleID)
	}
}

// schedulerContext adds synthetic scheduler session info for command dispatch.
// Source lets ScheduleConflictFilter apply priority semantics.
func schedulerContext(parent context.Context, sched *pb.Schedule, orgID int64) context.Context {
	return authn.SetInfo(parent, &session.Info{
		SessionID:      schedulerActorName,
		UserID:         sched.CreatedBy,
		OrganizationID: orgID,
		ExternalUserID: schedulerActorName,
		Username:       schedulerActorName,
		Actor:          session.ActorScheduler,
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
