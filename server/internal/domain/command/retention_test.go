package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRetentionConfig_FillsZeros(t *testing.T) {
	rc := &RetentionConfig{}
	defaultRetentionConfig(rc)
	assert.Equal(t, 720*time.Hour, rc.QueueMessageRetention)
	assert.Equal(t, 2160*time.Hour, rc.DeviceLogRetention)
	assert.Equal(t, 4320*time.Hour, rc.BatchLogRetention)
	assert.Equal(t, time.Hour, rc.CleanupInterval)
	assert.Equal(t, 1000, rc.DeleteBatchLimit)
}

func TestDefaultRetentionConfig_RespectsOverrides(t *testing.T) {
	rc := &RetentionConfig{
		QueueMessageRetention: time.Second,
		DeviceLogRetention:    2 * time.Second,
		BatchLogRetention:     3 * time.Second,
		CleanupInterval:       4 * time.Second,
		DeleteBatchLimit:      7,
	}
	defaultRetentionConfig(rc)
	assert.Equal(t, time.Second, rc.QueueMessageRetention)
	assert.Equal(t, 2*time.Second, rc.DeviceLogRetention)
	assert.Equal(t, 3*time.Second, rc.BatchLogRetention)
	assert.Equal(t, 4*time.Second, rc.CleanupInterval)
	assert.Equal(t, 7, rc.DeleteBatchLimit)
}

func TestRetentionCleaner_DrainLoopsUntilLessThanLimit(t *testing.T) {
	cfg := &RetentionConfig{DeleteBatchLimit: 10}
	defaultRetentionConfig(cfg)
	c := &RetentionCleaner{config: cfg}

	calls := 0
	fn := func(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
		calls++
		switch calls {
		case 1:
			return 10, nil // full page -> keep going
		case 2:
			return 10, nil // full page -> keep going
		case 3:
			return 3, nil // partial page -> stop
		default:
			t.Fatalf("unexpected extra call %d", calls)
			return 0, nil
		}
	}

	total, err := c.drain(context.Background(), "test", time.Now(), fn)
	require.NoError(t, err)
	assert.Equal(t, int64(23), total)
	assert.Equal(t, 3, calls)
}

func TestRetentionCleaner_DrainStopsOnError(t *testing.T) {
	cfg := &RetentionConfig{DeleteBatchLimit: 10}
	defaultRetentionConfig(cfg)
	c := &RetentionCleaner{config: cfg}

	calls := 0
	fn := func(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("db gone")
		}
		return 10, nil
	}

	total, err := c.drain(context.Background(), "test", time.Now(), fn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db gone")
	assert.Equal(t, int64(10), total, "first page counted before the error")
	assert.Equal(t, 2, calls)
}

func TestRetentionCleaner_DrainRespectsContextCancellation(t *testing.T) {
	cfg := &RetentionConfig{DeleteBatchLimit: 10}
	defaultRetentionConfig(cfg)
	c := &RetentionCleaner{config: cfg}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	fn := func(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
		calls++
		return 10, nil
	}

	_, err := c.drain(ctx, "test", time.Now(), fn)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, calls, "canceled ctx should exit before any call")
}
