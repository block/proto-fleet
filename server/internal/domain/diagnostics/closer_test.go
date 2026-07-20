package diagnostics

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// ============================================================================
// runCloser Tests
// ============================================================================

func TestNewService_DoesNotStartCloser(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	_ = NewService(Config{
		CloserPollInterval: time.Millisecond,
	}, mockStore, mockTransactor)

	// Construction must remain side-effect free so fleetd can decide which
	// process-owned jobs to start for the current runtime mode.
	time.Sleep(10 * time.Millisecond)
}

func TestCloser_WhenCloseStaleErrorsFails_ShouldContinuePolling(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	retried := make(chan struct{})
	gomock.InOrder(
		mockStore.EXPECT().
			CloseStaleErrors(gomock.Any(), 2*time.Minute).
			Return(int64(0), assert.AnError),
		mockStore.EXPECT().
			CloseStaleErrors(gomock.Any(), 2*time.Minute).
			DoAndReturn(func(context.Context, time.Duration) (int64, error) {
				close(retried)
				return 3, nil
			}),
	)

	ctx, cancel := context.WithCancel(t.Context())
	config := Config{
		CloserPollInterval:       10 * time.Millisecond,
		CloserStalenessThreshold: 2 * time.Minute,
	}

	svc := NewService(config, mockStore, mockTransactor)
	require.NoError(t, svc.Start(ctx))

	select {
	case <-retried:
	case <-time.After(time.Second):
		t.Fatal("closer did not poll again after an error")
	}
	cancel()
}

func TestCloser_CanRestartAfterStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	var calls atomic.Int32
	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), 2*time.Minute).
		DoAndReturn(func(context.Context, time.Duration) (int64, error) {
			calls.Add(1)
			return 0, nil
		}).
		AnyTimes()

	svc := NewService(Config{
		CloserPollInterval:       time.Millisecond,
		CloserStalenessThreshold: 2 * time.Minute,
	}, mockStore, mockTransactor)

	require.NoError(t, svc.Start(t.Context()))
	require.Eventually(t, func() bool {
		return calls.Load() > 0
	}, 100*time.Millisecond, time.Millisecond)
	require.NoError(t, svc.Stop(t.Context()))

	firstRunCalls := calls.Load()
	require.NoError(t, svc.Start(t.Context()))
	require.Eventually(t, func() bool {
		return calls.Load() > firstRunCalls
	}, 100*time.Millisecond, time.Millisecond)
	require.NoError(t, svc.Stop(t.Context()))
}

// ============================================================================
// closeStaleErrors Tests
// ============================================================================

func TestCloseStaleErrors_WithZeroClosed_ShouldNotLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)

	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), 2*time.Minute).
		Return(int64(0), nil)

	svc := &Service{
		config:     Config{},
		errorStore: mockStore,
	}

	svc.closeStaleErrors(t.Context(), 2*time.Minute)
}

func TestCloseStaleErrors_WithErrorsClosed_ShouldLogCount(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)

	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), 2*time.Minute).
		Return(int64(10), nil)

	svc := &Service{
		config:     Config{},
		errorStore: mockStore,
	}

	svc.closeStaleErrors(t.Context(), 2*time.Minute)
}
