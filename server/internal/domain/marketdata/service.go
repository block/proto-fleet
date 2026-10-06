package marketdata

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const maxSampleAge = time.Hour

// Service shares one on-demand cache across users and organizations. Refreshes
// are coalesced and throttled even on failures. No background polling occurs
// without a reader, and no fleet/user data is sent to external providers.
type Service struct {
	config      Config
	provider    Provider
	now         func() time.Time
	mu          sync.Mutex
	cached      Snapshot
	nextRefresh time.Time
	refreshing  chan struct{}
}

func NewService(config Config, provider Provider) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Enabled && provider == nil {
		return nil, fmt.Errorf("market-data provider is required when enabled")
	}
	return &Service{config: config, provider: provider, now: time.Now}, nil
}

func (s *Service) Get(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("market-data request canceled: %w", err)
	}
	if !s.config.Enabled {
		return Snapshot{}, nil
	}
	s.mu.Lock()
	if s.refreshing == nil && s.now().Before(s.nextRefresh) {
		result := s.snapshot()
		s.mu.Unlock()
		return result, nil
	}
	if s.refreshing == nil {
		s.refreshing = make(chan struct{})
		go s.refresh()
	}
	ready := s.refreshing
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return Snapshot{}, fmt.Errorf("market-data request canceled: %w", ctx.Err())
	case <-ready:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.snapshot(), nil
	}
}

func (s *Service) refresh() {
	// A canceled browser request must not cancel a refresh shared by other
	// readers. Bound its independent lifetime so slow feeds can't hang the cache.
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	result, err := s.provider.Fetch(ctx)
	if err != nil {
		slog.Warn("Some market-data feeds are unavailable", "error", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached.Price = merge(s.cached.Price, result.Price)
	s.cached.Hashrate = merge(s.cached.Hashrate, result.Hashrate)
	s.cached.Hashprice = merge(s.cached.Hashprice, result.Hashprice)
	s.nextRefresh = s.now().Add(s.config.RefreshInterval)
	close(s.refreshing)
	s.refreshing = nil
}

func merge(previous, fresh *Metric) *Metric {
	if fresh != nil {
		cloned := *fresh
		return &cloned
	}
	if previous != nil {
		cloned := *previous
		cloned.Stale = true
		return &cloned
	}
	return nil
}

// snapshot is called under mu. Expired samples are omitted, never presented as
// current or as zero. Copies prevent callers from modifying cached state.
func (s *Service) snapshot() Snapshot {
	now := s.now()
	visible := func(sample *Metric) *Metric {
		if sample == nil || now.Sub(sample.RetrievedAt) >= maxSampleAge {
			return nil
		}
		cloned := *sample
		cloned.Stale = cloned.Stale || now.Sub(sample.RetrievedAt) >= 2*s.config.RefreshInterval
		return &cloned
	}
	// #nosec G115 -- NewService validates enabled intervals are 60..900 seconds.
	seconds := uint32(s.config.RefreshInterval / time.Second)
	return Snapshot{Enabled: true, RefreshIntervalSeconds: seconds, Price: visible(s.cached.Price), Hashrate: visible(s.cached.Hashrate), Hashprice: visible(s.cached.Hashprice)}
}
