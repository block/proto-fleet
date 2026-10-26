package ha

import (
	"context"
	"sync"
	"time"
)

// ErrPairChangedForTest is the error failover returns when the pair it was
// asked to fail over no longer matches the database.
var ErrPairChangedForTest = errPairChanged

const (
	ReasonWaitingForNodes         = reasonWaitingForNodes
	ReasonUpgradeRequired         = reasonUpgradeRequired
	ReasonTransferFailed          = reasonTransferFailed
	MaxConcurrentFailoversForTest = maxConcurrentFailovers
)

// SetTimingForTest replaces the failover delay, startup grace, and transfer
// retry backoff.
func (s *Service) SetTimingForTest(failoverDelay, startupGrace, provisionRetry time.Duration) {
	s.failoverDelay, s.startupGrace, s.provisionRetry = failoverDelay, startupGrace, provisionRetry
}

func (s *Service) SetFailoverTimeoutForTest(timeout time.Duration) {
	s.failoverTimeout = timeout
}

// TickForTest runs one pass of Run as if fleetd started at started, and waits
// for any credential key transfers or automatic failovers it began.
func (s *Service) TickForTest(ctx context.Context, started time.Time) {
	var tasks sync.WaitGroup
	s.tick(ctx, started, &tasks)
	tasks.Wait()
}

// FailoverForTest runs a failover for the pair as given, without checking it
// against the database first.
func (s *Service) FailoverForTest(ctx context.Context, orgID, pairID, activeID, standbyID int64) error {
	return s.failover(ctx, orgID, pairID, activeID, standbyID, automaticFailover)
}
