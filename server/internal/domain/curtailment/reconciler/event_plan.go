package reconciler

import (
	"encoding/json"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

func candidateMinPowerWForEvent(ev *models.Event, fallback int32) int32 {
	if ev == nil || len(ev.DecisionSnapshotJSON) == 0 {
		return fallback
	}
	var snapshot struct {
		CandidateMinPowerW int32 `json:"candidate_min_power_w"`
	}
	if err := json.Unmarshal(ev.DecisionSnapshotJSON, &snapshot); err != nil || snapshot.CandidateMinPowerW <= 0 {
		return fallback
	}
	return snapshot.CandidateMinPowerW
}

func postEventCooldownSecForEvent(ev *models.Event) int32 {
	if ev == nil || len(ev.DecisionSnapshotJSON) == 0 {
		return 0
	}
	var snapshot struct {
		PostEventCooldownSec int32 `json:"post_event_cooldown_sec"`
	}
	if err := json.Unmarshal(ev.DecisionSnapshotJSON, &snapshot); err != nil || snapshot.PostEventCooldownSec <= 0 {
		return 0
	}
	return snapshot.PostEventCooldownSec
}

func isAllPairedTopologyPolicyEvent(ev *models.Event) bool {
	return isAllPairedPolicyEvent(ev) && isClosedLoopTopologyEvent(ev)
}

func isClosedLoopTopologyEvent(ev *models.Event) bool {
	if !isClosedLoopFullFleet(ev) {
		return false
	}
	scopeParams, hasScope := listCandidatesParamsForEventScope(ev)
	return hasScope && isTopologyCandidateFilter(scopeParams)
}
