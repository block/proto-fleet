package command

import (
	"context"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// CurtailmentActiveFilterName tags Skipped entries so the schedule processor
// (and audit) can tell curtailment-active skips apart from priority conflicts.
const CurtailmentActiveFilterName = "curtailment_active"

const curtailmentActiveSkipReason = "device is part of an active curtailment event"

// CurtailmentActiveQuerier is the minimal store surface this filter needs;
// interfaces.CurtailmentStore satisfies it directly.
type CurtailmentActiveQuerier interface {
	ListActiveCurtailedDevices(ctx context.Context, orgID int64) ([]string, error)
}

// CurtailmentActiveFilter blocks non-curtailment commands against the org's
// currently-curtailed device set. Reconciler self-traffic
// (Actor == ActorCurtailment) bypasses the gate.
type CurtailmentActiveFilter struct {
	querier CurtailmentActiveQuerier
}

func NewCurtailmentActiveFilter(querier CurtailmentActiveQuerier) *CurtailmentActiveFilter {
	return &CurtailmentActiveFilter{querier: querier}
}

func (f *CurtailmentActiveFilter) Name() string {
	return CurtailmentActiveFilterName
}

func (f *CurtailmentActiveFilter) Apply(ctx context.Context, in CommandFilterInput) (CommandFilterOutput, error) {
	// Reconciler self-bypass: only Curtail/Uncurtail under ActorCurtailment.
	if in.Actor == session.ActorCurtailment &&
		(in.CommandType == commandtype.Curtail || in.CommandType == commandtype.Uncurtail) {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}
	if len(in.DeviceIdentifiers) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	active, err := f.querier.ListActiveCurtailedDevices(ctx, in.OrganizationID)
	if err != nil {
		return CommandFilterOutput{}, fmt.Errorf("failed to list active curtailed devices: %w", err)
	}
	// Fast path: no active events.
	if len(active) == 0 {
		return CommandFilterOutput{Kept: in.DeviceIdentifiers}, nil
	}

	activeSet := make(map[string]struct{}, len(active))
	for _, id := range active {
		activeSet[id] = struct{}{}
	}

	var kept []string
	var skipped []SkippedDevice
	for _, id := range in.DeviceIdentifiers {
		if _, locked := activeSet[id]; locked {
			skipped = append(skipped, SkippedDevice{
				DeviceIdentifier: id,
				FilterName:       f.Name(),
				Reason:           curtailmentActiveSkipReason,
			})
			continue
		}
		kept = append(kept, id)
	}
	return CommandFilterOutput{Kept: kept, Skipped: skipped}, nil
}
