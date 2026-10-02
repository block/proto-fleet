package marketdata

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type providerFunc func(context.Context) (Snapshot, error)

func (fn providerFunc) Fetch(ctx context.Context) (Snapshot, error) { return fn(ctx) }

func testConfig() Config {
	return Config{
		Enabled:         true,
		RefreshInterval: time.Minute,
		PriceProvider:   PriceProviderCoinGecko,
		CoinbaseURL:     "https://api.coinbase.com",
		CoinGeckoURL:    "https://pro-api.coingecko.com",
		CoinGeckoAPIKey: "test-key",
		MempoolURL:      "https://mempool.space",
	}
}

func TestServiceCacheAndRecovery(t *testing.T) {
	at := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	now := at
	calls := 0
	provider := providerFunc(func(_ context.Context) (Snapshot, error) {
		calls++
		price := &Metric{Value: float64(calls) * 100000, RetrievedAt: now, Source: "Coinbase"}
		if calls == 2 {
			return Snapshot{Price: price}, errors.New("mining unavailable")
		}
		return Snapshot{Price: price, Hashrate: &Metric{Value: 1e21, RetrievedAt: now}, Hashprice: &Metric{Value: 65, RetrievedAt: now}}, nil
	})
	svc, err := NewService(testConfig(), provider)
	require.NoError(t, err)
	svc.now = func() time.Time { return now }
	first, err := svc.Get(t.Context())
	require.NoError(t, err)
	require.True(t, first.Enabled)
	require.Equal(t, uint32(60), first.RefreshIntervalSeconds)
	first.Price.Value = 1 // clients must not be able to poison the cache
	cached, err := svc.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, 100000.0, cached.Price.Value)
	require.Equal(t, 1, calls)

	now = now.Add(time.Minute)
	partial, err := svc.Get(t.Context())
	require.NoError(t, err)
	require.False(t, partial.Price.Stale)
	require.Equal(t, 200000.0, partial.Price.Value)
	require.True(t, partial.Hashrate.Stale)
	require.True(t, partial.Hashprice.Stale)
	require.Equal(t, at, partial.Hashprice.RetrievedAt)
	_, err = svc.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, calls) // failures are throttled too

	now = now.Add(time.Minute)
	recovered, err := svc.Get(t.Context())
	require.NoError(t, err)
	require.False(t, recovered.Hashprice.Stale)
	require.Equal(t, now, recovered.Hashprice.RetrievedAt)
}

func TestServiceExpiredAndUnavailable(t *testing.T) {
	now := time.Now()
	provider := providerFunc(func(_ context.Context) (Snapshot, error) { return Snapshot{}, errors.New("offline") })
	svc, err := NewService(testConfig(), provider)
	require.NoError(t, err)
	svc.now = func() time.Time { return now }
	svc.cached.Price = &Metric{Value: 100000, RetrievedAt: now.Add(-maxSampleAge)}
	result, err := svc.Get(t.Context())
	require.NoError(t, err)
	require.True(t, result.Enabled)
	require.Nil(t, result.Price)
	require.Nil(t, result.Hashrate)
	require.Nil(t, result.Hashprice)
}

func TestServiceCoalescesRefreshAndCallerCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc, err := NewService(testConfig(), providerFunc(func(ctx context.Context) (Snapshot, error) {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return Snapshot{}, ctx.Err()
		case <-release:
			return Snapshot{Price: &Metric{Value: 100000, RetrievedAt: time.Now()}}, nil
		}
	}))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	firstErr := make(chan error, 1)
	go func() { _, err := svc.Get(ctx); firstErr <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-firstErr, context.Canceled)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			result, err := svc.Get(t.Context())
			require.NoError(t, err)
			require.NotNil(t, result.Price)
		})
	}
	close(release)
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
}

func TestServiceDisabled(t *testing.T) {
	svc, err := NewService(Config{}, nil)
	require.NoError(t, err)
	result, err := svc.Get(t.Context())
	require.NoError(t, err)
	require.False(t, result.Enabled)
}

func TestServiceDisabledDoesNotFetchOrExposeCachedData(t *testing.T) {
	var calls atomic.Int32
	svc, err := NewService(Config{}, providerFunc(func(context.Context) (Snapshot, error) {
		calls.Add(1)
		return Snapshot{}, nil
	}))
	require.NoError(t, err)
	svc.cached = Snapshot{Price: &Metric{Value: 100000, RetrievedAt: time.Now()}}
	for range 3 {
		result, err := svc.Get(t.Context())
		require.NoError(t, err)
		require.Equal(t, Snapshot{}, result)
	}
	require.Zero(t, calls.Load())
}

func TestConfigValidation(t *testing.T) {
	require.NoError(t, testConfig().Validate())
	_, err := NewService(testConfig(), nil)
	require.Error(t, err)
	for _, interval := range []time.Duration{0, time.Second, time.Hour} {
		config := testConfig()
		config.RefreshInterval = interval
		require.Error(t, config.Validate())
	}
	for _, raw := range []string{"file:///tmp/feed", "https://user:secret@example.com", "https://example.com?key=secret", "https://example.com#fragment", "not a URL"} {
		config := testConfig()
		config.MempoolURL = raw
		require.Error(t, config.Validate())
	}
	config := testConfig()
	config.PriceProvider = "unsupported"
	require.Error(t, config.Validate())
	config = testConfig()
	config.PriceProvider = PriceProviderCoinGecko
	config.CoinGeckoURL = "not a URL"
	require.Error(t, config.Validate())
}
