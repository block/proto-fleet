package frigateupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/runtimejobs"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	// maxConcurrentJobs bounds jobs that start, poll or reconcile a rollout.
	maxConcurrentJobs = 8
	// maxConcurrentStaging bounds jobs that copy a package to their controllers.
	// Staging holds its worker for the whole transfer, so it has its own pool
	// and can never delay status polls, starts or cancels of other jobs.
	maxConcurrentStaging = 4
	// rolloutLostPolls is how many consecutive "unknown rollout" answers prove
	// that Frigate no longer has a running job's state.
	rolloutLostPolls = 3
	cleanupGiveUp    = 24 * time.Hour
	// unreachableGiveUp is how long an unfinished job may go without any
	// controller answering before Fleet abandons it. A blocking job keeps the
	// container from being removed, so a container whose controllers are gone
	// for good must not hold it forever.
	unreachableGiveUp = 24 * time.Hour
	// cleanupPassBudget bounds one cleanup sweep so a container that never
	// answers cannot keep it busy for the sum of every abort timeout.
	cleanupPassBudget = 2 * time.Minute
	// startLeaseMargin extends the start lease past the Start call timeout.
	startLeaseMargin = 5 * time.Second
	nextStepRestart  = "Resolve the cause, then start the update again."
)

// errStopStaging ends one instance's upload because the job left staging or
// failed. It is not itself a failure.
var errStopStaging = errors.New("staging stopped")

// Run advances update jobs until ctx ends. It must run on the active Fleet
// replica only. All progress lives in the database, so a replica that takes
// over resumes every job where the previous one stopped.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	s.run(ctx, ticker.C)
}

func (s *Service) run(ctx context.Context, ticks <-chan time.Time) {
	var pending sync.WaitGroup
	defer pending.Wait()
	report := runtimejobs.TrackProgress(ctx, s.cfg.PollInterval)
	slots := make(chan struct{}, maxConcurrentJobs)
	stagingSlots := make(chan struct{}, maxConcurrentStaging)
	// Cleanup talks to controllers that may be unreachable, so it runs on its
	// own loop and never delays polling or starting jobs.
	pending.Add(1)
	go func() {
		defer pending.Done()
		s.runCleanup(ctx)
	}()
	pass := func() {
		jobs, err := s.queries.ListRunnableFrigateUpdateJobs(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "list runnable frigate update jobs", "error", err)
			}
			return
		}
		for _, job := range s.fairOrder(jobs) {
			if ctx.Err() != nil {
				return
			}
			if !s.claim(job.ID) {
				continue
			}
			pool := slots
			if job.State == StateStaging {
				pool = stagingSlots
			}
			select {
			case pool <- struct{}{}:
			default:
				s.release(job.ID)
				continue
			}
			s.noteDispatched(job.ID)
			pending.Add(1)
			go func() {
				defer pending.Done()
				defer func() { <-pool }()
				defer s.release(job.ID)
				s.processJob(ctx, job)
			}()
		}
		report()
	}
	pass()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			pass()
		}
	}
}

// runCleanup sweeps staged copies of ended jobs until ctx ends.
func (s *Service) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	for {
		s.sweepCleanup(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runOnce advances every runnable job once and waits for the work to finish.
func (s *Service) runOnce(ctx context.Context) {
	s.sweepCleanup(ctx)
	jobs, err := s.queries.ListRunnableFrigateUpdateJobs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list runnable frigate update jobs", "error", err)
		return
	}
	for _, job := range jobs {
		s.processJob(ctx, job)
	}
}

// fairOrder returns the runnable jobs least recently dispatched first, so jobs
// that are always runnable (a retryable failure stays listed until an operator
// acts) cannot take every worker slot on every pass and starve the jobs behind
// them. Jobs never dispatched since this process started come first, then ties
// keep the database's created_at order. It also forgets jobs that are no longer
// runnable, which bounds the map. The order is in memory only: the runner runs
// on the active replica, and a restart merely resets it to created_at order.
func (s *Service) fairOrder(jobs []sqlc.FrigateUpdateJob) []sqlc.FrigateUpdateJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	runnable := make(map[uuid.UUID]struct{}, len(jobs))
	for _, j := range jobs {
		runnable[j.ID] = struct{}{}
	}
	for id := range s.dispatched {
		if _, ok := runnable[id]; !ok {
			delete(s.dispatched, id)
		}
	}
	ordered := append([]sqlc.FrigateUpdateJob(nil), jobs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return s.dispatched[ordered[i].ID] < s.dispatched[ordered[j].ID]
	})
	return ordered
}

// noteDispatched records that a job was just handed to a worker slot. A
// sequence number rather than a timestamp orders dispatches, so jobs handed out
// within one clock tick still rotate.
func (s *Service) noteDispatched(id uuid.UUID) {
	s.mu.Lock()
	s.dispatchSeq++
	s.dispatched[id] = s.dispatchSeq
	s.mu.Unlock()
}

func (s *Service) claim(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.inflight[id]; busy {
		return false
	}
	s.inflight[id] = struct{}{}
	return true
}

func (s *Service) release(id uuid.UUID) {
	s.mu.Lock()
	delete(s.inflight, id)
	s.mu.Unlock()
}

func (s *Service) processJob(ctx context.Context, job sqlc.FrigateUpdateJob) {
	switch job.State {
	case StateStaging:
		s.stageJob(ctx, job)
	case StateStaged, StateStarting:
		s.startJob(ctx, job)
	case StateRunning, StateFailed:
		s.pollJob(ctx, job)
	}
}

// mutate applies fn to the locked job. fn returns false to leave it unchanged.
func (s *Service) mutate(ctx context.Context, id uuid.UUID, fn func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool) (before, after sqlc.FrigateUpdateJob, changed bool, err error) {
	type result struct {
		before, after sqlc.FrigateUpdateJob
		changed       bool
	}
	res, err := runTx(ctx, s, func(q sqlc.Querier) (result, error) {
		cur, err := q.LockFrigateUpdateJob(ctx, id)
		if err != nil {
			return result{}, err
		}
		p := paramsFromJob(cur)
		if !fn(cur, &p) {
			return result{before: cur, after: cur}, nil
		}
		if err := q.UpdateFrigateUpdateJob(ctx, p); err != nil {
			return result{}, err
		}
		after, err := q.LockFrigateUpdateJob(ctx, id)
		return result{before: cur, after: after, changed: true}, err
	})
	return res.before, res.after, res.changed, err
}

// failJob ends a job that never reached a running rollout.
func (s *Service) failJob(ctx context.Context, id uuid.UUID, code, message string, cleanup bool) {
	s.failJobUnless(ctx, id, code, message, cleanup, nil)
}

// failJobUnless is failJob with a veto checked against the locked job.
func (s *Service) failJobUnless(ctx context.Context, id uuid.UUID, code, message string, cleanup bool, veto func(cur sqlc.FrigateUpdateJob) bool) {
	before, after, changed, err := s.mutate(ctx, id, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		switch cur.State {
		case StateStaging, StateStaged, StateStarting:
		default:
			return false
		}
		if veto != nil && veto(cur) {
			return false
		}
		p.State = StateFailed
		p.ErrorCode = truncate(code, 128)
		p.ErrorMessage = truncate(message, maxStoredMessage)
		p.NextStep = nextStepRestart
		p.Retryable = false
		p.Cancellable = false
		p.CleanupPending = cleanup
		p.CompletedAt = nullTime(s.now())
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "fail frigate update job", "job_id", id, "error", err)
		}
		return
	}
	if changed {
		s.forget(id)
		s.logTransition(ctx, before, after)
	}
}

// markUnreachable records that no controller answered. A job that has had no
// answer for unreachableGiveUp is abandoned, so it stops blocking the container.
func (s *Service) markUnreachable(ctx context.Context, id uuid.UUID, cause error) {
	slog.WarnContext(ctx, "frigate update status unavailable", "job_id", id, "error", cause)
	before, after, changed, err := s.mutate(ctx, id, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if isTerminal(cur) {
			return false
		}
		now := s.now()
		p.LastPolledAt = nullTime(now)
		if !cur.UnreachableSince.Valid {
			p.UnreachableSince = nullTime(now)
			return true
		}
		if now.Sub(cur.UnreachableSince.Time) >= unreachableGiveUp {
			p.State = StateAbandoned
			p.ErrorCode = CodeControllersLost
			p.ErrorMessage = fmt.Sprintf("No controller has answered for %s, so Fleet stopped tracking this update. "+
				"The controllers may still be partway through installing.", unreachableGiveUp)
			p.NextStep = "Check the controllers on site. Once they answer, start a new update if they are not on the target version."
			p.Retryable = false
			p.Cancellable = false
			p.CleanupPending = true
			p.CompletedAt = nullTime(now)
		}
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "record frigate update unreachable", "job_id", id, "error", err)
		}
		return
	}
	if changed && after.State == StateAbandoned {
		slog.WarnContext(ctx, "abandoned frigate update with unreachable controllers", "job_id", id, "unreachable_since", before.UnreachableSince.Time)
		s.forget(id)
		s.logTransition(ctx, before, after)
	}
}

// stallClock measures how long staging has gone without confirmed progress.
type stallClock struct {
	since time.Time
	// excused is the copy time since the last confirmed progress that Fleet
	// could not check and did not count.
	excused time.Duration
}

func (s *Service) noteProgress(id uuid.UUID) {
	s.mu.Lock()
	s.progress[id] = stallClock{since: s.now()}
	s.mu.Unlock()
}

// stalled reports whether staging made no progress for the stall timeout. The
// first observation starts the clock.
func (s *Service) stalled(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	clock, ok := s.progress[id]
	if !ok {
		s.progress[id] = stallClock{since: s.now()}
		return false
	}
	return s.now().Sub(clock.since) > s.cfg.StageStallTimeout
}

// startProgressClock starts the stall clock for a job that has none yet.
func (s *Service) startProgressClock(id uuid.UUID) {
	s.mu.Lock()
	if _, ok := s.progress[id]; !ok {
		s.progress[id] = stallClock{since: s.now()}
	}
	s.mu.Unlock()
}

// excuseUnknownAttempt does not count the time of a copy attempt whose result
// Fleet could not read toward the stall timeout. It excuses at most
// StageStallTimeout between two confirmed progress points, so a job with no
// confirmed progress stalls at the first check after twice the stall timeout.
func (s *Service) excuseUnknownAttempt(id uuid.UUID, start, end time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clock, ok := s.progress[id]
	if !ok {
		return
	}
	d := min(end.Sub(maxTime(start, clock.since)), s.cfg.StageStallTimeout-clock.excused)
	if d <= 0 {
		return
	}
	clock.since = clock.since.Add(d)
	clock.excused += d
	s.progress[id] = clock
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s *Service) forget(id uuid.UUID) {
	s.mu.Lock()
	delete(s.progress, id)
	delete(s.lost, id)
	s.mu.Unlock()
}

// manifestFor builds the per-instance self-coordinated manifest. It is a pure
// function of the job and package, so every attempt yields the same Frigate
// fingerprint and an interrupted upload resumes.
func manifestFor(job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, instanceID string) sdk.FrigateManifest {
	expected := append([]string(nil), job.ExpectedInstanceIds...)
	sort.Strings(expected)
	return sdk.FrigateManifest{
		JobID:               job.ID.String(),
		InstanceID:          instanceID,
		ExpectedInstanceIDs: expected,
		TargetVersion:       pkg.Version,
		SHA256:              pkg.Sha256,
		SizeBytes:           pkg.SizeBytes,
		Actor:               actorName(job.ActorUsername, job.ActorExternalID, job.ActorUserID),
		Coordination:        sdk.FrigateCoordinationSelf,
		ReleaseStatement:    pkg.Statement,
		ReleaseSignature:    pkg.Signature,
	}
}

func (s *Service) stageJob(ctx context.Context, job sqlc.FrigateUpdateJob) {
	pkg, err := s.queries.GetFrigateUpdatePackage(ctx, sqlc.GetFrigateUpdatePackageParams{OrgID: job.OrgID, ID: job.PackageID})
	if err != nil {
		slog.ErrorContext(ctx, "load frigate package for staging", "job_id", job.ID, "error", err)
		return
	}
	// A blob that is missing here (Fleet failed over to a replica that never
	// received the upload) fails the job before anything reaches a controller.
	file, size, err := s.store.Open(pkg.ID)
	if err != nil || size != pkg.SizeBytes {
		if file != nil {
			file.Close()
		}
		s.failJob(ctx, job.ID, CodePackageMissing,
			"The package is not available on this Fleet host. Upload it again and restart the update.", true)
		return
	}
	defer file.Close()

	updater, err := s.updaterFor(ctx, job.DeviceID)
	if err != nil {
		s.stageTransient(ctx, job.ID, err)
		return
	}
	instances, err := s.queries.ListFrigateUpdateInstances(ctx, job.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list frigate update instances", "job_id", job.ID, "error", err)
		return
	}

	var transient error
	if stager, ok := updater.(interfaces.FrigatePackageStager); ok {
		transient = s.stageThroughRelay(ctx, updater, stager, job, pkg, instances)
	} else {
		transient = s.stageDirect(ctx, updater, job, pkg, file, instances)
	}
	if ctx.Err() != nil {
		return
	}

	fresh, err := s.queries.GetFrigateUpdateJob(ctx, sqlc.GetFrigateUpdateJobParams{OrgID: job.OrgID, ID: job.ID})
	if err != nil {
		return
	}
	if fresh.State != StateStaging {
		// Cancelled or failed while uploading. A sibling upload may have noted
		// progress after the transition cleared it, so clear it again.
		s.forget(job.ID)
		return
	}
	rows, err := s.queries.ListFrigateUpdateInstances(ctx, job.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list frigate update instances", "job_id", job.ID, "error", err)
		return
	}
	for _, r := range rows {
		if r.StageState != StageStaged {
			if transient != nil {
				s.stageTransient(ctx, job.ID, transient)
			}
			return
		}
	}
	_, after, changed, err := s.mutate(ctx, job.ID, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if cur.State != StateStaging {
			return false
		}
		p.State = StateStaged
		p.Cancellable = true
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "mark frigate update staged", "job_id", job.ID, "error", err)
		}
		return
	}
	if changed {
		s.forget(job.ID)
		s.startJob(ctx, after)
	}
}

// stageTransient handles a retryable staging failure: the next pass resumes,
// unless staging has made no progress for too long.
func (s *Service) stageTransient(ctx context.Context, id uuid.UUID, cause error) {
	if ctx.Err() != nil {
		return
	}
	slog.WarnContext(ctx, "frigate package staging will retry", "job_id", id, "error", cause)
	if s.stalled(id) {
		s.failJob(ctx, id, CodeStageStalled,
			fmt.Sprintf("Staging made no progress for %s: %v", s.cfg.StageStallTimeout, cause), true)
	}
}

// persistStaging records one instance's staging position. It returns false when
// the job has left staging, which tells the uploader to stop.
func (s *Service) persistStaging(ctx context.Context, jobID uuid.UUID, instanceID, state, fingerprint string, bytes int64, code, message string) (bool, error) {
	rows, err := s.queries.UpdateFrigateUpdateInstanceStaging(ctx, sqlc.UpdateFrigateUpdateInstanceStagingParams{
		JobID: jobID, InstanceID: instanceID, StageState: state, ManifestSha256: fingerprint, StagedBytes: bytes,
		ErrorCode: truncate(code, 128), ErrorMessage: truncate(message, maxStoredMessage),
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// stageDirect uploads the package to every unstaged controller at the same
// time. It returns the last retryable failure, if any.
func (s *Service) stageDirect(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, file *os.File, instances []sqlc.FrigateUpdateInstance) error {
	stageCtx, stop := context.WithCancel(ctx)
	defer stop()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var transient error
	for _, inst := range instances {
		if inst.StageState == StageStaged {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.stageInstance(stageCtx, updater, job, pkg, file, inst)
			switch {
			case err == nil:
			case errors.Is(err, errStopStaging):
				stop()
			default:
				mu.Lock()
				transient = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return transient
}

// stageThroughRelay stages a job whose controllers Fleet reaches through a
// relay, such as a Fleet Node. Fleet begins each copy itself, so that it
// records every fingerprint before any bytes move. One call then has the relay
// fetch the package and copy it to all the controllers. It returns the last
// retryable failure, if any.
func (s *Service) stageThroughRelay(ctx context.Context, updater interfaces.FrigateUpdater, stager interfaces.FrigatePackageStager, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, instances []sqlc.FrigateUpdateInstance) error {
	var transient error
	var copies []stagingCopy
	targets := make([]interfaces.FrigateStageTarget, 0, len(instances))
	for _, inst := range instances {
		if inst.StageState == StageStaged {
			continue
		}
		c, err := s.beginInstance(ctx, updater, job, pkg, inst)
		switch {
		case err == nil:
			// The relay reports no position while it copies. An offset beyond
			// the recorded one shows that the last attempt moved bytes.
			if c.next > inst.StagedBytes {
				s.noteProgress(job.ID)
			}
			copies = append(copies, c)
			targets = append(targets, interfaces.FrigateStageTarget{InstanceID: inst.InstanceID, Manifest: c.manifest, ManifestSHA256: c.fingerprint})
		case errors.Is(err, errStopStaging):
			return nil
		default:
			transient = err
		}
	}
	if len(copies) == 0 {
		return transient
	}

	sent := s.now()
	outcomes, err := stager.FrigateStagePackage(ctx, interfaces.FrigatePackageRef{
		ID: pkg.ID.String(), Filename: pkg.Filename, SizeBytes: pkg.SizeBytes, SHA256: pkg.Sha256,
	}, targets)
	returned := s.now()
	if s.requeueCleanupIfStopped(ctx, job.ID) {
		return nil
	}
	defer s.requeueCleanupIfStopped(ctx, job.ID)
	if err == nil && len(outcomes) != len(copies) {
		err = fmt.Errorf("the relay returned %d stage outcomes for %d controllers", len(outcomes), len(copies))
	}
	unknown := false
	for i, c := range copies {
		outcomeErr := err
		if err == nil {
			outcomeErr = outcomes[i].Err
		}
		if outcomeErr == nil {
			outcomeErr = s.finishInstance(ctx, job, pkg, c, outcomes[i].Commit)
		} else {
			next, read := s.relayStagedOffset(ctx, updater, job, pkg, c, outcomeErr)
			unknown = unknown || !read
			outcomeErr = s.stageError(ctx, job, c.inst, c.fingerprint, next, outcomeErr)
		}
		switch {
		case outcomeErr == nil:
		case errors.Is(outcomeErr, errStopStaging):
			return nil
		default:
			transient = outcomeErr
		}
	}
	if unknown {
		s.excuseUnknownAttempt(job.ID, sent, returned)
	}
	return transient
}

// requeueCleanupIfStopped reports whether the job left staging. The relay can
// begin and fill a copy after cleanup removed the copies of such a job, so it
// queues cleanup again.
func (s *Service) requeueCleanupIfStopped(ctx context.Context, id uuid.UUID) bool {
	mutateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.CallTimeout)
	defer cancel()
	_, after, _, err := s.mutate(mutateCtx, id, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if cur.State == StateStaging {
			return false
		}
		p.CleanupPending = true
		return true
	})
	if err != nil {
		slog.ErrorContext(ctx, "queue frigate update cleanup after a relay copy", "job_id", id, "error", err)
		return true
	}
	return after.State != StateStaging
}

// relayStagedOffset returns how far a copy got that failed through a relay,
// and false when the controller did not answer. The relay reports no position,
// and one attempt can last longer than the stall timeout. Fleet therefore asks
// the controller, so that bytes this attempt moved count as progress before
// the stall check.
func (s *Service) relayStagedOffset(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, c stagingCopy, cause error) (int64, bool) {
	// A refusal is final, and a stopped pass makes no more calls.
	if ctx.Err() != nil || sdk.AsFrigateRefusal(cause) != nil {
		return c.next, true
	}
	beginCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	progress, err := updater.FrigateStageBegin(beginCtx, c.inst.InstanceID, c.manifest)
	cancel()
	if err != nil {
		return c.next, false
	}
	if progress.ManifestSHA256 != c.fingerprint || progress.NextOffset <= c.next || progress.NextOffset > pkg.SizeBytes {
		return c.next, true
	}
	s.noteProgress(job.ID)
	return progress.NextOffset, true
}

// stagingCopy is one controller's copy after Begin.
type stagingCopy struct {
	inst        sqlc.FrigateUpdateInstance
	manifest    sdk.FrigateManifest
	fingerprint string
	// next is the offset that the controller acknowledged.
	next int64
}

// beginInstance begins or resumes one controller's copy, and records its
// fingerprint and acknowledged offset.
func (s *Service) beginInstance(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, inst sqlc.FrigateUpdateInstance) (stagingCopy, error) {
	manifest := manifestFor(job, pkg, inst.InstanceID)
	actor := manifest.Actor
	none := stagingCopy{}

	next := inst.StagedBytes
	if inst.StageState == StagePending {
		// Mark the instance before its first Begin, so a copy whose Begin reply
		// is lost still reaches cleanup, which can recover its fingerprint.
		if ok, err := s.persistStaging(ctx, job.ID, inst.InstanceID, StageStaging, "", 0, "", ""); err != nil {
			return none, err
		} else if !ok {
			return none, errStopStaging
		}
	}
	beginCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	progress, err := updater.FrigateStageBegin(beginCtx, inst.InstanceID, manifest)
	cancel()
	if err != nil {
		// Keep the saved fingerprint: a resumed copy may already hold bytes,
		// and cleanup can abort it only by that fingerprint.
		return none, s.stageError(ctx, job, inst, inst.ManifestSha256, next, err)
	}
	fingerprint := progress.ManifestSHA256
	next = progress.NextOffset
	if next < 0 || next > pkg.SizeBytes {
		return none, s.stageError(ctx, job, inst, fingerprint, next, fmt.Errorf("controller reported staged offset %d outside the package", next))
	}
	// Record the fingerprint even if a sibling instance just cancelled the
	// staging context, so the sweep can abort the copy by fingerprint.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.CallTimeout)
	ok, err := s.persistStaging(persistCtx, job.ID, inst.InstanceID, StageStaging, fingerprint, next, "", "")
	persistCancel()
	if err != nil {
		return none, err
	} else if !ok {
		// The job left staging while Begin was in flight, so the sweep cannot
		// find this fingerprint. Abort the fresh copy directly.
		abortCtx, abortCancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.CallTimeout)
		if err := updater.FrigateStageAbort(abortCtx, inst.InstanceID, sdk.FrigateStageRef{JobID: job.ID.String(), ManifestSHA256: fingerprint, Actor: actor}); err != nil && !isFinalRefusal(err) {
			slog.WarnContext(ctx, "abort staged frigate package", "job_id", job.ID, "instance_id", inst.InstanceID, "error", err)
		}
		abortCancel()
		return none, errStopStaging
	}
	// Reaching Begin is not progress: a controller that answers Begin but never
	// accepts a chunk must still trip the stall timeout.
	s.startProgressClock(job.ID)

	return stagingCopy{inst: inst, manifest: manifest, fingerprint: fingerprint, next: next}, nil
}

// stageInstance uploads the package to one controller, resuming from the
// controller's acknowledged offset.
func (s *Service) stageInstance(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, file *os.File, inst sqlc.FrigateUpdateInstance) error {
	c, err := s.beginInstance(ctx, updater, job, pkg, inst)
	if err != nil {
		return err
	}
	// A stop or a failed write of the position is not a failed copy, so it
	// does not go through stageError.
	var stopped error
	commit, staged, err := sdk.CopyFrigateStage(ctx, updater, sdk.FrigateStageCopy{
		InstanceID: inst.InstanceID,
		Ref:        sdk.FrigateStageRef{JobID: job.ID.String(), ManifestSHA256: c.fingerprint, Actor: c.manifest.Actor},
		Package:    file, SizeBytes: pkg.SizeBytes, Offset: c.next,
		ChunkTimeout: s.cfg.ChunkTimeout, CallTimeout: s.cfg.CallTimeout,
		OnChunk: func(next int64) error {
			ok, err := s.persistStaging(ctx, job.ID, inst.InstanceID, StageStaging, c.fingerprint, next, "", "")
			if err == nil && !ok {
				err = errStopStaging
			}
			if err != nil {
				stopped = err
				return err
			}
			s.noteProgress(job.ID)
			return nil
		},
	})
	switch {
	case stopped != nil:
		return stopped
	case err != nil:
		return s.stageError(ctx, job, inst, c.fingerprint, staged, err)
	}
	return s.finishInstance(ctx, job, pkg, c, commit)
}

// finishInstance checks what the controller committed, then records the copy
// as staged.
func (s *Service) finishInstance(ctx context.Context, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, c stagingCopy, commit sdk.FrigateStageCommit) error {
	inst, fingerprint := c.inst, c.fingerprint
	if commit.BytesStaged != pkg.SizeBytes || commit.SHA256 != pkg.Sha256 {
		msg := fmt.Sprintf("Controller %s staged %d bytes with SHA-256 %s; expected %d bytes with %s.",
			inst.InstanceID, commit.BytesStaged, commit.SHA256, pkg.SizeBytes, pkg.Sha256)
		return s.failStaging(ctx, job, inst, fingerprint, pkg.SizeBytes, CodeStageDigestMismatch, msg)
	}
	if ok, err := s.persistStaging(ctx, job.ID, inst.InstanceID, StageStaged, fingerprint, pkg.SizeBytes, "", ""); err != nil {
		return err
	} else if !ok {
		return errStopStaging
	}
	s.noteProgress(job.ID)
	return nil
}

// stageError handles a failed staging call. A Frigate refusal is final and
// fails the job; anything else is retried on the next pass.
func (s *Service) stageError(ctx context.Context, job sqlc.FrigateUpdateJob, inst sqlc.FrigateUpdateInstance, fingerprint string, staged int64, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("staging interrupted: %w", ctxErr)
	}
	if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
		msg := fmt.Sprintf("Controller %s refused the package: %s", inst.InstanceID, withDetail(refusal.Reason, refusal.Message))
		return s.failStaging(ctx, job, inst, fingerprint, staged, refusal.Reason, msg)
	}
	if _, perr := s.persistStaging(ctx, job.ID, inst.InstanceID, StageStaging, fingerprint, staged, "", "Retrying: "+err.Error()); perr != nil {
		slog.WarnContext(ctx, "record frigate staging retry", "job_id", job.ID, "error", perr)
	}
	return err
}

// failStaging marks one controller failed and fails the job. Staged copies on
// the controllers are aborted by the cleanup sweep.
func (s *Service) failStaging(ctx context.Context, job sqlc.FrigateUpdateJob, inst sqlc.FrigateUpdateInstance, fingerprint string, staged int64, code, message string) error {
	if _, err := s.persistStaging(ctx, job.ID, inst.InstanceID, StageFailed, fingerprint, staged, code, message); err != nil {
		slog.WarnContext(ctx, "record frigate staging failure", "job_id", job.ID, "error", err)
	}
	s.failJob(ctx, job.ID, code, message, true)
	return errStopStaging
}

// startJob asks Frigate to start the rollout. A Start whose outcome is unknown
// (a deadline or transport error) may have been accepted, so every attempt first
// reads the rollout back: Start is sent only when Frigate does not know the job,
// always with the same idempotency key.
func (s *Service) startJob(ctx context.Context, job sqlc.FrigateUpdateJob) {
	if CancelPending(job) {
		s.finishPendingCancel(ctx, job)
		return
	}
	instances, err := s.queries.ListFrigateUpdateInstances(ctx, job.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list frigate update instances", "job_id", job.ID, "error", err)
		return
	}
	fingerprints := make(map[string]string, len(instances))
	for _, in := range instances {
		if in.StageState != StageStaged || in.ManifestSha256 == "" {
			s.failJob(ctx, job.ID, CodeStageFailed,
				fmt.Sprintf("Controller %s does not have the package staged.", in.InstanceID), true)
			return
		}
		fingerprints[in.InstanceID] = in.ManifestSha256
	}
	updater, err := s.updaterFor(ctx, job.DeviceID)
	if err != nil {
		s.markUnreachable(ctx, job.ID, err)
		return
	}

	// Persist the intent before any network I/O: a crash after this point makes
	// the next pass reconcile instead of assuming Start was never sent.
	_, cur, changed, err := s.mutate(ctx, job.ID, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if cur.State != StateStaged && cur.State != StateStarting {
			return false
		}
		if cur.State == StateStarting && cur.StartAttempted {
			return false
		}
		p.State = StateStarting
		p.StartAttempted = true
		p.Cancellable = true
		if !cur.StartAttemptedAt.Valid {
			p.StartAttemptedAt = nullTime(s.now())
		}
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "record frigate update start intent", "job_id", job.ID, "error", err)
		}
		return
	}
	if !changed && cur.State != StateStarting {
		return // Cancelled or finished concurrently.
	}

	statusCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	status, err := updater.FrigateUpdateStatus(statusCtx, job.ID.String())
	cancel()
	switch {
	case err == nil:
		// Frigate already has the rollout: an earlier Start was accepted.
		after, err := s.applyRollout(ctx, job.ID, rolloutObservation{Rollout: status.Rollout, ServedByMaintenance: status.ServedByMaintenance})
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "apply frigate rollout", "job_id", job.ID, "error", err)
		}
		s.honorCancelIntent(ctx, updater, after, status.Rollout, status.ServedByMaintenance)
		return
	case isNotFoundRefusal(err):
		if s.startExpired(cur) {
			// cur predates the status read; another runner may have leased a
			// Start since. Its outcome is unknown until the lease expires, so a
			// live lease defers the timeout to a later pass.
			fresh, err := s.queries.GetFrigateUpdateJob(ctx, sqlc.GetFrigateUpdateJobParams{OrgID: cur.OrgID, ID: cur.ID})
			if err != nil {
				if ctx.Err() == nil {
					slog.ErrorContext(ctx, "reload frigate update job", "job_id", job.ID, "error", err)
				}
				return
			}
			if fresh.State != StateStarting || s.startLeaseLive(fresh) {
				return
			}
			s.startTimedOut(ctx, updater, fresh)
			return
		}
	default:
		s.markUnreachable(ctx, job.ID, err)
		return
	}

	// Take the start lease, and re-check the state under the job lock, right
	// before the network call: an operator cancel that completed locally in the
	// meantime must not be followed by a Start. The lease keeps a later local
	// cancel from completing while this call can still be in flight.
	_, leasedJob, leased, err := s.mutate(ctx, job.ID, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		// A cancel recorded since the list was read takes no lease: the next
		// pass finishes the cancel instead of starting.
		if cur.State != StateStarting || cur.CancelRequestedAt.Valid {
			return false
		}
		p.StartLeaseUntil = nullTime(s.now().Add(s.cfg.CallTimeout + startLeaseMargin))
		if !cur.StartAttemptedAt.Valid {
			p.StartAttemptedAt = nullTime(s.now())
		}
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "take frigate update start lease", "job_id", job.ID, "error", err)
		}
		return
	}
	if !leased {
		return
	}
	if s.afterStartLease != nil {
		s.afterStartLease()
	}

	// Start must resolve while the persisted lease still holds off a local
	// cancel, so its deadline comes from the lease rather than from now: a slow
	// commit or scheduling delay shortens the call instead of letting it outlive
	// the lease. The margin stays reserved for transport and clock skew.
	remaining := leasedJob.StartLeaseUntil.Time.Add(-startLeaseMargin).Sub(s.now())
	if !leasedJob.StartLeaseUntil.Valid || remaining <= 0 {
		slog.WarnContext(ctx, "frigate update start lease expired before dispatch; retrying on the next pass", "job_id", job.ID)
		return
	}
	startCtx, cancel := context.WithTimeout(ctx, remaining)
	rollout, err := updater.FrigateUpdateStart(startCtx, sdk.FrigateStartRequest{
		JobID:                  job.ID.String(),
		TargetVersion:          job.TargetVersion,
		ExpectedInstanceIDs:    sortedCopy(job.ExpectedInstanceIds),
		StageOnly:              job.StageOnly,
		IdempotencyKey:         derivedKey("start", job.ID, 0),
		ExpectedManifestSHA256: fingerprints,
		Actor:                  actorName(job.ActorUsername, job.ActorExternalID, job.ActorUserID),
	})
	cancel()
	switch {
	case err == nil:
		after, err := s.applyRollout(ctx, job.ID, rolloutObservation{Rollout: rollout})
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "apply frigate rollout", "job_id", job.ID, "error", err)
		}
		s.honorCancelIntent(ctx, updater, after, rollout, false)
	case ctx.Err() != nil:
	case isFinalRefusal(err):
		refusal := sdk.AsFrigateRefusal(err)
		s.failJob(ctx, job.ID, refusal.Reason, withDetail(startRefusalSummary(refusal.Reason), refusal.Message), true)
	default:
		// Ambiguous: leave the job in starting; the next pass reconciles.
		s.markUnreachable(ctx, job.ID, err)
	}
}

// startRefusalSummary names who refused a start: FLEET_ reasons come from
// Fleet's own pre-start checks, which never reach Frigate.
func startRefusalSummary(reason string) string {
	if strings.HasPrefix(reason, "FLEET_") {
		return "Fleet stopped the update before starting it"
	}
	return "The controller refused to start the update"
}

// finishPendingCancel carries out an operator cancel that was recorded while a
// start lease was live. It takes no start lease and sends no Start. It waits
// for any existing lease to expire, so a Start that is still in flight has
// resolved, then reads a fresh Status: no record means nothing can be started
// any more and the cancel completes locally; a record means Frigate has the
// rollout, so Frigate is asked to cancel it; an unreadable status leaves the
// job pending for the next pass.
func (s *Service) finishPendingCancel(ctx context.Context, listed sqlc.FrigateUpdateJob) {
	job, err := s.queries.GetFrigateUpdateJob(ctx, sqlc.GetFrigateUpdateJobParams{OrgID: listed.OrgID, ID: listed.ID})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "load frigate update with a pending cancel", "job_id", listed.ID, "error", err)
		}
		return
	}
	if !CancelPending(job) {
		return
	}
	if s.startLeaseLive(job) {
		return // A Start may still be in flight; wait for its lease to expire.
	}
	updater, err := s.updaterFor(ctx, job.DeviceID)
	if err != nil {
		s.markUnreachable(ctx, job.ID, err)
		return
	}
	statusCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	status, err := updater.FrigateUpdateStatus(statusCtx, job.ID.String())
	cancel()
	switch {
	case ctx.Err() != nil:
		return
	case err == nil:
		s.forwardCancel(ctx, updater, job, status.Rollout, status.ServedByMaintenance)
	case isNotFoundRefusal(err):
		s.completePendingCancel(ctx, job.ID)
	default:
		// Without a readable status nothing proves that no controller has the
		// job, so the cancel stays pending.
		s.markUnreachable(ctx, job.ID, err)
	}
}

// completePendingCancel ends a job as CANCELLED once a fresh status read proved
// that Frigate has no record of it. The lease is re-checked under the job lock.
func (s *Service) completePendingCancel(ctx context.Context, id uuid.UUID) {
	type outcome struct{ before, after sqlc.FrigateUpdateJob }
	res, err := runTx(ctx, s, func(q sqlc.Querier) (outcome, error) {
		cur, err := q.LockFrigateUpdateJob(ctx, id)
		if err != nil {
			return outcome{}, err
		}
		if !CancelPending(cur) || (cur.StartLeaseUntil.Valid && cur.StartLeaseUntil.Time.After(s.now())) {
			return outcome{before: cur, after: cur}, nil
		}
		after, err := completeCancelLocally(ctx, q, cur, cur.CancelRequestKey, s.now())
		return outcome{before: cur, after: after}, err
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "complete frigate update cancel", "job_id", id, "error", err)
		}
		return
	}
	s.logTransition(ctx, res.before, res.after)
	if isTerminal(res.after) {
		s.forget(id)
	}
}

// forwardCancel asks Frigate to cancel a rollout that it accepted while the
// operator's cancel was pending. The key is derived from the operator's action,
// so an interrupted attempt repeats the same Cancel. ro is the newest rollout
// Fleet read. When Frigate refuses because the rollout is past the point of
// cancellation, the job follows ro and the refusal is recorded in the activity
// log. The intent is cleared once Frigate has answered either way; an
// unanswered Cancel leaves it for the next pass.
func (s *Service) forwardCancel(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, ro sdk.FrigateRollout, maintenance bool) {
	var counter int32
	if prior, err := s.queries.GetFrigateUpdateAction(ctx, sqlc.GetFrigateUpdateActionParams{JobID: job.ID, Kind: actionCancel, RequestKey: job.CancelRequestKey}); err == nil {
		counter = prior.Counter
	}
	actor := job.CancelRequestActor
	if actor == "" {
		actor = actorName(job.ActorUsername, job.ActorExternalID, job.ActorUserID)
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	answer, err := updater.FrigateUpdateCancel(callCtx, sdkAction(job.ID, derivedKey("cancel-pending", job.ID, counter), actor))
	cancel()
	apply := func(r sdk.FrigateRollout, served bool) sqlc.FrigateUpdateJob {
		after, err := s.applyRollout(ctx, job.ID, rolloutObservation{Rollout: r, ServedByMaintenance: served})
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "apply frigate rollout", "job_id", job.ID, "error", err)
		}
		return after
	}
	switch {
	case ctx.Err() != nil:
		return
	case err == nil:
		apply(answer, false)
	case isNotFoundRefusal(err):
		// Frigate lost the job between the two calls; the next pass reads it again.
		slog.WarnContext(ctx, "frigate has no record of an update with a pending cancel", "job_id", job.ID, "error", err)
		return
	case isFinalRefusal(err):
		refusal := sdk.AsFrigateRefusal(err)
		slog.WarnContext(ctx, "frigate refused a pending cancel", "job_id", job.ID, "error", err)
		after := apply(ro, maintenance)
		s.logJobEvent(ctx, after, "frigate_update_cancel_refused",
			fmt.Sprintf("The controller refused to cancel the update to %s", after.TargetVersion), activitymodels.ResultFailure,
			withDetail(refusal.Reason, refusal.Message))
	default:
		s.markUnreachable(ctx, job.ID, err)
		return
	}
	if _, _, _, err := s.mutate(ctx, job.ID, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		p.CancelRequestedAt = sql.NullTime{}
		return cur.CancelRequestedAt.Valid
	}); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "clear frigate update cancel intent", "job_id", job.ID, "error", err)
	}
}

// honorCancelIntent forwards an operator cancel that was recorded while a Start
// was in flight and that Start was then accepted: the rollout is running, so
// the cancel goes to Frigate as soon as Fleet sees that.
func (s *Service) honorCancelIntent(ctx context.Context, updater interfaces.FrigateUpdater, after sqlc.FrigateUpdateJob, ro sdk.FrigateRollout, maintenance bool) {
	if after.State == StateRunning && after.CancelRequestedAt.Valid {
		s.forwardCancel(ctx, updater, after, ro, maintenance)
	}
}

// startLeaseLive reports whether a runner holds the start lease, so its Start
// call may still be in flight and its outcome is not yet known.
func (s *Service) startLeaseLive(job sqlc.FrigateUpdateJob) bool {
	return job.StartLeaseUntil.Valid && job.StartLeaseUntil.Time.After(s.now())
}

// startExpired reports whether Start has been retried for longer than the start
// timeout.
func (s *Service) startExpired(job sqlc.FrigateUpdateJob) bool {
	return job.StartAttemptedAt.Valid && s.now().Sub(job.StartAttemptedAt.Time) > s.cfg.StartTimeout
}

// startTimedOut ends a job whose Start never reached Frigate within the start
// timeout. The caller just read a NotFound status, so no controller knows the
// job. A Cancel with a derived key covers a Start that Frigate accepted after
// that read, and the job fails with its staged copies queued for cleanup.
func (s *Service) startTimedOut(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob) {
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	rollout, err := updater.FrigateUpdateCancel(callCtx, sdkAction(job.ID, derivedKey("start-timeout", job.ID, 0),
		actorName(job.ActorUsername, job.ActorExternalID, job.ActorUserID)))
	cancel()
	switch {
	case ctx.Err() != nil:
		return
	case err == nil:
		// Frigate had the job after all; take its answer.
		if _, err := s.applyRollout(ctx, job.ID, rolloutObservation{Rollout: rollout}); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "apply frigate rollout", "job_id", job.ID, "error", err)
		}
		return
	case isNotFoundRefusal(err):
	case isFinalRefusal(err):
		// Frigate knows the job but will not cancel it; the next pass reads its state.
		slog.WarnContext(ctx, "frigate refused to cancel an update that timed out starting", "job_id", job.ID, "error", err)
		return
	default:
		// The controllers did not answer the Cancel. Fail anyway: the status read
		// that led here was fresh and nothing proves Frigate will ever accept it.
		slog.WarnContext(ctx, "could not cancel an update that timed out starting", "job_id", job.ID, "error", err)
	}
	// Re-check the lease under the job lock: job predates the status read, and
	// a Start another runner leased since then may still be accepted.
	s.failJobUnless(ctx, job.ID, CodeStartTimeout,
		fmt.Sprintf("No controller accepted the update within %s or has a record of it.", s.cfg.StartTimeout), true,
		s.startLeaseLive)
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// pollJob reads the rollout status. Frigate's maintenance service answers while
// the controllers are stopped, so a poll keeps working through the install.
func (s *Service) pollJob(ctx context.Context, job sqlc.FrigateUpdateJob) {
	updater, err := s.updaterFor(ctx, job.DeviceID)
	if err != nil {
		s.markUnreachable(ctx, job.ID, err)
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	status, err := updater.FrigateUpdateStatus(callCtx, job.ID.String())
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if isNotFoundRefusal(err) {
			s.rolloutMissing(ctx, job)
			return
		}
		s.markUnreachable(ctx, job.ID, err)
		return
	}
	s.mu.Lock()
	delete(s.lost, job.ID)
	s.mu.Unlock()
	after, err := s.applyRollout(ctx, job.ID, rolloutObservation{Rollout: status.Rollout, ServedByMaintenance: status.ServedByMaintenance})
	if err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "apply frigate rollout", "job_id", job.ID, "error", err)
	}
	s.honorCancelIntent(ctx, updater, after, status.Rollout, status.ServedByMaintenance)
}

// rolloutMissing handles Frigate not knowing a job Fleet believes is running.
// Only repeated answers count, so a controller that is still catching up after a
// failover does not end the job.
func (s *Service) rolloutMissing(ctx context.Context, job sqlc.FrigateUpdateJob) {
	s.mu.Lock()
	s.lost[job.ID]++
	count := s.lost[job.ID]
	s.mu.Unlock()
	if count < rolloutLostPolls {
		return
	}
	before, after, changed, err := s.mutate(ctx, job.ID, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
		if cur.State != StateRunning && !(cur.State == StateFailed && cur.Retryable) {
			return false
		}
		p.State = StateFailed
		p.ErrorCode = CodeRolloutLost
		p.ErrorMessage = "The controllers no longer have a record of this update."
		p.NextStep = "Check the controllers' running version; start the update again if it did not install."
		p.Retryable = false
		p.Cancellable = false
		p.CompletedAt = nullTime(s.now())
		return true
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "fail lost frigate update", "job_id", job.ID, "error", err)
		}
		return
	}
	if changed {
		s.forget(job.ID)
		s.logTransition(ctx, before, after)
	}
}

// sweepCleanup aborts staged copies of jobs that ended before Frigate started
// them. It is best effort, bounded by cleanupPassBudget, and gives up on a job
// after a day. A job that could not be cleaned is stamped so the next pass
// starts with the ones it has not tried.
func (s *Service) sweepCleanup(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, cleanupPassBudget)
	defer cancel()
	jobs, err := s.queries.ListFrigateUpdateCleanupJobs(ctx)
	if err != nil {
		if parent.Err() == nil {
			slog.ErrorContext(parent, "list frigate update cleanup jobs", "error", err)
		}
		return
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		if s.cleanupJob(ctx, job) {
			if _, _, _, err := s.mutate(parent, job.ID, func(cur sqlc.FrigateUpdateJob, p *sqlc.UpdateFrigateUpdateJobParams) bool {
				p.CleanupPending = false
				// A relay copy that ended during this pass may have queued
				// cleanup again for a copy that this pass did not abort.
				return cur.CleanupPending && cur.UpdatedAt.Equal(job.UpdatedAt)
			}); err != nil && parent.Err() == nil {
				slog.ErrorContext(parent, "clear frigate update cleanup", "job_id", job.ID, "error", err)
			}
			continue
		}
		if err := s.queries.MarkFrigateUpdateCleanupAttempted(parent, sqlc.MarkFrigateUpdateCleanupAttemptedParams{
			ID: job.ID, AttemptedAt: nullTime(s.now()),
		}); err != nil && parent.Err() == nil {
			slog.ErrorContext(parent, "record frigate update cleanup attempt", "job_id", job.ID, "error", err)
		}
	}
}

// resolveFingerprint learns the fingerprint of a copy whose Begin reply was
// never recorded by repeating the same Begin, which Frigate answers with the
// existing copy. It returns an empty fingerprint when Frigate refuses, which
// leaves nothing Fleet can abort, and false when the call should be retried.
func (s *Service) resolveFingerprint(ctx context.Context, updater interfaces.FrigateUpdater, job sqlc.FrigateUpdateJob, pkg sqlc.GetFrigateUpdatePackageRow, instanceID string) (string, bool) {
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	progress, err := updater.FrigateStageBegin(callCtx, instanceID, manifestFor(job, pkg, instanceID))
	cancel()
	switch {
	case err == nil:
		return progress.ManifestSHA256, progress.ManifestSHA256 != ""
	case isFinalRefusal(err):
		return "", true
	default:
		slog.WarnContext(ctx, "resolve staged frigate package for cleanup", "job_id", job.ID, "instance_id", instanceID, "error", err)
		return "", false
	}
}

// cleanupJob reports whether nothing is left to clean up.
func (s *Service) cleanupJob(ctx context.Context, job sqlc.FrigateUpdateJob) bool {
	completed := job.UpdatedAt
	if job.CompletedAt.Valid {
		completed = job.CompletedAt.Time
	}
	if s.now().Sub(completed) > cleanupGiveUp {
		slog.WarnContext(ctx, "gave up cleaning up staged frigate update copies", "job_id", job.ID)
		return true
	}
	updater, err := s.updaterFor(ctx, job.DeviceID)
	if err != nil {
		// A removed container has no controllers Fleet may still address.
		// Frigate expires partial stages itself, and a committed stage cannot
		// install without a Start, which only Fleet sends.
		if _, lookupErr := s.queries.GetFrigateUpdateDeviceSite(ctx, job.DeviceID); errors.Is(lookupErr, sql.ErrNoRows) {
			slog.WarnContext(ctx, "frigate update cleanup skipped: container was removed", "job_id", job.ID)
			return true
		}
		return false
	}
	instances, err := s.queries.ListFrigateUpdateInstances(ctx, job.ID)
	if err != nil {
		return false
	}
	done := true
	actor := actorName(job.ActorUsername, job.ActorExternalID, job.ActorUserID)
	var pkg *sqlc.GetFrigateUpdatePackageRow
	for _, in := range instances {
		if in.StageState == StagePending {
			continue
		}
		fingerprint := in.ManifestSha256
		if fingerprint == "" {
			if pkg == nil {
				row, err := s.queries.GetFrigateUpdatePackage(ctx, sqlc.GetFrigateUpdatePackageParams{OrgID: job.OrgID, ID: job.PackageID})
				if err != nil {
					slog.WarnContext(ctx, "load frigate package for cleanup", "job_id", job.ID, "error", err)
					return false
				}
				pkg = &row
			}
			resolved, ok := s.resolveFingerprint(ctx, updater, job, *pkg, in.InstanceID)
			if !ok {
				done = false
				continue
			}
			if resolved == "" {
				continue
			}
			fingerprint = resolved
		}
		callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
		err := updater.FrigateStageAbort(callCtx, in.InstanceID, sdk.FrigateStageRef{JobID: job.ID.String(), ManifestSHA256: fingerprint, Actor: actor})
		cancel()
		if err != nil && !isFinalRefusal(err) {
			slog.WarnContext(ctx, "abort staged frigate package", "job_id", job.ID, "instance_id", in.InstanceID, "error", err)
			done = false
		}
	}
	return done
}
