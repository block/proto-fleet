package diagnostics

import (
	"context"
	"log/slog"
	"time"
)

// Fallback defaults for closer configuration.
const (
	defaultCloserPollInterval       = 30 * time.Second
	defaultCloserStalenessThreshold = 2 * time.Minute
)

// runCloser periodically closes stale errors until the context is cancelled.
func (s *Service) runCloser(ctx context.Context) {
	pollInterval := getConfigDurationOrDefault(s.config.CloserPollInterval, defaultCloserPollInterval)
	stalenessThreshold := getConfigDurationOrDefault(s.config.CloserStalenessThreshold, defaultCloserStalenessThreshold)

	slog.Info("starting error closer",
		"pollInterval", pollInterval,
		"stalenessThreshold", stalenessThreshold)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("error closer stopped")
			return
		case <-ticker.C:
			s.closeStaleErrors(ctx, stalenessThreshold)
		}
	}
}

// closeStaleErrors closes errors that haven't been seen within the threshold.
func (s *Service) closeStaleErrors(ctx context.Context, threshold time.Duration) {
	closed, err := s.errorStore.CloseStaleErrors(ctx, threshold)
	if err != nil {
		slog.Error("failed to close stale errors", "error", err)
		return
	}

	if closed > 0 {
		slog.Info("closed stale errors", "count", closed, "threshold", threshold)
	}
}
