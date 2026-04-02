package diagnostics

import (
	"context"
	"log/slog"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics/models"
)

// WatchKind represents the state of an error in a watch update.
type WatchKind int

const (
	// WatchKindOpened indicates a newly created error (first_seen_at within poll window).
	WatchKindOpened WatchKind = iota + 1
	// WatchKindUpdated indicates an existing error that was updated (first_seen_at before poll window, last_seen_at within).
	WatchKindUpdated
	// WatchKindClosed indicates an error that has been resolved (closed_at is now set).
	WatchKindClosed
)

// WatchUpdate represents a single update from the watch stream.
type WatchUpdate struct {
	Kind   WatchKind
	Errors []models.ErrorMessage
}

// WatchOptions configures the watch behavior.
type WatchOptions struct {
	Filter *models.QueryFilter
}

// watcher manages a single watch session with time-based polling.
type watcher struct {
	service        *Service
	orgID          int64
	opts           *WatchOptions
	pollInterval   time.Duration
	updateChan     chan *WatchUpdate
	lastPollTime   time.Time
	droppedUpdates int
	baseFilter     *models.QueryFilter
}

// Fallback defaults if config values are zero/invalid.
const (
	defaultWatchPollInterval  = 20 * time.Second
	defaultWatchChannelBuffer = 10
)

// newWatcher creates a new watcher instance.
func newWatcher(s *Service, orgID int64, opts *WatchOptions, config Config) *watcher {
	if opts == nil {
		opts = &WatchOptions{}
	}

	bufferSize := getConfigIntOrDefault(config.WatchChannelBuffer, defaultWatchChannelBuffer)
	pollInterval := getConfigDurationOrDefault(config.WatchPollInterval, defaultWatchPollInterval)

	// Pre-build base filter from options (immutable fields copied once)
	baseFilter := &models.QueryFilter{
		IncludeClosed: true,
	}
	if opts.Filter != nil {
		baseFilter.DeviceIdentifiers = opts.Filter.DeviceIdentifiers
		baseFilter.DeviceTypes = opts.Filter.DeviceTypes
		baseFilter.ComponentIDs = opts.Filter.ComponentIDs
		baseFilter.ComponentTypes = opts.Filter.ComponentTypes
		baseFilter.MinerErrors = opts.Filter.MinerErrors
		baseFilter.Severities = opts.Filter.Severities
		baseFilter.Logic = opts.Filter.Logic
	}

	return &watcher{
		service:      s,
		orgID:        orgID,
		opts:         opts,
		pollInterval: pollInterval,
		updateChan:   make(chan *WatchUpdate, bufferSize),
		baseFilter:   baseFilter,
	}
}

// run starts the watch loop and blocks until context is cancelled.
func (w *watcher) run(ctx context.Context) {
	defer close(w.updateChan)

	w.lastPollTime = time.Now()

	// Initial poll
	w.pollAndSend(ctx)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollAndSend(ctx)
		}
	}
}

// pollAndSend queries for errors updated since lastPollTime and sends events.
func (w *watcher) pollAndSend(ctx context.Context) {
	// Capture poll start time BEFORE query to avoid race condition:
	// If we update lastPollTime after the query completes, errors that arrive
	// during the query execution would be missed by the next poll.
	pollStartTime := time.Now()
	filter := w.buildFilterWithTimeFrom()

	// Query the store directly - we only need raw errors, not the grouping/pagination
	// logic that Service.Query() provides. This is more efficient for watch polling.
	errors, err := w.service.errorStore.QueryErrors(ctx, &models.QueryOptions{
		OrgID:    w.orgID,
		Filter:   filter,
		PageSize: MaxPageSize,
	})
	if err != nil {
		hasFilter := w.opts != nil && w.opts.Filter != nil
		slog.Warn("watch poll failed",
			"error", err,
			"orgID", w.orgID,
			"hasFilter", hasFilter,
			"pollInterval", w.pollInterval)
		return
	}

	// Classify errors by their state
	var openedErrors, updatedErrors, closedErrors []models.ErrorMessage
	for _, e := range errors {
		if e.ClosedAt != nil {
			// Error was resolved
			closedErrors = append(closedErrors, e)
		} else if !e.FirstSeenAt.Before(w.lastPollTime) {
			// Error is NEW (first_seen_at >= lastPollTime, within this poll window)
			openedErrors = append(openedErrors, e)
		} else {
			// Error existed before but was updated (first_seen_at < lastPollTime)
			updatedErrors = append(updatedErrors, e)
		}
	}

	// Send events (only if non-empty)
	if len(openedErrors) > 0 {
		w.sendEvent(ctx, WatchKindOpened, openedErrors)
	}
	if len(updatedErrors) > 0 {
		w.sendEvent(ctx, WatchKindUpdated, updatedErrors)
	}
	if len(closedErrors) > 0 {
		w.sendEvent(ctx, WatchKindClosed, closedErrors)
	}

	w.lastPollTime = pollStartTime
}

// buildFilterWithTimeFrom updates the base filter with time_from set to lastPollTime.
func (w *watcher) buildFilterWithTimeFrom() *models.QueryFilter {
	w.baseFilter.TimeFrom = &w.lastPollTime
	return w.baseFilter
}

// Threshold for escalating dropped update warnings from Warn to Error level.
const droppedUpdateErrorThreshold = 3

// sendEvent sends an update to the channel. Non-blocking - drops if channel is full.
func (w *watcher) sendEvent(ctx context.Context, kind WatchKind, errors []models.ErrorMessage) {
	update := &WatchUpdate{
		Kind:   kind,
		Errors: errors,
	}

	select {
	case w.updateChan <- update:
		// Sent successfully - reset dropped counter on successful send
		w.droppedUpdates = 0
	case <-ctx.Done():
		// Context cancelled, don't send
	default:
		w.droppedUpdates++
		logLevel := slog.LevelWarn
		if w.droppedUpdates >= droppedUpdateErrorThreshold {
			logLevel = slog.LevelError
		}
		slog.Log(ctx, logLevel, "watch channel full, dropping update",
			"orgID", w.orgID,
			"kind", kind,
			"errorCount", len(errors),
			"consecutiveDrops", w.droppedUpdates)
	}
}
