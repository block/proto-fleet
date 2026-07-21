package runtimejobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJobValidation(t *testing.T) {
	t.Parallel()

	_, err := NewJob("", testLifecycle{start: noopJob, stop: noopJob})
	require.EqualError(t, err, "name must not be empty")

	_, err = NewJob("job", nil)
	require.EqualError(t, err, "lifecycle must not be nil")
}

func TestNewGroupValidation(t *testing.T) {
	t.Parallel()

	_, err := NewGroup([]Job{{name: "job"}}, time.Second)
	require.ErrorContains(t, err, "runtime job 0: lifecycle must not be nil")

	job := newTestJob("job", nil, nil)
	_, err = NewGroup([]Job{job, job}, time.Second)
	require.ErrorContains(t, err, "appears more than once")

	_, err = NewGroup(nil, 0)
	require.ErrorContains(t, err, "cleanup timeout must be positive")
}

func TestGroupStartsInOrderAndStopsInReverseAfterBroadcastingCancellation(t *testing.T) {
	t.Parallel()

	var events eventLog
	makeJob := func(name string) Job {
		var activationDone <-chan struct{}
		return newTestJob(
			name,
			func(ctx context.Context) error {
				activationDone = ctx.Done()
				events.add("start:" + name)
				return nil
			},
			func(context.Context) error {
				select {
				case <-activationDone:
				default:
					return errors.New("activation context was not canceled before stop")
				}
				events.add("stop:" + name)
				return nil
			},
		)
	}

	group := newTestGroup(t, makeJob("a"), makeJob("b"), makeJob("c"))
	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Stop(context.Background()))

	assert.Equal(t, []string{
		"start:a", "start:b", "start:c",
		"stop:c", "stop:b", "stop:a",
	}, events.snapshot())
}

func TestGroupRollsBackCleanlyAndCanRetry(t *testing.T) {
	t.Parallel()

	var events eventLog
	var failingStarts atomic.Int32
	makeJob := func(name string) Job {
		return newTestJob(
			name,
			func(context.Context) error {
				events.add("start:" + name)
				if name == "c" && failingStarts.Add(1) == 1 {
					return errors.New("start c")
				}
				return nil
			},
			func(context.Context) error {
				events.add("stop:" + name)
				return nil
			},
		)
	}
	group := newTestGroup(t, makeJob("a"), makeJob("b"), makeJob("c"))

	err := group.Start(context.Background())
	require.ErrorContains(t, err, "start c")
	assert.Equal(t, []string{"start:a", "start:b", "start:c", "stop:b", "stop:a"}, events.snapshot())

	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Stop(context.Background()))
}

func TestGroupRollbackUsesStartDeadline(t *testing.T) {
	t.Parallel()

	rollbackDeadline := make(chan time.Time, 1)
	group := newTestGroup(t,
		newTestJob("started", nil, func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				return errors.New("rollback context has no deadline")
			}
			rollbackDeadline <- deadline
			return nil
		}),
		newTestJob("fails", func(context.Context) error { return errors.New("start failed") }, nil),
	)

	startCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	wantDeadline, ok := startCtx.Deadline()
	require.True(t, ok)

	require.ErrorContains(t, group.Start(startCtx), "start failed")
	assert.True(t, wantDeadline.Equal(<-rollbackDeadline), "rollback must retain the Start caller's deadline")
}

func TestGroupRollbackFailureIsTerminal(t *testing.T) {
	t.Parallel()

	rollbackErr := errors.New("rollback failed")
	group := newTestGroup(t,
		newTestJob("a", nil, func(context.Context) error { return rollbackErr }),
		newTestJob("b", func(context.Context) error { return errors.New("start failed") }, nil),
	)

	err := group.Start(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, rollbackErr)

	err = group.Start(context.Background())
	require.ErrorContains(t, err, "cannot restart after incomplete cleanup")
	assert.ErrorIs(t, err, rollbackErr)
	assert.ErrorIs(t, group.Err(), rollbackErr)
}

func TestGroupStopAggregatesErrorsAndBecomesTerminal(t *testing.T) {
	t.Parallel()

	errA := errors.New("stop a")
	errB := errors.New("stop b")
	group := newTestGroup(t,
		newTestJob("a", nil, func(context.Context) error { return errA }),
		newTestJob("b", nil, func(context.Context) error { return errB }),
	)
	require.NoError(t, group.Start(context.Background()))

	err := group.Stop(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, errA)
	assert.ErrorIs(t, err, errB)

	err = group.Start(context.Background())
	require.ErrorContains(t, err, "cannot restart after incomplete cleanup")
	assert.ErrorIs(t, err, errA)
	assert.ErrorIs(t, group.Err(), errA)
}

func TestGroupStopTimeoutIsGroupWideAndTerminal(t *testing.T) {
	t.Parallel()

	stopEntered := make(chan struct{})
	allowStop := make(chan struct{})
	defer close(allowStop)
	group, err := NewGroup([]Job{newTestJob(
		"stuck",
		noopJob,
		func(context.Context) error {
			close(stopEntered)
			<-allowStop
			return nil
		},
	)}, 20*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, group.Start(context.Background()))

	started := time.Now()
	err = group.Stop(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
	select {
	case <-stopEntered:
	default:
		t.Fatal("stop was not attempted")
	}

	err = group.Start(context.Background())
	require.ErrorContains(t, err, "cannot restart after incomplete cleanup")
}

func TestGroupStopSharesOneDeadlineAcrossJobs(t *testing.T) {
	t.Parallel()

	deadlines := make(chan time.Time, 2)
	makeJob := func(name string, waitForDeadline bool) Job {
		return newTestJob(
			name,
			noopJob,
			func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("stop context has no deadline")
				}
				deadlines <- deadline
				if waitForDeadline {
					<-ctx.Done()
					return ctx.Err()
				}
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		)
	}
	group, err := NewGroup([]Job{
		makeJob("waits-for-deadline", true),
		makeJob("uses-part-of-budget", false),
	}, 30*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, group.Start(context.Background()))
	require.ErrorIs(t, group.Stop(context.Background()), context.DeadlineExceeded)

	first := <-deadlines
	second := <-deadlines
	assert.True(t, first.Equal(second), "every job must receive the same group-wide deadline")
}

func TestGroupStopHonorsCallerCancellation(t *testing.T) {
	t.Parallel()

	stopEntered := make(chan struct{})
	releaseStop := make(chan struct{})
	defer close(releaseStop)
	group := newTestGroup(t, newTestJob(
		"job",
		nil,
		func(context.Context) error {
			close(stopEntered)
			<-releaseStop
			return nil
		},
	))
	require.NoError(t, group.Start(context.Background()))

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err := group.Stop(stopCtx)
	require.ErrorIs(t, err, context.Canceled)

	require.Eventually(t, func() bool {
		select {
		case <-stopEntered:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond, "stop was not attempted")
}

func TestGroupStartAndStopAreIdempotent(t *testing.T) {
	t.Parallel()

	var starts atomic.Int32
	var stops atomic.Int32
	group := newTestGroup(t, newTestJob(
		"job",
		func(context.Context) error { starts.Add(1); return nil },
		func(context.Context) error { stops.Add(1); return nil },
	))

	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Stop(context.Background()))
	require.NoError(t, group.Stop(context.Background()))
	assert.Equal(t, int32(1), starts.Load())
	assert.Equal(t, int32(1), stops.Load())
}

func TestGroupRequiresStopAfterActivationContextEnds(t *testing.T) {
	t.Parallel()

	group := newTestGroup(t, newTestJob("job", nil, nil))
	activationCtx, cancel := context.WithCancel(context.Background())
	require.NoError(t, group.Start(activationCtx))
	cancel()

	err := group.Start(context.Background())
	require.ErrorContains(t, err, "activation ended before stop")
	assert.ErrorIs(t, err, context.Canceled)

	require.NoError(t, group.Stop(context.Background()))
	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Stop(context.Background()))
}

func TestGroupRestartUsesFreshActivationContext(t *testing.T) {
	t.Parallel()

	var contexts []context.Context
	group := newTestGroup(t, newTestJob(
		"job",
		func(ctx context.Context) error {
			contexts = append(contexts, ctx)
			return nil
		},
		noopJob,
	))

	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Stop(context.Background()))
	require.NoError(t, group.Start(context.Background()))

	require.Len(t, contexts, 2)
	first := contexts[0]
	second := contexts[1]
	select {
	case <-first.Done():
	default:
		t.Fatal("first activation context was not canceled")
	}
	select {
	case <-second.Done():
		t.Fatal("second activation context should still be active")
	default:
	}
	require.NoError(t, group.Stop(context.Background()))
}

func newTestGroup(t *testing.T, jobs ...Job) *Group {
	t.Helper()
	group, err := NewGroup(jobs, time.Second)
	require.NoError(t, err)
	return group
}

func newTestJob(name string, start, stop func(context.Context) error) Job {
	if start == nil {
		start = noopJob
	}
	if stop == nil {
		stop = noopJob
	}
	job, err := NewJob(name, testLifecycle{start: start, stop: stop})
	if err != nil {
		panic(err)
	}
	return job
}

func noopJob(context.Context) error { return nil }

type testLifecycle struct {
	start func(context.Context) error
	stop  func(context.Context) error
}

func (l testLifecycle) Start(ctx context.Context) error { return l.start(ctx) }
func (l testLifecycle) Stop(ctx context.Context) error  { return l.stop(ctx) }

type eventLog struct {
	events []string
}

func (l *eventLog) add(event string) {
	l.events = append(l.events, event)
}

func (l *eventLog) snapshot() []string {
	return append([]string(nil), l.events...)
}
