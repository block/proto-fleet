package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// ============================================================================
// newWatcher Tests
// ============================================================================

func TestNewWatcher_WithValidConfig_ShouldUseConfigValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	config := Config{
		WatchPollInterval:  30 * time.Second,
		WatchChannelBuffer: 20,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	w := newWatcher(svc, 1, nil, config)

	assert.Equal(t, 30*time.Second, w.pollInterval)
	assert.Equal(t, 20, cap(w.updateChan))
	assert.Equal(t, int64(1), w.orgID)
	assert.NotNil(t, w.opts)
}

func TestNewWatcher_WithZeroConfig_ShouldUseFallbackDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	config := Config{
		WatchPollInterval:  0, // Zero - should use default
		WatchChannelBuffer: 0, // Zero - should use default
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	w := newWatcher(svc, 1, nil, config)

	assert.Equal(t, defaultWatchPollInterval, w.pollInterval)
	assert.Equal(t, defaultWatchChannelBuffer, cap(w.updateChan))
}

func TestNewWatcher_WithNegativeConfig_ShouldUseFallbackDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	config := Config{
		WatchPollInterval:  -5 * time.Second,
		WatchChannelBuffer: -10,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	w := newWatcher(svc, 1, nil, config)

	assert.Equal(t, defaultWatchPollInterval, w.pollInterval)
	assert.Equal(t, defaultWatchChannelBuffer, cap(w.updateChan))
}

func TestNewWatcher_WithNilOpts_ShouldCreateEmptyOpts(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	svc := NewService(context.Background(), Config{}, mockStore, mockTransactor)

	w := newWatcher(svc, 1, nil, Config{})

	assert.NotNil(t, w.opts)
}

func TestNewWatcher_WithOpts_ShouldPreserveOpts(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	svc := NewService(context.Background(), Config{}, mockStore, mockTransactor)
	opts := &WatchOptions{
		Filter: &models.QueryFilter{
			DeviceIdentifiers: []string{"device-1"},
		},
	}

	w := newWatcher(svc, 1, opts, Config{})

	assert.Equal(t, opts, w.opts)
	assert.Equal(t, []string{"device-1"}, w.opts.Filter.DeviceIdentifiers)
}

// ============================================================================
// buildFilterWithTimeFrom Tests
// ============================================================================

func TestBuildFilterWithTimeFrom_WithNoOptsFilter_ShouldSetTimeFromAndIncludeClosed(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	svc := NewService(context.Background(), Config{}, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, Config{})
	w.lastPollTime = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	filter := w.buildFilterWithTimeFrom()

	require.NotNil(t, filter.TimeFrom)
	assert.Equal(t, w.lastPollTime, *filter.TimeFrom)
	assert.True(t, filter.IncludeClosed)
}

func TestBuildFilterWithTimeFrom_WithOptsFilter_ShouldMergeFilters(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	svc := NewService(context.Background(), Config{}, mockStore, mockTransactor)
	opts := &WatchOptions{
		Filter: &models.QueryFilter{
			DeviceIdentifiers: []string{"device-1", "device-2"},
			Severities:        []models.Severity{models.SeverityCritical},
			MinerErrors:       []models.MinerError{models.PSUFaultGeneric},
			Logic:             models.FilterLogicOR,
		},
	}
	w := newWatcher(svc, 1, opts, Config{})
	w.lastPollTime = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	filter := w.buildFilterWithTimeFrom()

	// Should preserve user-provided filters
	assert.Equal(t, []string{"device-1", "device-2"}, filter.DeviceIdentifiers)
	assert.Equal(t, []models.Severity{models.SeverityCritical}, filter.Severities)
	assert.Equal(t, []models.MinerError{models.PSUFaultGeneric}, filter.MinerErrors)
	assert.Equal(t, models.FilterLogicOR, filter.Logic)

	// Should override time_from and include_closed
	require.NotNil(t, filter.TimeFrom)
	assert.Equal(t, w.lastPollTime, *filter.TimeFrom)
	assert.True(t, filter.IncludeClosed)
}

func TestBuildFilterWithTimeFrom_WithOptsFilterHavingTimeTo_ShouldIgnoreUserTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	userTimeFrom := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	userTimeTo := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	svc := NewService(context.Background(), Config{}, mockStore, mockTransactor)
	opts := &WatchOptions{
		Filter: &models.QueryFilter{
			TimeFrom:      &userTimeFrom, // Should be ignored
			TimeTo:        &userTimeTo,   // Should be ignored
			IncludeClosed: false,         // Should be overridden
		},
	}
	w := newWatcher(svc, 1, opts, Config{})
	w.lastPollTime = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	filter := w.buildFilterWithTimeFrom()

	// Should use watcher's lastPollTime, not user's TimeFrom
	require.NotNil(t, filter.TimeFrom)
	assert.Equal(t, w.lastPollTime, *filter.TimeFrom)
	// TimeTo is not copied
	assert.Nil(t, filter.TimeTo)
	// IncludeClosed is always true for watch
	assert.True(t, filter.IncludeClosed)
}

// ============================================================================
// sendEvent Tests
// ============================================================================

func TestSendEvent_WithSpaceInChannel_ShouldSendUpdate(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	config := Config{WatchChannelBuffer: 5}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)

	ctx := context.Background()
	errors := []models.ErrorMessage{{ErrorID: "ERR1"}}

	w.sendEvent(ctx, WatchKindOpened, errors)

	// Should receive the event
	select {
	case update := <-w.updateChan:
		assert.Equal(t, WatchKindOpened, update.Kind)
		assert.Len(t, update.Errors, 1)
		assert.Equal(t, "ERR1", update.Errors[0].ErrorID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected update but channel was empty")
	}
}

func TestSendEvent_WithFullChannel_ShouldDropUpdate(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	config := Config{WatchChannelBuffer: 1}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)

	ctx := context.Background()

	// Fill the channel
	w.updateChan <- &WatchUpdate{Kind: WatchKindOpened}

	// Try to send another (should be dropped)
	errors := []models.ErrorMessage{{ErrorID: "ERR_DROPPED"}}
	w.sendEvent(ctx, WatchKindUpdated, errors)

	// Should only have the first event
	update := <-w.updateChan
	assert.Equal(t, WatchKindOpened, update.Kind)

	// Channel should be empty now
	select {
	case <-w.updateChan:
		t.Fatal("channel should be empty after reading first event")
	default:
		// Expected - channel is empty
	}
}

func TestSendEvent_WithCancelledContextAndFullChannel_ShouldNotBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// Use buffer size 1 and fill it
	config := Config{WatchChannelBuffer: 1}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)

	// Fill the channel
	w.updateChan <- &WatchUpdate{Kind: WatchKindOpened}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before sending

	errors := []models.ErrorMessage{{ErrorID: "ERR1"}}

	// This should not block - either ctx.Done() or default case handles it
	done := make(chan struct{})
	go func() {
		w.sendEvent(ctx, WatchKindUpdated, errors)
		close(done)
	}()

	select {
	case <-done:
		// Expected - sendEvent returned without blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sendEvent should not block with cancelled context and full channel")
	}
}

// ============================================================================
// pollAndSend Tests
// ============================================================================

func TestPollAndSend_WithNewError_ShouldSendKindOpened(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	pollTime := time.Now()
	// Error with first_seen_at AFTER lastPollTime = NEW error
	newErrorTime := pollTime.Add(1 * time.Second)
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR_NEW", FirstSeenAt: newErrorTime, LastSeenAt: newErrorTime, ClosedAt: nil},
	}

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = pollTime

	ctx := context.Background()
	w.pollAndSend(ctx)

	select {
	case update := <-w.updateChan:
		assert.Equal(t, WatchKindOpened, update.Kind)
		assert.Len(t, update.Errors, 1)
		assert.Equal(t, "ERR_NEW", update.Errors[0].ErrorID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected KIND_OPENED update for new error")
	}
}

func TestPollAndSend_WithUpdatedError_ShouldSendKindUpdated(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	pollTime := time.Now()
	// Error with first_seen_at BEFORE lastPollTime = existing error that was UPDATED
	oldFirstSeen := pollTime.Add(-1 * time.Hour)
	recentLastSeen := pollTime.Add(1 * time.Second)
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR_UPDATED", FirstSeenAt: oldFirstSeen, LastSeenAt: recentLastSeen, ClosedAt: nil},
	}

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = pollTime

	ctx := context.Background()
	w.pollAndSend(ctx)

	select {
	case update := <-w.updateChan:
		assert.Equal(t, WatchKindUpdated, update.Kind)
		assert.Len(t, update.Errors, 1)
		assert.Equal(t, "ERR_UPDATED", update.Errors[0].ErrorID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected KIND_UPDATED update for existing error")
	}
}

func TestPollAndSend_WithClosedError_ShouldSendKindClosed(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	pollTime := time.Now()
	closedAt := pollTime.Add(1 * time.Second)
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR_CLOSED", FirstSeenAt: pollTime.Add(-1 * time.Hour), LastSeenAt: closedAt, ClosedAt: &closedAt},
	}

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = pollTime

	ctx := context.Background()
	w.pollAndSend(ctx)

	select {
	case update := <-w.updateChan:
		assert.Equal(t, WatchKindClosed, update.Kind)
		assert.Len(t, update.Errors, 1)
		assert.Equal(t, "ERR_CLOSED", update.Errors[0].ErrorID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected KIND_CLOSED update")
	}
}

func TestPollAndSend_WithMixedErrorStates_ShouldSendAllThreeKinds(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	pollTime := time.Now()
	closedAt := pollTime.Add(1 * time.Second)

	mockErrors := []models.ErrorMessage{
		// New error (first_seen_at >= lastPollTime)
		{ErrorID: "ERR_NEW", FirstSeenAt: pollTime.Add(1 * time.Second), LastSeenAt: pollTime.Add(1 * time.Second), ClosedAt: nil},
		// Updated error (first_seen_at < lastPollTime, not closed)
		{ErrorID: "ERR_UPDATED", FirstSeenAt: pollTime.Add(-1 * time.Hour), LastSeenAt: pollTime.Add(1 * time.Second), ClosedAt: nil},
		// Closed error
		{ErrorID: "ERR_CLOSED", FirstSeenAt: pollTime.Add(-2 * time.Hour), LastSeenAt: closedAt, ClosedAt: &closedAt},
	}

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = pollTime

	ctx := context.Background()
	w.pollAndSend(ctx)

	// Collect all updates
	var updates []*WatchUpdate
	for range 3 {
		select {
		case update := <-w.updateChan:
			updates = append(updates, update)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected 3 updates, got %d", len(updates))
		}
	}

	// Verify we got all three kinds
	kindCounts := make(map[WatchKind]int)
	for _, u := range updates {
		kindCounts[u.Kind]++
	}
	assert.Equal(t, 1, kindCounts[WatchKindOpened], "expected 1 KIND_OPENED")
	assert.Equal(t, 1, kindCounts[WatchKindUpdated], "expected 1 KIND_UPDATED")
	assert.Equal(t, 1, kindCounts[WatchKindClosed], "expected 1 KIND_CLOSED")
}

func TestPollAndSend_WithErrorAtExactPollTime_ShouldClassifyAsOpened(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	pollTime := time.Now()
	// Error with first_seen_at EQUAL to lastPollTime should be NEW (edge case)
	mockErrors := []models.ErrorMessage{
		{ErrorID: "ERR_EXACT", FirstSeenAt: pollTime, LastSeenAt: pollTime, ClosedAt: nil},
	}

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(mockErrors, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = pollTime

	ctx := context.Background()
	w.pollAndSend(ctx)

	select {
	case update := <-w.updateChan:
		assert.Equal(t, WatchKindOpened, update.Kind)
		assert.Equal(t, "ERR_EXACT", update.Errors[0].ErrorID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected KIND_OPENED for error at exact poll time")
	}
}

func TestPollAndSend_WithNoErrors_ShouldNotSendAnyUpdates(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = time.Now()

	ctx := context.Background()
	w.pollAndSend(ctx)

	select {
	case <-w.updateChan:
		t.Fatal("should not send updates when no errors")
	case <-time.After(50 * time.Millisecond):
		// Expected - no updates
	}
}

func TestPollAndSend_WhenQueryFails_ShouldNotSendUpdates(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return(nil, assert.AnError)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)
	w.lastPollTime = time.Now()

	ctx := context.Background()
	w.pollAndSend(ctx)

	select {
	case <-w.updateChan:
		t.Fatal("should not send updates when query fails")
	case <-time.After(50 * time.Millisecond):
		// Expected - no updates
	}
}

func TestPollAndSend_ShouldUpdateLastPollTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil)

	config := Config{WatchChannelBuffer: 10}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)

	originalTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	w.lastPollTime = originalTime

	ctx := context.Background()
	w.pollAndSend(ctx)

	assert.True(t, w.lastPollTime.After(originalTime))
}

// ============================================================================
// run Tests
// ============================================================================

func TestRun_WithImmediateCancellation_ShouldCloseChannel(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// Even with immediate cancellation, initial poll may or may not happen
	// depending on timing, so we allow any calls
	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil).AnyTimes()

	config := Config{
		WatchPollInterval:  100 * time.Millisecond,
		WatchChannelBuffer: 5,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()

	// run() should complete quickly after context cancellation
	select {
	case <-done:
		// Expected - run completed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("run should complete quickly after context cancellation")
	}

	// Channel should be closed
	_, open := <-w.updateChan
	assert.False(t, open, "channel should be closed")
}

func TestRun_WithCancellationAfterInitialPoll_ShouldCloseChannel(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// Return an error with old first_seen_at so it classifies as UPDATED
	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *models.QueryOptions) ([]models.ErrorMessage, error) {
			now := time.Now()
			return []models.ErrorMessage{
				{ErrorID: "ERR1", FirstSeenAt: now.Add(-1 * time.Hour), LastSeenAt: now, ClosedAt: nil},
			}, nil
		}).AnyTimes()

	config := Config{
		WatchPollInterval:  500 * time.Millisecond, // Long interval so we can control cancellation
		WatchChannelBuffer: 10,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)
	w := newWatcher(svc, 1, nil, config)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()

	// Wait for initial poll to complete (KIND_UPDATED since first_seen_at is old)
	select {
	case update := <-w.updateChan:
		assert.Equal(t, WatchKindUpdated, update.Kind)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected initial poll update")
	}

	// Cancel context
	cancel()

	// run() should complete
	select {
	case <-done:
		// Expected - run completed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("run should complete after context cancellation")
	}

	// Channel should be closed
	_, open := <-w.updateChan
	assert.False(t, open, "channel should be closed")
}

// ============================================================================
// Service.Watch Tests
// ============================================================================

func TestWatch_ShouldReturnChannel(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// Expect initial poll
	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil).AnyTimes()

	config := Config{
		WatchPollInterval:  100 * time.Millisecond,
		WatchChannelBuffer: 10,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updateChan, err := svc.Watch(ctx, 1, nil)

	assert.NoError(t, err)
	assert.NotNil(t, updateChan)
}

func TestWatch_WithOpts_ShouldUseFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// Verify that the filter is applied by checking QueryErrors is called
	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, opts *models.QueryOptions) ([]models.ErrorMessage, error) {
			// Verify filter was applied
			require.NotNil(t, opts.Filter)
			assert.Equal(t, []string{"device-1"}, opts.Filter.DeviceIdentifiers)
			assert.True(t, opts.Filter.IncludeClosed)
			return []models.ErrorMessage{}, nil
		}).AnyTimes()

	config := Config{
		WatchPollInterval:  100 * time.Millisecond,
		WatchChannelBuffer: 10,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := &WatchOptions{
		Filter: &models.QueryFilter{
			DeviceIdentifiers: []string{"device-1"},
		},
	}

	_, err := svc.Watch(ctx, 1, opts)
	assert.NoError(t, err)

	// Give time for initial poll to occur
	time.Sleep(50 * time.Millisecond)
}

func TestWatch_ChannelClosesOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).Return([]models.ErrorMessage{}, nil).AnyTimes()

	config := Config{
		WatchPollInterval:  50 * time.Millisecond,
		WatchChannelBuffer: 10,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	ctx, cancel := context.WithCancel(context.Background())

	updateChan, err := svc.Watch(ctx, 1, nil)
	require.NoError(t, err)

	// Cancel the context
	cancel()

	// Channel should eventually close
	assert.Eventually(t, func() bool {
		select {
		case _, open := <-updateChan:
			return !open
		default:
			return false
		}
	}, 500*time.Millisecond, 10*time.Millisecond, "channel should close when context is cancelled")
}

func TestWatch_SendsCorrectKindBasedOnErrorState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// The mock will be called and we need to return errors that will be
	// classified based on their first_seen_at relative to lastPollTime.
	// Since lastPollTime is set to time.Now() when Watch starts,
	// we return errors with recent first_seen_at to trigger KIND_OPENED.
	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *models.QueryOptions) ([]models.ErrorMessage, error) {
			now := time.Now()
			return []models.ErrorMessage{
				{ErrorID: "ERR_NEW", FirstSeenAt: now.Add(1 * time.Millisecond), LastSeenAt: now.Add(1 * time.Millisecond), ClosedAt: nil},
			}, nil
		}).AnyTimes()

	config := Config{
		WatchPollInterval:  1 * time.Second,
		WatchChannelBuffer: 10,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updateChan, err := svc.Watch(ctx, 1, nil)
	require.NoError(t, err)

	// Should receive KIND_OPENED for new errors
	select {
	case update := <-updateChan:
		assert.Equal(t, WatchKindOpened, update.Kind)
		assert.Len(t, update.Errors, 1)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected KIND_OPENED update for new error")
	}
}

func TestWatch_UsesServiceConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	// Return errors with old first_seen_at so they classify as UPDATED (not new)
	mockStore.EXPECT().QueryErrors(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *models.QueryOptions) ([]models.ErrorMessage, error) {
			now := time.Now()
			return []models.ErrorMessage{
				{ErrorID: "ERR1", FirstSeenAt: now.Add(-1 * time.Hour), LastSeenAt: now, ClosedAt: nil},
			}, nil
		}).AnyTimes()

	// Use custom config values
	config := Config{
		WatchPollInterval:  50 * time.Millisecond, // Short poll interval
		WatchChannelBuffer: 3,
	}
	svc := NewService(context.Background(), config, mockStore, mockTransactor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updateChan, err := svc.Watch(ctx, 1, nil)
	require.NoError(t, err)

	// Verify channel buffer size matches config
	assert.Equal(t, 3, cap(updateChan))

	// Should receive updates (KIND_UPDATED since first_seen_at is old)
	select {
	case update := <-updateChan:
		assert.Equal(t, WatchKindUpdated, update.Kind)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected update")
	}

	// With 50ms poll interval, we should get subsequent updates quickly
	select {
	case update := <-updateChan:
		assert.Equal(t, WatchKindUpdated, update.Kind)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected subsequent update from polling")
	}
}
