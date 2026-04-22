package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
)

// stubStore is a hand-rolled stub so tests don't depend on the mock package
// wiring. It records calls to DeleteOlderThan and returns a scripted sequence
// of (deleted, err) pairs.
type stubStore struct {
	calls  []stubCall
	script []stubReply
}

type stubCall struct {
	cutoff  time.Time
	maxRows int32
}

type stubReply struct {
	deleted int64
	err     error
}

func (s *stubStore) DeleteOlderThan(ctx context.Context, cutoff time.Time, maxRows int32) (int64, error) {
	s.calls = append(s.calls, stubCall{cutoff: cutoff, maxRows: maxRows})
	if len(s.calls) > len(s.script) {
		return 0, errors.New("stubStore: unexpected extra call")
	}
	reply := s.script[len(s.calls)-1]
	return reply.deleted, reply.err
}

// unused interface methods — we only exercise DeleteOlderThan here.
func (s *stubStore) Insert(_ context.Context, _ *models.Event) error { return nil }
func (s *stubStore) List(_ context.Context, _ models.Filter) ([]models.Entry, error) {
	return nil, nil
}
func (s *stubStore) Count(_ context.Context, _ models.Filter) (int64, error) { return 0, nil }
func (s *stubStore) GetDistinctUsers(_ context.Context, _ int64) ([]models.UserInfo, error) {
	return nil, nil
}
func (s *stubStore) GetDistinctEventTypes(_ context.Context, _ int64) ([]models.EventTypeInfo, error) {
	return nil, nil
}
func (s *stubStore) GetDistinctScopeTypes(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}

func TestDefaultRetentionConfig(t *testing.T) {
	rc := &RetentionConfig{}
	defaultRetentionConfig(rc)
	assert.Equal(t, 8760*time.Hour, rc.ActivityLogRetention)
	assert.Equal(t, 6*time.Hour, rc.CleanupInterval)
	assert.Equal(t, 1000, rc.DeleteBatchLimit)
}

func TestDefaultRetentionConfig_ClampsTinyCleanupInterval(t *testing.T) {
	rc := &RetentionConfig{CleanupInterval: time.Millisecond}
	defaultRetentionConfig(rc)
	assert.Equal(t, minCleanupInterval, rc.CleanupInterval,
		"sub-minute cleanup interval must be clamped to the minimum")
}

func TestDefaultRetentionConfig_ClampsHugeDeleteBatchLimit(t *testing.T) {
	rc := &RetentionConfig{DeleteBatchLimit: 10_000_000}
	defaultRetentionConfig(rc)
	assert.Equal(t, maxDeleteBatchSize, rc.DeleteBatchLimit,
		"oversized delete batch limit must be clamped down")
}

func TestRetentionCleaner_DrainsUntilShortPage(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	store := &stubStore{script: []stubReply{
		{deleted: 10, err: nil},
		{deleted: 10, err: nil},
		{deleted: 3, err: nil},
	}}
	cfg := &RetentionConfig{ActivityLogRetention: 24 * time.Hour, DeleteBatchLimit: 10}
	c := NewRetentionCleaner(store, cfg)
	c.now = func() time.Time { return now }

	err := c.runOnce(context.Background())
	require.NoError(t, err)
	assert.Len(t, store.calls, 3)

	// Every call must use the same cutoff and limit.
	expectedCutoff := now.Add(-cfg.ActivityLogRetention)
	for _, call := range store.calls {
		assert.Equal(t, expectedCutoff, call.cutoff)
		assert.Equal(t, int32(10), call.maxRows)
	}
}

func TestRetentionCleaner_PropagatesStoreError(t *testing.T) {
	store := &stubStore{script: []stubReply{
		{deleted: 10, err: nil},
		{deleted: 0, err: errors.New("db boom")},
	}}
	// Small limit so the first full page triggers a second iteration.
	cfg := &RetentionConfig{DeleteBatchLimit: 10}
	c := NewRetentionCleaner(store, cfg)

	err := c.runOnce(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db boom")
	assert.Len(t, store.calls, 2)
}

func TestRetentionCleaner_StopsOnCancelledContext(t *testing.T) {
	store := &stubStore{script: []stubReply{{deleted: 10, err: nil}}}
	cfg := &RetentionConfig{}
	c := NewRetentionCleaner(store, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.runOnce(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, store.calls, "canceled ctx must exit before any delete")
}
