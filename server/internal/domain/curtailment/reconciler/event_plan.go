package reconciler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// eventPlan is immutable for one event tick. Locked dispatch fences separately
// validate their freshly loaded event and topology before hardware I/O.
type eventPlan struct {
	event                *models.Event
	scopeJSON            []byte
	candidates           interfaces.ListCandidatesParams
	hasCandidateScope    bool
	topology             bool
	scopeErr             error
	candidateMinPowerW   int32
	postEventCooldownSec int32
}
type eventPlanContextKey struct{}

func decodeEventPlan(ev *models.Event) *eventPlan {
	p := &eventPlan{event: ev, scopeJSON: append([]byte(nil), ev.ScopeJSON...)}
	p.candidates, p.hasCandidateScope = listCandidatesParamsForEventScope(ev)
	if ev.ScopeType == models.ScopeTypeMixed && !p.hasCandidateScope {
		p.scopeErr = fmt.Errorf("invalid mixed scope")
	}
	if ev.ScopeType == models.ScopeTypeSite && !p.hasCandidateScope {
		p.scopeErr = fmt.Errorf("invalid site scope")
	}
	p.topology = p.hasCandidateScope && isTopologyCandidateFilter(p.candidates)
	var snapshot struct {
		CandidateMinPowerW   int32 `json:"candidate_min_power_w"`
		PostEventCooldownSec int32 `json:"post_event_cooldown_sec"`
	}
	if json.Unmarshal(ev.DecisionSnapshotJSON, &snapshot) == nil {
		p.candidateMinPowerW = snapshot.CandidateMinPowerW
		p.postEventCooldownSec = snapshot.PostEventCooldownSec
	}
	return p
}
func eventPlanFromContext(ctx context.Context, ev *models.Event) *eventPlan {
	if p, ok := ctx.Value(eventPlanContextKey{}).(*eventPlan); ok &&
		(p.event == ev || (p.event.ID == ev.ID && p.event.EventUUID == ev.EventUUID && bytes.Equal(p.scopeJSON, ev.ScopeJSON))) {
		return p
	}
	return decodeEventPlan(ev)
}
func candidateMinPowerWForPlan(ctx context.Context, ev *models.Event, fallback int32) int32 {
	if value := eventPlanFromContext(ctx, ev).candidateMinPowerW; value > 0 {
		return value
	}
	return fallback
}
func postEventCooldownForPlan(ctx context.Context, ev *models.Event) int32 {
	if value := eventPlanFromContext(ctx, ev).postEventCooldownSec; value > 0 {
		return value
	}
	return 0
}
func isClosedLoopTopologyEvent(ctx context.Context, ev *models.Event) bool {
	return isClosedLoopFullFleet(ev) && eventPlanFromContext(ctx, ev).topology
}
func isAllPairedTopologyPolicyEvent(ctx context.Context, ev *models.Event) bool {
	return isAllPairedPolicyEvent(ev) && isClosedLoopTopologyEvent(ctx, ev)
}
