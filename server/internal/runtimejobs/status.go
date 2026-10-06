package runtimejobs

import (
	"context"
	"time"
)

const progressStaleIntervalCount = 3

// State is the current lifecycle state of a group or job.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

// GroupStatus is a point-in-time snapshot of a group and its jobs.
type GroupStatus struct {
	State         State
	TerminalError error
	Jobs          []JobStatus
}

// JobStatus is a point-in-time snapshot of one job. StaleAfter is zero when
// progress is not tracked.
type JobStatus struct {
	Name         string
	State        State
	LastProgress time.Time
	StaleAfter   time.Duration
	Stale        bool
}

type progressContext struct {
	group      *Group
	jobIndex   int
	generation uint64
}

type progressContextKey struct{}

// TrackProgress registers freshness tracking for the managed job associated
// with ctx. A job becomes stale after three loop intervals without a completed
// representative work cycle. Calling the returned function reports completion
// of one such cycle. It is a no-op outside a Group-managed Start context or
// when effectiveInterval is not positive.
func TrackProgress(ctx context.Context, effectiveInterval time.Duration) func() {
	if effectiveInterval <= 0 || ctx.Err() != nil {
		return func() {}
	}
	progress, ok := ctx.Value(progressContextKey{}).(progressContext)
	if !ok {
		return func() {}
	}
	report := progress.group.trackProgress(
		progress.jobIndex,
		progress.generation,
		progressStaleIntervalCount*effectiveInterval,
	)
	return func() {
		if ctx.Err() == nil {
			report()
		}
	}
}
