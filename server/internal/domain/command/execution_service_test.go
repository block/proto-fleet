package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register the pgx SQL driver for sql.Open

	minerMocks "github.com/block/proto-fleet/server/internal/domain/command/mocks"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	minerIfaceMocks "github.com/block/proto-fleet/server/internal/domain/miner/interfaces/mocks"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	storeMocks "github.com/block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	tmodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue/mocks"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type firmwareStatusMiner struct {
	minerInterfaces.Miner
	minerInterfaces.FirmwareUpdateStatusProvider
}

func TestExecutionService_Start(t *testing.T) {
	t.Run("rejects a canceled activation", func(t *testing.T) {
		svc := &ExecutionService{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.ErrorIs(t, svc.Start(ctx), context.Canceled)
		require.False(t, svc.IsRunning())
	})

	t.Run("is idempotent while running", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		var started atomic.Bool
		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockQueue.EXPECT().Dequeue(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ int32) ([]queue.Message, error) {
			started.Store(true)
			return nil, nil
		}).AnyTimes()
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		svc := NewExecutionService(&Config{
			MaxWorkers:            5,
			MasterPollingInterval: 10 * time.Millisecond,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Start the service first
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Verify the processor started
		assert.Eventually(t, started.Load, 100*time.Millisecond, 5*time.Millisecond, "Processor should start")

		// Act - try to start again
		err = svc.Start(t.Context())

		// Assert
		require.NoError(t, err)
		assert.True(t, svc.IsRunning())
		require.NoError(t, svc.Stop(context.Background()))
	})

	t.Run("rejects restart while a canceled activation is draining", func(t *testing.T) {
		run := newExecutionRun(t.Context())
		svc := &ExecutionService{run: run}
		svc.beginStop(run)

		err := svc.Start(t.Context())

		require.EqualError(t, err, "command execution service activation is still draining")
		run.cancelWork()
	})

	t.Run("activation cancellation stops promptly and allows restart", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)

		var dequeues atomic.Int32
		mockQueue.EXPECT().Dequeue(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, int32) ([]queue.Message, error) {
			dequeues.Add(1)
			return nil, nil
		}).AnyTimes()

		svc := NewExecutionService(&Config{
			MaxWorkers:            1,
			MasterPollingInterval: time.Hour,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		activationCtx, cancelActivation := context.WithCancel(t.Context())
		require.NoError(t, svc.Start(activationCtx))
		require.Eventually(t, func() bool { return dequeues.Load() >= 1 }, 100*time.Millisecond, time.Millisecond)
		cancelActivation()
		require.Eventually(t, func() bool {
			svc.lifecycleMu.Lock()
			defer svc.lifecycleMu.Unlock()
			return svc.run == nil
		}, 100*time.Millisecond, time.Millisecond)
		require.False(t, svc.IsRunning())

		stop := func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			require.NoError(t, svc.Stop(stopCtx))
		}
		require.NoError(t, svc.Start(t.Context()))
		require.Eventually(t, func() bool { return dequeues.Load() >= 2 }, 100*time.Millisecond, time.Millisecond)
		stop()
	})
}

func TestExecutionService_StopTimeoutRetainsActivationUntilWorkerDrains(t *testing.T) {
	svc := NewExecutionService(&Config{
		MaxWorkers:            1,
		MasterPollingInterval: time.Hour,
	}, nil, nil, nil, nil, nil, nil, nil, nil)

	run := newExecutionRun(context.Background())
	svc.run = run
	workerStarted := make(chan struct{})
	workerCanceled := make(chan struct{})
	releaseWorker := make(chan struct{})
	run.wg.Go(func() {
		close(workerStarted)
		<-run.workCtx.Done()
		close(workerCanceled)
		<-releaseWorker
	})
	go svc.finishRun(run)
	<-workerStarted

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	require.ErrorIs(t, svc.Stop(stopCtx), context.DeadlineExceeded)
	require.False(t, svc.IsRunning())
	<-workerCanceled
	cancelStop()
	require.Error(t, svc.Start(t.Context()))

	close(releaseWorker)
	require.NoError(t, svc.Stop(context.Background()))
}

func TestExecutionService_StopDrainsAdmittedWorker(t *testing.T) {
	svc := NewExecutionService(&Config{MaxWorkers: 1}, nil, nil, nil, nil, nil, nil, nil, nil)
	run := newExecutionRun(t.Context())
	svc.run = run

	workerStarted := make(chan struct{})
	releaseWorker := make(chan struct{})
	workerCanceled := make(chan struct{})
	run.wg.Go(func() {
		close(workerStarted)
		select {
		case <-run.workCtx.Done():
			close(workerCanceled)
		case <-releaseWorker:
		}
	})
	go svc.finishRun(run)
	<-workerStarted

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- svc.Stop(context.Background())
	}()
	require.Eventually(t, func() bool { return !svc.IsRunning() }, 100*time.Millisecond, time.Millisecond)

	select {
	case <-workerCanceled:
		t.Fatal("normal Stop canceled admitted worker")
	default:
	}
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before admitted worker drained: %v", err)
	default:
	}

	close(releaseWorker)
	require.NoError(t, <-stopDone)
}

func TestExecutionService_StopWaitsForAdmittedEnqueue(t *testing.T) {
	svc := NewExecutionService(&Config{MaxWorkers: 1}, nil, nil, nil, nil, nil, nil, nil, nil)
	run := newExecutionRun(t.Context())
	svc.run = run
	run.wg.Add(1) // Matches the processor's lifetime while admission is open.
	go svc.finishRun(run)

	enqueueStarted := make(chan struct{})
	releaseEnqueue := make(chan struct{})
	enqueueDone := make(chan error, 1)
	go func() {
		enqueueDone <- svc.withAdmission(t.Context(), func(ctx context.Context) error {
			close(enqueueStarted)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseEnqueue:
				return nil
			}
		})
	}()
	<-enqueueStarted
	run.wg.Done()

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- svc.Stop(context.Background())
	}()
	require.Eventually(t, func() bool { return !svc.IsRunning() }, 100*time.Millisecond, time.Millisecond)
	require.ErrorIs(t, svc.withAdmission(t.Context(), func(context.Context) error {
		return nil
	}), errExecutionNotAccepting)

	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before admitted enqueue drained: %v", err)
	default:
	}

	close(releaseEnqueue)
	require.NoError(t, <-enqueueDone)
	require.NoError(t, <-stopDone)
}

func TestExecutionService_DequeueIsLimitedToAvailableWorkers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockQueue := mocks.NewMockMessageQueue(ctrl)
	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	dequeued := make(chan struct{})
	mockQueue.EXPECT().Dequeue(gomock.Any(), int32(1)).DoAndReturn(func(context.Context, int32) ([]queue.Message, error) {
		close(dequeued)
		return nil, nil
	})

	svc := NewExecutionService(&Config{
		MaxWorkers:            1,
		MasterPollingInterval: time.Hour,
	}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	run := newExecutionRun(ctx)
	defer run.cancelWork()
	done := make(chan error, 1)
	go func() { done <- svc.startQueueProcessorThread(run) }()
	<-dequeued
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
}

func TestQueueProcessorRetries(t *testing.T) {
	t.Run("treats context cancellation during dequeue as shutdown", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		ctx, cancel := context.WithCancel(context.Background())
		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ int32) ([]queue.Message, error) {
				cancel()
				return nil, fleeterror.NewInternalError("error opening tx: context canceled")
			})

		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		svc := NewExecutionService(&Config{
			MaxWorkers:            5,
			MasterPollingInterval: time.Millisecond,
			DequeueRetries:        0,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		run := newExecutionRun(ctx)
		defer run.cancelWork()
		err := svc.startQueueProcessorThread(run)

		// Assert
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("retries dequeue errors and continues running", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		testError := errors.New("temporary error")

		mockQueue := mocks.NewMockMessageQueue(ctrl)

		// Track successful retry completion
		var retrySucceeded atomic.Bool

		// First call - returns error
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			Return(nil, testError).
			Times(1)

		// Second call - returns error
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			Return(nil, testError).
			Times(1)

		// Third call - returns success and signals completion
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ int32) ([]queue.Message, error) {
				retrySucceeded.Store(true)
				return []queue.Message{}, nil
			}).
			Times(1)

		// Subsequent calls just block
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ int32) ([]queue.Message, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}).
			AnyTimes()

		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		svc := NewExecutionService(&Config{
			MaxWorkers:            5,
			MasterPollingInterval: time.Millisecond,
			DequeueRetries:        3,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		// Assert
		assert.Eventually(t, retrySucceeded.Load, 200*time.Millisecond, 10*time.Millisecond, "Service should retry and eventually succeed")

		assert.True(t, svc.IsRunning())
		require.NoError(t, svc.Stop(context.Background()))
	})

	t.Run("keeps running after max retries exhausted", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		testError := errors.New("persistent error")

		mockQueue := mocks.NewMockMessageQueue(ctrl)

		// First three calls fail (initial + 2 retries)
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			Return(nil, testError).
			Times(3)
		retrying := make(chan struct{})
		mockQueue.EXPECT().
			Dequeue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ int32) ([]queue.Message, error) {
				close(retrying)
				<-ctx.Done()
				return nil, ctx.Err()
			})

		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		svc := NewExecutionService(&Config{
			MaxWorkers:            5,
			MasterPollingInterval: time.Millisecond,
			DequeueRetries:        2, // Only 2 retries allowed
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		err := svc.Start(t.Context())
		require.NoError(t, err)

		select {
		case <-retrying:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("service did not resume dequeuing after exhausting retries")
		}
		assert.True(t, svc.IsRunning())
		require.NoError(t, svc.Stop(context.Background()))
	})
}

func TestExecuteCommandOnDevice(t *testing.T) {
	t.Run("unimplemented error is returned as-is", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
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

		mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
		mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

		mockMiner.EXPECT().
			Reboot(gomock.Any()).
			Return(fleeterror.NewUnimplementedError("reboot not supported"))

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.Reboot, message)

		// Assert
		require.Error(t, err)
		assert.True(t, fleeterror.IsUnimplementedError(err))
	})

	t.Run("retryable error is returned as-is", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
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
		mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
		mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

		mockMiner.EXPECT().
			Reboot(gomock.Any()).
			Return(fleeterror.NewInternalErrorf("temporary failure"))

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.Reboot, message)

		// Assert
		require.Error(t, err)
		assert.False(t, fleeterror.IsUnimplementedError(err))
	})

	t.Run("successful command returns nil", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
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
		mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
		mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

		mockMiner.EXPECT().
			Reboot(gomock.Any()).
			Return(nil)

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.Reboot, message)

		// Assert
		assert.NoError(t, err)
	})

	t.Run("GetMiner failure returns error and falls back to message OrgID", func(t *testing.T) {
		// Arrange
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)

		message := queue.Message{
			ID:           4,
			BatchLogUUID: "batch-101",
			CommandType:  commandtype.Reboot,
			DeviceID:     45,
			OrgID:        77,
		}

		mockMinerGetter.EXPECT().
			GetMiner(gomock.Any(), int64(45)).
			Return(nil, errors.New("device not found"))

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		// Act
		orgID, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.Reboot, message)

		// Assert
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error getting miner connection info")
		assert.Equal(t, int64(77), orgID, "executeCommandOnDevice should return message.OrgID when miner construction fails")
	})

	t.Run("Curtail dispatches with payload-derived level", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		payload, err := json.Marshal(dto.CurtailPayload{Level: int32(sdk.CurtailLevelFull)})
		require.NoError(t, err)

		message := queue.Message{
			ID:          5,
			CommandType: commandtype.Curtail,
			DeviceID:    50,
			Payload:     payload,
		}

		mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
		mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
		mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(50)).Return(mockMiner, nil)
		mockMiner.EXPECT().
			Curtail(gomock.Any(), sdk.CurtailRequest{Level: sdk.CurtailLevelFull}).
			Return(nil)

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.Curtail, message)
		require.NoError(t, err)
	})

	t.Run("Curtail surfaces unmarshal failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		message := queue.Message{
			ID:          6,
			CommandType: commandtype.Curtail,
			DeviceID:    51,
			Payload:     []byte("not-json"),
		}

		mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
		mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
		mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(51)).Return(mockMiner, nil)
		// Curtail must NOT be called when payload unmarshal fails.

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.Curtail, message)
		require.Error(t, err)
		assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
		assert.Contains(t, err.Error(), "unmarshalling curtail payload")
	})

	// Both bounds of the level range — covers a `>` -> `>=` mutation on the
	// upper arm and a `<` -> `<=` mutation on the lower arm.
	for _, level := range []int32{0, 3} {
		t.Run(fmt.Sprintf("Curtail rejects out-of-range level=%d as FailedPrecondition", level), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockQueue := mocks.NewMockMessageQueue(ctrl)
			mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
			mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

			payload, err := json.Marshal(dto.CurtailPayload{Level: level})
			require.NoError(t, err)

			message := queue.Message{
				ID:          8,
				CommandType: commandtype.Curtail,
				DeviceID:    53,
				Payload:     payload,
			}

			mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
			mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
			mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(53)).Return(mockMiner, nil)
			// No mockMiner.EXPECT().Curtail(...) — bounds check must short-circuit.

			svc := NewExecutionService(&Config{
				MaxWorkers:             5,
				MasterPollingInterval:  10 * time.Millisecond,
				WorkerExecutionTimeout: 5 * time.Second,
			}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

			_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.Curtail, message)
			require.Error(t, err)
			assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
			assert.Contains(t, err.Error(), "invalid curtail level")
		})
	}

	t.Run("Uncurtail dispatches with empty request", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockQueue := mocks.NewMockMessageQueue(ctrl)
		mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		message := queue.Message{
			ID:          7,
			CommandType: commandtype.Uncurtail,
			DeviceID:    52,
		}

		mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
		mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
		mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(52)).Return(mockMiner, nil)
		mockMiner.EXPECT().
			Uncurtail(gomock.Any(), sdk.UncurtailRequest{}).
			Return(nil)

		svc := NewExecutionService(&Config{
			MaxWorkers:             5,
			MasterPollingInterval:  10 * time.Millisecond,
			WorkerExecutionTimeout: 5 * time.Second,
		}, nil, mockQueue, nil, nil, mockMinerGetter, nil, nil, nil)

		_, _, err := svc.executeCommandOnDevice(t.Context(), commandtype.Uncurtail, message)
		require.NoError(t, err)
	})
}

func TestFirmwareUpdateAutoReboot(t *testing.T) {
	t.Run("verified install status is reboot ready", func(t *testing.T) {
		for _, state := range []string{"installed", "success", "confirming"} {
			t.Run(state, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				defer ctrl.Finish()

				mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)
				mockProvider := minerIfaceMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
				devID := tmodels.DeviceIdentifier("device-123")

				mockDeviceStore.EXPECT().
					UpsertDeviceStatus(gomock.Any(), devID, models.MinerStatusUpdating, "").
					Return(nil)
				mockDeviceStore.EXPECT().
					UpsertDeviceStatus(gomock.Any(), devID, models.MinerStatusRebootRequired, "").
					Return(nil)

				svc := &ExecutionService{deviceStore: mockDeviceStore}

				installVerified, err := svc.doPollFirmwareInstall(
					t.Context(),
					mockProvider,
					devID,
					42,
					&sdk.FirmwareUpdateStatus{State: state},
					nil,
				)

				require.NoError(t, err)
				assert.True(t, installVerified)
			})
		}
	})

	t.Run("successful automatic reboot clears firmware status", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
		devID := tmodels.DeviceIdentifier("device-123")

		mockMiner.EXPECT().Reboot(gomock.Any()).Return(nil)
		mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-123"))
		mockDeviceStore.EXPECT().
			GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []tmodels.DeviceIdentifier{devID}).
			Return(map[tmodels.DeviceIdentifier]models.MinerStatus{devID: models.MinerStatusRebootRequired}, nil)
		mockDeviceStore.EXPECT().
			UpsertDeviceStatus(gomock.Any(), devID, models.MinerStatusActive, "").
			Return(nil)

		svc := &ExecutionService{deviceStore: mockDeviceStore}

		err := svc.rebootAfterFirmwareInstall(t.Context(), mockMiner, 42)

		require.NoError(t, err)
	})

	t.Run("automatic reboot failure is permanent and leaves reboot required", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)
		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

		mockMiner.EXPECT().Reboot(gomock.Any()).Return(errors.New("connection refused"))

		svc := &ExecutionService{deviceStore: mockDeviceStore}

		err := svc.rebootAfterFirmwareInstall(t.Context(), mockMiner, 42)

		require.Error(t, err)
		assert.True(t, fleeterror.IsFailedPreconditionError(err), "expected FailedPrecondition, got %v", err)
		assert.Contains(t, err.Error(), "automatic reboot failed")
	})

	t.Run("miner without firmware status provider is reboot ready after upload", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
		svc := &ExecutionService{}

		installVerified, err := svc.pollFirmwareInstallStatus(t.Context(), mockMiner, 42)

		require.NoError(t, err)
		assert.True(t, installVerified)
	})

	t.Run("status provider with nil status is reboot ready after upload", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
		mockProvider := minerIfaceMocks.NewMockFirmwareUpdateStatusProvider(ctrl)
		mockProvider.EXPECT().GetFirmwareUpdateStatus(gomock.Any()).Return(nil, nil)

		svc := &ExecutionService{}
		miner := firmwareStatusMiner{Miner: mockMiner, FirmwareUpdateStatusProvider: mockProvider}

		installVerified, err := svc.pollFirmwareInstallStatus(t.Context(), miner, 42)

		require.NoError(t, err)
		assert.True(t, installVerified)
	})
}

func TestExecuteCommandOnDevice_FirmwareUpdatePassesFileMetadata(t *testing.T) {
	t.Chdir(t.TempDir())
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filesService, err := files.NewService(files.Config{})
	require.NoError(t, err)
	content := "firmware image"
	fileID, err := filesService.SaveFirmwareFile("update.swu", strings.NewReader(content))
	require.NoError(t, err)

	mockQueue := mocks.NewMockMessageQueue(ctrl)
	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.FirmwareUpdatePayload{FirmwareFileID: fileID})
	require.NoError(t, err)
	message := queue.Message{
		ID:          9,
		CommandType: commandtype.FirmwareUpdate,
		DeviceID:    42,
		Payload:     payloadBytes,
	}

	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMinerGetter.EXPECT().GetMiner(gomock.Any(), int64(42)).Return(mockMiner, nil)
	mockMiner.EXPECT().FirmwareUpdate(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, firmware sdk.FirmwareFile) error {
			assert.Equal(t, fileID, firmware.ID)
			assert.Equal(t, "update.swu", firmware.Filename)
			assert.Equal(t, int64(len(content)), firmware.Size)
			assert.NotEmpty(t, firmware.SHA256)
			assert.NotEmpty(t, firmware.FilePath)
			data, err := io.ReadAll(firmware.Reader)
			require.NoError(t, err)
			assert.Equal(t, content, string(data))
			return nil
		})
	mockMiner.EXPECT().Reboot(gomock.Any()).Return(nil)
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-123"))
	mockDeviceStore.EXPECT().
		GetDeviceStatusForDeviceIdentifiers(gomock.Any(), []tmodels.DeviceIdentifier{"device-123"}).
		Return(map[tmodels.DeviceIdentifier]models.MinerStatus{}, nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, mockQueue, nil, nil, mockMinerGetter, mockDeviceStore, nil, filesService)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.FirmwareUpdate, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_UsesStoredWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority:        0,
			URL:             "stratum+tcp://pool1.example.com:3333",
			Username:        "wallet",
			AppendMinerName: true,
		},
		Backup1Pool: &dto.MiningPool{
			Priority:        1,
			URL:             "stratum+tcp://pool2.example.com:3333",
			Username:        "wallet-backup",
			AppendMinerName: true,
		},
		Backup2Pool: &dto.MiningPool{
			Priority: 2,
			URL:      "stratum+tcp://pool3.example.com:3333",
			Username: "existing.raw.username",
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           10,
		BatchLogUUID: "batch-pools-123",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     42,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(42)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().GetOrgID().Return(int64(7)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-123")).AnyTimes()
	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return(nil, errors.New("miner read failed"))

	mockDeviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(7), []string{"device-123"}, false).
		Return([]stores.DeviceRenameProperties{
			{
				DeviceIdentifier: "device-123",
				WorkerName:       "rig-01",
			},
		}, nil)

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.rig-01", payload.DefaultPool.Username)
			require.NotNil(t, payload.Backup1Pool)
			assert.Equal(t, "wallet-backup.rig-01", payload.Backup1Pool.Username)
			require.NotNil(t, payload.Backup2Pool)
			assert.Equal(t, "existing.raw.username", payload.Backup2Pool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_UsesStoredWorkerNameAfterLookupTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority:        0,
			URL:             "stratum+tcp://pool1.example.com:3333",
			Username:        "wallet",
			AppendMinerName: true,
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           16,
		BatchLogUUID: "batch-pools-timeout-fallback",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     48,
		Payload:      payloadBytes,
	}

	commandCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(48)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().GetOrgID().Return(int64(10)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-timeout")).AnyTimes()
	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]minerInterfaces.MinerConfiguredPool, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})

	mockDeviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(10), []string{"device-timeout"}, false).
		DoAndReturn(func(ctx context.Context, orgID int64, deviceIdentifiers []string, includeTelemetry bool) ([]stores.DeviceRenameProperties, error) {
			require.NoError(t, ctx.Err())
			return []stores.DeviceRenameProperties{
				{
					DeviceIdentifier: "device-timeout",
					WorkerName:       "rig-timeout",
				},
			}, nil
		})

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			require.NoError(t, ctx.Err())
			assert.Equal(t, "wallet.rig-timeout", payload.DefaultPool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(commandCtx, commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_PrefersCurrentPrimaryPoolWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority:        0,
			URL:             "stratum+tcp://pool1.example.com:3333",
			Username:        "wallet",
			AppendMinerName: true,
		},
		Backup1Pool: &dto.MiningPool{
			Priority:        1,
			URL:             "stratum+tcp://pool2.example.com:3333",
			Username:        "wallet-backup",
			AppendMinerName: true,
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           15,
		BatchLogUUID: "batch-pools-live-worker",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     47,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(47)).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{
				Priority: 1,
				URL:      "stratum+tcp://backup.example.com:3333",
				Username: "wallet.backup-worker",
			},
			{
				Priority: 0,
				URL:      "stratum+tcp://primary.example.com:3333",
				Username: "wallet.live-worker",
			},
		}, nil)

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.live-worker", payload.DefaultPool.Username)
			require.NotNil(t, payload.Backup1Pool)
			assert.Equal(t, "wallet-backup.live-worker", payload.Backup1Pool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, nil, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_FallsBackToStoredMacAddress(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority:        0,
			URL:             "stratum+tcp://pool1.example.com:3333",
			Username:        "wallet",
			AppendMinerName: true,
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           13,
		BatchLogUUID: "batch-pools-fallback",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     45,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(45)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().GetOrgID().Return(int64(8)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-456")).AnyTimes()
	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return(nil, errors.New("miner read failed"))

	mockDeviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(8), []string{"device-456"}, false).
		Return([]stores.DeviceRenameProperties{
			{
				DeviceIdentifier: "device-456",
				MacAddress:       "AA:BB:CC:DD:1A:2B",
			},
		}, nil)

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.AA:BB:CC:DD:1A:2B", payload.DefaultPool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_LeavesUsernameUnchangedWhenWorkerSuffixUnavailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority:        0,
			URL:             "stratum+tcp://pool1.example.com:3333",
			Username:        "wallet",
			AppendMinerName: true,
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           14,
		BatchLogUUID: "batch-pools-no-suffix",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     46,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(46)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().GetOrgID().Return(int64(9)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-789")).AnyTimes()
	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{
				Priority: 0,
				URL:      "stratum+tcp://pool1.example.com:3333",
				Username: "wallet",
			},
		}, nil)

	mockDeviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(9), []string{"device-789"}, false).
		Return([]stores.DeviceRenameProperties{
			{
				DeviceIdentifier: "device-789",
			},
		}, nil)

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet", payload.DefaultPool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_LeavesRawPoolUsernamesUnchanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority: 0,
			URL:      "stratum+tcp://pool1.example.com:3333",
			Username: "wallet.existing-worker",
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           11,
		BatchLogUUID: "batch-pools-456",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     43,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(43)).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.existing-worker", payload.DefaultPool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, nil, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_PreservesLegacyDottedFleetUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		DefaultPool: dto.MiningPool{
			Priority:        0,
			URL:             "stratum+tcp://pool1.example.com:3333",
			Username:        "wallet.worker-a",
			AppendMinerName: true,
		},
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           12,
		BatchLogUUID: "batch-pools-legacy",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     44,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(44)).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.worker-a", payload.DefaultPool.Username)
			return nil
		})

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReappliesCurrentPoolsWithStoredWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "new-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           17,
		BatchLogUUID: "batch-pools-reapply",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     49,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(49)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{Priority: 1, URL: "stratum+tcp://backup.example.com:3333", Username: "wallet-backup.old-worker"},
			{Priority: 0, URL: "stratum+tcp://primary.example.com:3333", Username: "wallet.old-worker"},
			{Priority: 2, URL: "stratum+tcp://custom.example.com:3333", Username: "custom.username"},
		}, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(11)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-reapply")).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.new-worker", payload.DefaultPool.Username)
			require.NotNil(t, payload.Backup1Pool)
			assert.Equal(t, "wallet-backup.new-worker", payload.Backup1Pool.Username)
			require.NotNil(t, payload.Backup2Pool)
			assert.Equal(t, "custom.new-worker", payload.Backup2Pool.Username)
			return nil
		})
	mockDeviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-reapply"), "new-worker").
		Return(nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReapplyUsesDesiredWorkerNameFromPayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "payload-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           117,
		BatchLogUUID: "batch-pools-reapply-payload",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     149,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(149)).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()

	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{Priority: 0, URL: "stratum+tcp://primary.example.com:3333", Username: "wallet.old-worker"},
		}, nil)
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-reapply-payload")).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.payload-worker", payload.DefaultPool.Username)
			return nil
		})
	mockDeviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-reapply-payload"), "payload-worker").
		Return(nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReapplyAppendsStoredWorkerNameWhenCurrentUsernameHasNoSuffix(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "new-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           18,
		BatchLogUUID: "batch-pools-reapply-append",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     50,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(50)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{Priority: 0, URL: "stratum+tcp://primary.example.com:3333", Username: "wallet"},
		}, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(12)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-reapply-append")).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.new-worker", payload.DefaultPool.Username)
			assert.Nil(t, payload.Backup1Pool)
			return nil
		})
	mockDeviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-reapply-append"), "new-worker").
		Return(nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReapplyReplacesEntireDottedWorkerSuffix(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "new-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           20,
		BatchLogUUID: "batch-pools-reapply-dotted-worker",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     52,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(52)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{Priority: 0, URL: "stratum+tcp://primary.example.com:3333", Username: "wallet.primary.worker"},
		}, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(13)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-reapply-dotted-worker")).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.new-worker", payload.DefaultPool.Username)
			return nil
		})
	mockDeviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-reapply-dotted-worker"), "new-worker").
		Return(nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReapplyNormalizesAllPoolsToStoredWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "new-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           21,
		BatchLogUUID: "batch-pools-reapply-normalize-all",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     53,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(53)).
		Return(mockMiner, nil)

	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return([]minerInterfaces.MinerConfiguredPool{
			{Priority: 0, URL: "stratum+tcp://primary.example.com:3333", Username: "wallet"},
			{Priority: 1, URL: "stratum+tcp://backup.example.com:3333", Username: "wallet-backup.old-worker"},
			{Priority: 2, URL: "stratum+tcp://custom.example.com:3333", Username: "custom.username"},
		}, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(14)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-reapply-normalize-all")).AnyTimes()

	mockMiner.EXPECT().
		UpdateMiningPools(gomock.Any(), gomock.AssignableToTypeOf(dto.UpdateMiningPoolsPayload{})).
		DoAndReturn(func(ctx context.Context, payload dto.UpdateMiningPoolsPayload) error {
			assert.Equal(t, "wallet.new-worker", payload.DefaultPool.Username)
			require.NotNil(t, payload.Backup1Pool)
			assert.Equal(t, "wallet-backup.new-worker", payload.Backup1Pool.Username)
			require.NotNil(t, payload.Backup2Pool)
			assert.Equal(t, "custom.new-worker", payload.Backup2Pool.Username)
			return nil
		})
	mockDeviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-reapply-normalize-all"), "new-worker").
		Return(nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_PersistsWorkerNameWhenNoCurrentPoolsExist(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "new-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           19,
		BatchLogUUID: "batch-pools-reapply-empty",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     51,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(51)).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return(nil, nil)
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("device-reapply-empty")).AnyTimes()
	mockDeviceStore.EXPECT().
		UpdateWorkerName(gomock.Any(), models.DeviceIdentifier("device-reapply-empty"), "new-worker").
		Return(nil)

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.NoError(t, err)
}

func TestExecuteCommandOnDevice_UpdateMiningPools_ReapplyPreservesGetPoolsErrorClass(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)
	mockDeviceStore := storeMocks.NewMockDeviceStore(ctrl)

	payloadBytes, err := json.Marshal(dto.UpdateMiningPoolsPayload{
		ReapplyCurrentPoolsWithStoredWorkerName: true,
		DesiredWorkerName:                       "new-worker",
	})
	require.NoError(t, err)

	message := queue.Message{
		ID:           22,
		BatchLogUUID: "batch-pools-reapply-get-pools-failure",
		CommandType:  commandtype.UpdateMiningPools,
		DeviceID:     54,
		Payload:      payloadBytes,
	}

	mockMinerGetter.EXPECT().
		GetMiner(gomock.Any(), int64(54)).
		Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().
		GetMiningPools(gomock.Any()).
		Return(nil, fleeterror.NewUnimplementedErrorf("get pools not supported"))

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, nil, nil, nil, nil, mockMinerGetter, mockDeviceStore, nil, nil)

	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMiningPools, message)
	require.Error(t, err)
	assert.True(t, fleeterror.IsUnimplementedError(err))
}

func TestStoredMinerWorkerName(t *testing.T) {
	t.Run("prefers stored worker name", func(t *testing.T) {
		assert.Equal(t, "rig-01", storedMinerWorkerName(stores.DeviceRenameProperties{
			WorkerName: "  rig-01  ",
			MacAddress: "AA:BB:CC:DD:1A:2B",
		}))
	})

	t.Run("falls back to stored mac address", func(t *testing.T) {
		assert.Equal(t, "AA:BB:CC:DD:1A:2B", storedMinerWorkerName(stores.DeviceRenameProperties{
			MacAddress: "AA:BB:CC:DD:1A:2B",
		}))
	})

	t.Run("returns empty string when nothing is stored", func(t *testing.T) {
		assert.Equal(t, "", storedMinerWorkerName(stores.DeviceRenameProperties{}))
	})
}

func TestConfiguredMinerWorkerName(t *testing.T) {
	t.Run("uses the primary pool only", func(t *testing.T) {
		assert.Equal(t, "primary-worker", configuredMinerWorkerName([]minerInterfaces.MinerConfiguredPool{
			{Priority: 1, Username: "wallet.backup-worker"},
			{Priority: 0, Username: "wallet.primary-worker"},
		}))
	})

	t.Run("returns empty when the primary pool has no suffix", func(t *testing.T) {
		assert.Empty(t, configuredMinerWorkerName([]minerInterfaces.MinerConfiguredPool{
			{Priority: 0, Username: "wallet"},
			{Priority: 1, Username: "wallet.backup-worker"},
		}))
	})

	t.Run("preserves dots inside worker suffix", func(t *testing.T) {
		assert.Equal(t, "primary.worker", configuredMinerWorkerName([]minerInterfaces.MinerConfiguredPool{
			{Priority: 0, Username: "wallet.primary.worker"},
			{Priority: 1, Username: "wallet.backup-worker"},
		}))
	})
}

func TestShouldAppendMinerNameToUsername(t *testing.T) {
	assert.True(t, shouldAppendMinerNameToUsername("wallet"))
	assert.False(t, shouldAppendMinerNameToUsername("wallet.worker-a"))
	assert.False(t, shouldAppendMinerNameToUsername(""))
	assert.False(t, shouldAppendMinerNameToUsername("   "))
}

// TestExecuteCommand_UpdateMinerPassword_PersistFailureFailsCommand verifies that
// when the on-device password change succeeds but persisting the new credential
// to the DB fails, the command is reported as failed rather than a false success
// (which would leave Fleet with stale credentials).
func TestExecuteCommand_UpdateMinerPassword_PersistFailureFailsCommand(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Arrange
	encryptSvc, err := encrypt.NewService(&encrypt.Config{
		ServiceMasterKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	})
	require.NoError(t, err)

	// A closed DB makes the credential-persistence transaction fail deterministically.
	closedDB, err := sql.Open("pgx", "")
	require.NoError(t, err)
	require.NoError(t, closedDB.Close())

	mockQueue := mocks.NewMockMessageQueue(ctrl)
	mockMinerGetter := minerMocks.NewMockCachedMinerGetter(ctrl)
	mockMiner := minerIfaceMocks.NewMockMiner(ctrl)

	payload, err := json.Marshal(dto.UpdateMinerPasswordPayload{CurrentPassword: "old", NewPassword: "new"})
	require.NoError(t, err)
	message := queue.Message{ID: 7, DeviceID: 50, CommandType: commandtype.UpdateMinerPassword, Payload: payload}

	mockMinerGetter.EXPECT().GetMinerForPasswordUpdate(gomock.Any(), int64(50), "old").Return(mockMiner, nil)
	mockMiner.EXPECT().GetOrgID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetSiteID().Return(int64(0)).AnyTimes()
	mockMiner.EXPECT().GetDriverName().Return("antminer").AnyTimes()
	mockMiner.EXPECT().GetID().Return(models.DeviceIdentifier("dev-50")).AnyTimes()
	// On-device password change succeeds.
	mockMiner.EXPECT().UpdateMinerPassword(gomock.Any(), gomock.Any()).Return(nil)
	mockMinerGetter.EXPECT().InvalidateMiner(models.DeviceIdentifier("dev-50"))

	svc := NewExecutionService(&Config{
		MaxWorkers:             5,
		MasterPollingInterval:  10 * time.Millisecond,
		WorkerExecutionTimeout: 5 * time.Second,
	}, closedDB, mockQueue, encryptSvc, nil, mockMinerGetter, nil, nil, nil)

	// Act
	_, _, err = svc.executeCommandOnDevice(t.Context(), commandtype.UpdateMinerPassword, message)

	// Assert
	require.Error(t, err, "persist failure after on-device change must fail the command")
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "post-change persistence failure must not be retryable")
	assert.Contains(t, err.Error(), "credential persistence failed")
}
