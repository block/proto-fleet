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

// applyDeviceLifecycleStatus also serves board rows covered by a whole-container
// operation. Saved assignments take precedence over lifecycle fallback evidence.
func applyDeviceLifecycleStatus(row *sqlc.ListPoolEquipmentAssignmentsRow, events map[AssignmentTarget]sqlc.ListVisiblePoolLifecycleTransitionsRow) {
	if event, ok := events[AssignmentTarget{Kind: TargetDevice, ID: row.DeviceID, DeviceID: row.DeviceID}]; ok && row.ConfigurationID == "" {
		row.State = lifecycleDisplayState(event)
		row.ResultCode = event.ResultCode
		row.OperationID = event.TransitionID.String()
		row.UpdatedAt = event.UpdatedAt
		row.ConfigurationID = event.ConfigurationID.String
		row.Name = event.ConfigurationName
	}
}
