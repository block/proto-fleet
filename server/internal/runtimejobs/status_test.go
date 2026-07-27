package runtimejobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupStatusReportsLifecycleTransitionsInCatalogOrder(t *testing.T) {
	t.Parallel()

	startSecond := make(chan struct{})
	stopSecond := make(chan struct{})
	startResult := make(chan error, 1)
	stopResult := make(chan error, 1)
	group := newTestGroup(t,
		newTestJob("first", nil, nil),
		newTestJob(
			"second",
			func(context.Context) error {
				<-startSecond
				return nil
			},
			func(context.Context) error {
				<-stopSecond
				return nil
			},
		),
		newTestJob("third", nil, nil),
	)

	assert.Equal(t, GroupStatus{
		State: StateStopped,
		Jobs: []JobStatus{
			{Name: "first", State: StateStopped},
			{Name: "second", State: StateStopped},
			{Name: "third", State: StateStopped},
		},
	}, group.Status())

	go func() {
		startResult <- group.Start(context.Background())
	}()
	require.Eventually(t, func() bool {
		status := group.Status()
		return status.State == StateStarting &&
			status.Jobs[0].State == StateRunning &&
			status.Jobs[1].State == StateStarting &&
			status.Jobs[2].State == StateStopped
	}, time.Second, time.Millisecond)

	close(startSecond)
	require.NoError(t, <-startResult)
	assert.Equal(t, StateRunning, group.Status().State)
	assert.Equal(t, []JobStatus{
		{Name: "first", State: StateRunning},
		{Name: "second", State: StateRunning},
		{Name: "third", State: StateRunning},
	}, group.Status().Jobs)

	go func() {
		stopResult <- group.Stop(context.Background())
	}()
	require.Eventually(t, func() bool {
		status := group.Status()
		return status.State == StateStopping &&
			status.Jobs[0].State == StateRunning &&
			status.Jobs[1].State == StateStopping &&
			status.Jobs[2].State == StateStopped
	}, time.Second, time.Millisecond)

	close(stopSecond)
	require.NoError(t, <-stopResult)
	assert.Equal(t, GroupStatus{
		State: StateStopped,
		Jobs: []JobStatus{
			{Name: "first", State: StateStopped},
			{Name: "second", State: StateStopped},
			{Name: "third", State: StateStopped},
		},
	}, group.Status())
}

func TestTrackProgressReportsFreshnessAndIsolatesActivations(t *testing.T) {
	t.Parallel()

	reporters := make(chan func(), 2)
	group := newTestGroup(t, newTestJob("job", func(ctx context.Context) error {
		reporters <- TrackProgress(ctx, time.Hour)
		return nil
	}, nil))

	require.NoError(t, group.Start(context.Background()))
	oldReporter := <-reporters
	first := group.Status()
	require.True(t, first.Jobs[0].ProgressTracked)
	assert.WithinDuration(t, time.Now(), first.Jobs[0].LastProgress, time.Second)
	assert.Equal(t, time.Hour, first.Jobs[0].StaleAfter)
	assert.False(t, first.Jobs[0].Stale)
	require.NoError(t, group.Stop(context.Background()))

	stopped := group.Status()
	assert.True(t, stopped.Jobs[0].ProgressTracked)
	assert.False(t, stopped.Jobs[0].Stale)

	activationCtx, cancelActivation := context.WithCancel(context.Background())
	require.NoError(t, group.Start(activationCtx))
	currentReporter := <-reporters
	beforeOldReport := group.Status().Jobs[0].LastProgress
	time.Sleep(time.Millisecond)
	oldReporter()
	assert.Equal(t, beforeOldReport, group.Status().Jobs[0].LastProgress)

	time.Sleep(time.Millisecond)
	currentReporter()
	assert.True(t, group.Status().Jobs[0].LastProgress.After(beforeOldReport))

	beforeCancellation := group.Status().Jobs[0].LastProgress
	cancelActivation()
	currentReporter()
	assert.Equal(t, beforeCancellation, group.Status().Jobs[0].LastProgress)
	require.NoError(t, group.Stop(context.Background()))
}

func TestProgressReporterAcceptsCycleBeforeStartReturns(t *testing.T) {
	t.Parallel()

	var group *Group
	group = newTestGroup(t, newTestJob("job", func(ctx context.Context) error {
		report := TrackProgress(ctx, time.Hour)
		registered := group.Status().Jobs[0].LastProgress
		time.Sleep(time.Millisecond)
		report()
		if !group.Status().Jobs[0].LastProgress.After(registered) {
			return errors.New("progress was not recorded while the job was starting")
		}
		return nil
	}, nil))

	require.NoError(t, group.Start(context.Background()))
	require.NoError(t, group.Stop(context.Background()))
}

func TestProgressMonitorLogsOnlyStaleAndRecoveredTransitions(t *testing.T) {
	t.Parallel()

	reporter := make(chan func(), 1)
	group := newTestGroup(t, newTestJob("periodic", func(ctx context.Context) error {
		reporter <- TrackProgress(ctx, 15*time.Millisecond)
		return nil
	}, nil))
	logs := &recordingHandler{}
	group.logger = slog.New(logs)
	group.monitorInterval = time.Millisecond

	require.NoError(t, group.Start(context.Background()))
	report := <-reporter
	require.Eventually(t, func() bool {
		return group.Status().Jobs[0].Stale
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		return logs.countMessage("runtime job stale") == 1
	}, time.Second, time.Millisecond)
	staleRecord, ok := logs.firstMessage("runtime job stale")
	require.True(t, ok)
	staleAttrs := recordAttrs(staleRecord)
	assert.Equal(t, "periodic", staleAttrs["job"].String())
	assert.Equal(t, slog.KindTime, staleAttrs["last_progress"].Kind())
	assert.Equal(t, 15*time.Millisecond, staleAttrs["stale_after"].Duration())

	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, logs.countMessage("runtime job stale"))
	assert.Equal(t, 0, logs.countMessage("runtime job recovered"))

	report()
	require.Eventually(t, func() bool {
		return !group.Status().Jobs[0].Stale
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		return logs.countMessage("runtime job recovered") == 1
	}, time.Second, time.Millisecond)

	report()
	time.Sleep(5 * time.Millisecond)
	assert.Equal(t, 1, logs.countMessage("runtime job stale"))
	assert.Equal(t, 1, logs.countMessage("runtime job recovered"))
	require.NoError(t, group.Stop(context.Background()))
}

func TestUntrackedJobsNeverBecomeStale(t *testing.T) {
	t.Parallel()

	tracked := make(chan struct{}, 1)
	group := newTestGroup(t,
		newTestJob("untracked", nil, nil),
		newTestJob("tracked", func(ctx context.Context) error {
			TrackProgress(ctx, time.Millisecond)
			tracked <- struct{}{}
			return nil
		}, nil),
	)
	group.monitorInterval = time.Millisecond

	require.NoError(t, group.Start(context.Background()))
	<-tracked
	require.Eventually(t, func() bool {
		return group.Status().Jobs[1].Stale
	}, time.Second, time.Millisecond)
	assert.False(t, group.Status().Jobs[0].Stale)

	require.NoError(t, group.Stop(context.Background()))
}

func TestTrackProgressOutsideManagedJobIsNoop(t *testing.T) {
	t.Parallel()

	report := TrackProgress(context.Background(), time.Nanosecond)
	assert.NotPanics(t, report)
}

func TestStatusReturnsIndependentSnapshots(t *testing.T) {
	t.Parallel()

	group := newTestGroup(t, newTestJob("job", nil, nil))
	snapshot := group.Status()
	snapshot.Jobs[0].Name = "changed"
	assert.Equal(t, "job", group.Status().Jobs[0].Name)
}

func TestStatusIsSafeDuringConcurrentLifecycleAndProgress(t *testing.T) {
	t.Parallel()

	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	stopEntered := make(chan struct{})
	releaseStop := make(chan struct{})
	reporter := make(chan func(), 1)
	group := newTestGroup(t, newTestJob("job", func(ctx context.Context) error {
		reporter <- TrackProgress(ctx, time.Hour)
		close(startEntered)
		<-releaseStart
		return nil
	}, func(context.Context) error {
		close(stopEntered)
		<-releaseStop
		return nil
	}))

	startResult := make(chan error, 1)
	go func() {
		startResult <- group.Start(context.Background())
	}()
	<-startEntered
	report := <-reporter

	done := make(chan struct{})
	var observers sync.WaitGroup
	for range 8 {
		observers.Add(1)
		go func() {
			defer observers.Done()
			for {
				select {
				case <-done:
					return
				default:
					report()
					_ = group.Status()
				}
			}
		}()
	}

	close(releaseStart)
	require.NoError(t, <-startResult)

	stopResult := make(chan error, 1)
	go func() {
		stopResult <- group.Stop(context.Background())
	}()
	<-stopEntered
	close(releaseStop)
	require.NoError(t, <-stopResult)

	close(done)
	observers.Wait()
	assert.Equal(t, StateStopped, group.Status().State)
}

func TestGroupLogsCanonicalJobLifecycleTransitions(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	stopErr := errors.New("stop failed")
	tests := []struct {
		name          string
		job           Job
		run           func(*Group) error
		message       string
		level         slog.Level
		expectedError error
	}{
		{
			name:    "start success",
			job:     newTestJob("job", nil, nil),
			run:     func(group *Group) error { return group.Start(context.Background()) },
			message: "runtime job started",
			level:   slog.LevelInfo,
		},
		{
			name: "start failure",
			job: newTestJob("job", func(context.Context) error {
				return startErr
			}, nil),
			run:           func(group *Group) error { return group.Start(context.Background()) },
			message:       "runtime job start failed",
			level:         slog.LevelError,
			expectedError: startErr,
		},
		{
			name: "stop success",
			job:  newTestJob("job", nil, nil),
			run: func(group *Group) error {
				if err := group.Start(context.Background()); err != nil {
					return err
				}
				return group.Stop(context.Background())
			},
			message: "runtime job stopped",
			level:   slog.LevelInfo,
		},
		{
			name: "stop failure",
			job: newTestJob("job", nil, func(context.Context) error {
				return stopErr
			}),
			run: func(group *Group) error {
				if err := group.Start(context.Background()); err != nil {
					return err
				}
				return group.Stop(context.Background())
			},
			message:       "runtime job stop failed",
			level:         slog.LevelError,
			expectedError: stopErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logs := &recordingHandler{}
			group := newTestGroup(t, tt.job)
			group.logger = slog.New(logs)

			err := tt.run(group)
			if tt.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.expectedError)
			}

			record, ok := logs.firstMessage(tt.message)
			require.True(t, ok)
			assert.Equal(t, tt.level, record.Level)
			attrs := recordAttrs(record)
			assert.Equal(t, "job", attrs["job"].String())
			assert.Equal(t, slog.KindDuration, attrs["duration"].Kind())
			if tt.expectedError != nil {
				loggedErr, ok := attrs["error"].Any().(error)
				require.True(t, ok)
				assert.Equal(t, tt.expectedError.Error(), loggedErr.Error())
			}
		})
	}
}

type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*recordingHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *recordingHandler) WithGroup(string) slog.Handler {
	return h
}

func (h *recordingHandler) countMessage(message string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for _, record := range h.records {
		if record.Message == message {
			count++
		}
	}
	return count
}

func (h *recordingHandler) firstMessage(message string) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, record := range h.records {
		if record.Message == message {
			return record.Clone(), true
		}
	}
	return slog.Record{}, false
}

func recordAttrs(record slog.Record) map[string]slog.Value {
	attrs := make(map[string]slog.Value)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value
		return true
	})
	return attrs
}
