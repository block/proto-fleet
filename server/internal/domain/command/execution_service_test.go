package command

import (
	"context"
	"errors"
	"testing"
	"time"

	minerMocks "github.com/btc-mining/proto-fleet/server/internal/domain/command/mocks"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/queue/mocks"
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
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil)

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
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil)

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
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil)

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
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Assert - wait for the service to stop running with timeout
		assert.Eventually(t, func() bool {
			return !svc.IsRunning()
		}, 500*time.Millisecond, 10*time.Millisecond, "Service should stop running after max retries are exhausted")
	})
}
