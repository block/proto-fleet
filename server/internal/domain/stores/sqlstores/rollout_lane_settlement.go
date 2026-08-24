package sqlstores

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func releaseRolloutLaneActiveParentIfSettled(
	ctx context.Context,
	q sqlc.Querier,
	laneID uuid.UUID,
	orgID int64,
) (bool, error) {
	if _, err := q.LockRolloutLane(
		ctx,
		sqlc.LockRolloutLaneParams{LaneID: laneID, OrgID: orgID},
	); err != nil {
		return false, err
	}
	claim, err := q.LockRolloutLaneActiveParent(
		ctx,
		sqlc.LockRolloutLaneActiveParentParams{LaneID: laneID, OrgID: orgID},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state, err := q.GetRolloutLaneSettlementState(
		ctx,
		sqlc.GetRolloutLaneSettlementStateParams{
			LaneID: uuid.NullUUID{UUID: laneID, Valid: true},
			OrgID:  orgID,
			GroupID: uuid.NullUUID{
				UUID: claim.GroupID, Valid: true,
			},
		},
	)
	if err != nil {
		return false, err
	}
	if rolloutLaneSettlementBlocked(state) {
		return false, nil
	}
	rows, err := q.ReleaseRolloutLaneActiveParent(
		ctx,
		sqlc.ReleaseRolloutLaneActiveParentParams{
			LaneID:  laneID,
			OrgID:   orgID,
			GroupID: claim.GroupID,
		},
	)
	if err != nil || rows != 1 {
		return false, err
	}
	if _, err = q.RefreshFirmwareRolloutGroupResult(
		ctx,
		sqlc.RefreshFirmwareRolloutGroupResultParams{
			GroupID: claim.GroupID,
			OrgID:   orgID,
		},
	); err != nil {
		return false, err
	}
	return true, nil
}

func rolloutLaneSettlementBlocked(state sqlc.GetRolloutLaneSettlementStateRow) bool {
	return state.ChildUnsettled ||
		state.OwnerUnsettled ||
		state.ControlUnsettled ||
		state.AuthorityUnsettled ||
		state.EnforcementUnsettled ||
		state.FinalizationUnsettled ||
		state.RevertUnsettled ||
		state.EvidenceUnsettled
}

func rolloutLaneSettlementBlocker(state sqlc.GetRolloutLaneSettlementStateRow) string {
	switch {
	case state.ControlUnsettled:
		return "child control"
	case state.EnforcementUnsettled:
		return "child enforcement"
	case state.FinalizationUnsettled:
		return "child finalization"
	case state.RevertUnsettled:
		return "child revert"
	case state.OwnerUnsettled:
		return "child ownership"
	case state.AuthorityUnsettled:
		return "child authority"
	case state.EvidenceUnsettled:
		return "child evidence"
	case state.ChildUnsettled:
		return "child execution"
	default:
		return ""
	}
}
