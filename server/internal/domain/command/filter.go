package command

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// SkippedDevice describes one identifier excluded by a CommandFilter.
type SkippedDevice struct {
	DeviceIdentifier string
	FilterName       string
	Reason           string
}

// CommandResult is the command-domain result before handlers project it back
// to the existing response protos.
type CommandResult struct {
	BatchIdentifier string
	DispatchedCount int
	Skipped         []SkippedDevice
}

// CommandFilterInput is what processCommand passes to each registered filter.
// Filters must not mutate DeviceIdentifiers.
type CommandFilterInput struct {
	CommandType       commandtype.Type
	OrganizationID    int64
	Actor             session.Actor
	Source            session.Source
	DeviceIdentifiers []string
}

// CommandFilterOutput partitions identifiers into kept and skipped.
type CommandFilterOutput struct {
	Kept    []string
	Skipped []SkippedDevice
}

// CommandFilter is a preflight gate consulted before enqueue. Filters run in
// registration order, each seeing only survivors from earlier filters.
//
// Apply returns an error only for I/O / data-fetch failures, never for the
// policy decision itself — a "no devices pass this filter" outcome is
// expressed by an empty Kept slice with the rejected devices in Skipped.
type CommandFilter interface {
	Name() string
	Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error)
}

// applyFilters accumulates skips while passing survivors through the chain.
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
