package command

import (
	"context"
	"errors"
	"testing"
	"time"

	minerMocks "github.com/proto-at-block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/commandtype"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	minerIfaceMocks "github.com/proto-at-block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/proto-at-block/proto-fleet/server/internal/infrastructure/queue/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestExecutionService_Start(t *testing.T) {
	t.Run("starts when not running and returns true", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		started := false
		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockQueue.EXPECT().Dequeue(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]queue.Message, error) {
			started = true
			return nil, nil
		}).AnyTimes()
		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)

		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:            5,
			MasterPollingInterval: 10 * time.Millisecond,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())

		// Assert
		require.NoError(t, err)

		// Verify the processor started
		assert.Eventually(t, func() bool {
			return started
		}, 100*time.Millisecond, 5*time.Millisecond, "Processor should start")

		assert.True(t, svc.IsRunning())
	})

	t.Run("returns false when already running", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		started := false
		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockQueue.EXPECT().Dequeue(gomock.Any()).DoAndReturn(func(ctx context.Context) ([]queue.Message, error) {
			started = true
			return nil, nil
		}).AnyTimes()
		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)
		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:            5,
			MasterPollingInterval: 10 * time.Millisecond,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Start the service first
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Verify the processor started
		assert.Eventually(t, func() bool {
			return started
		}, 100*time.Millisecond, 5*time.Millisecond, "Processor should start")

		// Act - try to start again
		err = svc.Start(t.Context())

		// Assert
		require.NoError(t, err)
		assert.True(t, svc.IsRunning())
	})
}

func TestQueueProcessorRetries(t *testing.T) {
	t.Run("retries dequeue errors and continues running", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		testError := errors.New("temporary error")

		retryComplete := make(chan struct{})

		mockQueue := mocks.NewMockMessageQueue(ctrl)

		// Track successful retry completion
		retrySucceeded := false

		// First call - returns error
		mockQueue.EXPECT().
			Dequeue(gomock.Any()).
			Return(nil, testError).
			Times(1)

		// Second call - returns error
		mockQueue.EXPECT().
			Dequeue(gomock.Any()).
			Return(nil, testError).
			Times(1)

		// Third call - returns success and signals completion
		mockQueue.EXPECT().
			Dequeue(gomock.Any()).
			DoAndReturn(func(ctx context.Context) ([]queue.Message, error) {
				// Signal that retry sequence completed successfully
				retrySucceeded = true
				close(retryComplete)

				return []queue.Message{}, nil
			}).
			Times(1)

		// Subsequent calls just block
		mockQueue.EXPECT().
			Dequeue(gomock.Any()).
			DoAndReturn(func(ctx context.Context) ([]queue.Message, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}).
			AnyTimes()

		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)
		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:            5,
			MasterPollingInterval: time.Millisecond,
			DequeueRetries:        3,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Assert
		assert.Eventually(t, func() bool {
			return retrySucceeded
		}, 200*time.Millisecond, 10*time.Millisecond, "Service should retry and eventually succeed")

		assert.True(t, svc.IsRunning())
	})

	t.Run("stops running after max retries exhausted", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		testError := errors.New("persistent error")

		mockQueue := mocks.NewMockMessageQueue(ctrl)

		// First three calls fail (initial + 2 retries)
		mockQueue.EXPECT().
			Dequeue(gomock.Any()).
			Return(nil, testError).
			Times(3)

		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)
		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:            5,
			MasterPollingInterval: time.Millisecond,
			DequeueRetries:        2, // Only 2 retries allowed
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Assert - wait for the service to stop running with timeout
		assert.Eventually(t, func() bool {
			return !svc.IsRunning()
		}, 500*time.Millisecond, 10*time.Millisecond, "Service should stop running after max retries are exhausted")
	})
}

func TestWorkerExecuteCommand_PermanentFailureHandling(t *testing.T) {
	t.Run("unimplemented error calls MarkPermanentlyFailed", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		message := queue.Message{
			ID:           1,
			BatchLogUUID: "batch-123",
			CommandType:  commandtype.Reboot,
			DeviceID:     42,
		}

		mockMinerGetter.EXPECT().
			GetMiner(gomock.Any(), int64(42)).
			Return(mockMiner, nil)

		mockMiner.EXPECT().
			Reboot(gomock.Any()).
			Return(fleeterror.NewUnimplementedError("reboot not supported"))

		mockQueue.EXPECT().
			MarkPermanentlyFailed(gomock.Any(), int64(1), gomock.Any()).
			Return(nil)

		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.workerExecuteCommand(t.Context(), commandtype.Reboot, message)

		// Assert
		require.Error(t, err)
		assert.True(t, fleeterror.IsUnimplementedError(err))
	})

	t.Run("retryable error calls MarkFailed not MarkPermanentlyFailed", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		message := queue.Message{
			ID:           2,
			BatchLogUUID: "batch-456",
			CommandType:  commandtype.Reboot,
			DeviceID:     43,
		}

		mockMinerGetter.EXPECT().
			GetMiner(gomock.Any(), int64(43)).
			Return(mockMiner, nil)

		mockMiner.EXPECT().
			Reboot(gomock.Any()).
			Return(fleeterror.NewInternalErrorf("temporary failure"))

		mockQueue.EXPECT().
			MarkFailed(gomock.Any(), int64(2), gomock.Any()).
			Return(nil)

		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.workerExecuteCommand(t.Context(), commandtype.Reboot, message)

		// Assert
		require.Error(t, err)
		assert.False(t, fleeterror.IsUnimplementedError(err))
	})

	t.Run("successful command calls MarkSuccess", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockMinerGetter(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		message := queue.Message{
			ID:           3,
			BatchLogUUID: "batch-789",
			CommandType:  commandtype.Reboot,
			DeviceID:     44,
		}

		mockMinerGetter.EXPECT().
			GetMiner(gomock.Any(), int64(44)).
			Return(mockMiner, nil)

		mockMiner.EXPECT().
			Reboot(gomock.Any()).
			Return(nil)

		mockQueue.EXPECT().
			MarkSuccess(gomock.Any(), int64(3)).
			Return(nil)

		svc := NewExecutionService(t.Context(), &Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.workerExecuteCommand(t.Context(), commandtype.Reboot, message)

		// Assert
		assert.NoError(t, err)
	})
}
