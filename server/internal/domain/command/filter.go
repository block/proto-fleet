package command

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// SkippedDevice describes a single device that a CommandFilter excluded from
// the dispatch list. Callers that need to log "we skipped N devices because
// X" use the Reason and FilterName fields. Reason is human-readable; for
// machine-grouping use FilterName.
type SkippedDevice struct {
	DeviceIdentifier string
	FilterName       string
	Reason           string
}

// CommandResult is what processCommand returns to every public wrapper on
// Service. BatchIdentifier is empty when nothing was dispatched (either the
// caller's selector was empty or every selected device was filtered out) — in
// that case no command_batch_log row is created and no queue messages are
// enqueued, but Skipped may still be populated so callers can audit the skip.
type CommandResult struct {
	BatchIdentifier string
	DispatchedCount int
	Skipped         []SkippedDevice
}

// CommandFilterInput is what processCommand passes to each registered filter.
// Filters MUST NOT mutate DeviceIdentifiers; copy if you need to.
type CommandFilterInput struct {
	CommandType       commandtype.Type
	OrganizationID    int64
	Actor             session.Actor
	Source            session.Source
	DeviceIdentifiers []string
}

// CommandFilterOutput partitions the input identifiers into kept (will be
// dispatched if no later filter excludes them) and skipped. Filters that
// don't apply to a given input should return Kept=in.DeviceIdentifiers with
// Skipped=nil — the framework treats that as a pass-through.
type CommandFilterOutput struct {
	Kept    []string
	Skipped []SkippedDevice
}

// CommandFilter is a preflight gate consulted by processCommand before a
// command is enqueued. Filters are idempotent: re-running a filter on its
// own output must produce no further skips. Filter ordering is deterministic
// (registration order); each filter sees only the survivors of earlier
// filters.
//
// Apply returns an error only for I/O / data-fetch failures, never for the
// policy decision itself — a "no devices pass this filter" outcome is
// expressed by an empty Kept slice with the rejected devices in Skipped.
type CommandFilter interface {
	Name() string
	Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error)
}

// applyFilters runs every registered filter in order. Each filter sees only
// the kept slice from the previous filter. Skipped devices accumulate across
// filters and preserve which filter rejected them.
func applyFilters(ctx context.Context, filters []CommandFilter, in CommandFilterInput) (kept []string, skipped []SkippedDevice, err error) {
	kept = in.DeviceIdentifiers
	for _, f := range filters {
		if len(kept) == 0 {
			break
		}
		stepIn := in
		stepIn.DeviceIdentifiers = kept
		out, err := f.Apply(ctx, stepIn)
		if err != nil {
			return nil, nil, err
		}
		kept = out.Kept
		skipped = append(skipped, out.Skipped...)
	}
	return kept, skipped, nil
}
