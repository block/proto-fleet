package poolconfigurations

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func lifecycleStates(ctx context.Context, q sqlc.Querier, scope Scope, devices, installations []int64) (map[AssignmentTarget]sqlc.ListVisiblePoolLifecycleTransitionsRow, error) {
	rows, err := q.ListVisiblePoolLifecycleTransitions(ctx, sqlc.ListVisiblePoolLifecycleTransitionsParams{OrganizationID: scope.OrganizationID, OwnerAccountID: scope.AccountID, DeviceIds: devices, InstallationIds: installations})
	if err != nil {
		return nil, err
	}
	result := map[AssignmentTarget]sqlc.ListVisiblePoolLifecycleTransitionsRow{}
	for _, row := range rows {
		if row.ResultCode == "no_default" {
			continue
		}
		target := AssignmentTarget{Kind: row.DisplayKind, ID: row.DisplayID, DeviceID: row.DeviceID}
		if _, ok := result[target]; !ok {
			result[target] = row
		}
	}
	return result, nil
}

func lifecycleDisplayState(event sqlc.ListVisiblePoolLifecycleTransitionsRow) string {
	if event.State == "ready" {
		return "pending"
	}
	if event.State == "completed" {
		return "cleared"
	}
	return event.State
}
