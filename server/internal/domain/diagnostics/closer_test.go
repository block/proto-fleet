package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
)

// ============================================================================
// runCloser Tests
// ============================================================================

func TestCloser_WithValidConfig_ShouldCallCloseStaleErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), 2*time.Minute).
		Return(int64(5), nil).
		MinTimes(1)

	ctx, cancel := context.WithCancel(t.Context())
	config := Config{
		CloserPollInterval:       10 * time.Millisecond,
		CloserStalenessThreshold: 2 * time.Minute,
	}

	_ = NewService(ctx, config, mockStore, mockTransactor)

	time.Sleep(50 * time.Millisecond)
	cancel()
}

func TestCloser_WithZeroConfig_ShouldUseDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), defaultCloserStalenessThreshold).
		Return(int64(0), nil).
		AnyTimes()

	ctx, cancel := context.WithCancel(t.Context())
	config := Config{}

	_ = NewService(ctx, config, mockStore, mockTransactor)

	time.Sleep(10 * time.Millisecond)
	cancel()
}

func TestCloser_WhenContextCancelled_ShouldStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), gomock.Any()).
		Return(int64(0), nil).
		AnyTimes()

	ctx, cancel := context.WithCancel(t.Context())
	config := Config{
		CloserPollInterval:       10 * time.Millisecond,
		CloserStalenessThreshold: 2 * time.Minute,
	}

	_ = NewService(ctx, config, mockStore, mockTransactor)

	cancel()

	time.Sleep(50 * time.Millisecond)
}

func TestCloser_WhenCloseStaleErrorsFails_ShouldContinuePolling(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	gomock.InOrder(
		mockStore.EXPECT().
			CloseStaleErrors(gomock.Any(), 2*time.Minute).
			Return(int64(0), assert.AnError),
		mockStore.EXPECT().
			CloseStaleErrors(gomock.Any(), 2*time.Minute).
			Return(int64(3), nil).
			AnyTimes(),
	)

	ctx, cancel := context.WithCancel(t.Context())
	config := Config{
		CloserPollInterval:       10 * time.Millisecond,
		CloserStalenessThreshold: 2 * time.Minute,
	}

	_ = NewService(ctx, config, mockStore, mockTransactor)

	time.Sleep(50 * time.Millisecond)
	cancel()
}

func TestCloser_WithNegativeConfig_ShouldUseDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := storeMocks.NewMockErrorStore(ctrl)
	mockTransactor := storeMocks.NewMockTransactor(ctrl)

	mockStore.EXPECT().
		CloseStaleErrors(gomock.Any(), defaultCloserStalenessThreshold).
		Return(int64(0), nil).
		AnyTimes()

	ctx, cancel := context.WithCancel(t.Context())
	config := Config{
		CloserPollInterval:       -10 * time.Second,
		CloserStalenessThreshold: -2 * time.Minute,
	}

	_ = NewService(ctx, config, mockStore, mockTransactor)

	time.Sleep(10 * time.Millisecond)
	cancel()
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
