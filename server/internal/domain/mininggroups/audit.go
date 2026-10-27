package mininggroups

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// Audit metadata contains references only, never pool endpoints or credentials.
type activityMetadata struct {
	OperationID    string  `json:"operation_id"`
	DeviceID       int64   `json:"device_id"`
	HashboardIDs   []int64 `json:"hashboard_ids,omitempty"`
	PoolRevisionID string  `json:"pool_revision_id,omitempty"`
	Status         string  `json:"status"`
}

func activityID(orgID int64, operation uuid.UUID, phase string) uuid.UUID {
	return uuid.NewSHA1(operation, []byte(fmt.Sprintf("mining_groups.%d.%s", orgID, phase)))
}

func activitySubject(kind string) string {
	if kind == operationActivate {
		return "Mining-group activation"
	}
	return "Mining-group pool assignment"
}

func recordAcceptedActivity(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, identifier string, site sql.NullInt64, config string, boards []int64) error {
	actor, err := q.GetUserById(ctx, row.ActorID.Int64)
	if err != nil {
		return err
	}
	metadata, err := encode(activityMetadata{OperationID: row.OperationID.UUID.String(), DeviceID: row.DeviceID, HashboardIDs: boards, PoolRevisionID: config, Status: "accepted"})
	if err != nil {
		return err
	}
	return q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
		EventID: activityID(row.OrganizationID, row.OperationID.UUID, "accepted"), EventCategory: string(models.CategoryFleetManagement),
		EventType: "mining_groups." + row.OperationKind + ".accepted", Description: activitySubject(row.OperationKind) + " request accepted",
		Result: string(models.ResultSuccess), ActorType: string(models.ActorUser),
		UserID: sql.NullString{String: actor.UserID, Valid: true}, Username: sql.NullString{String: actor.Username, Valid: true},
		OrganizationID: sql.NullInt64{Int64: row.OrganizationID, Valid: true}, SiteID: site,
		ScopeType: sql.NullString{String: "container", Valid: true}, ScopeLabel: sql.NullString{String: identifier, Valid: true},
		ScopeCount: sql.NullInt32{Int32: 1, Valid: true}, Metadata: pqtype.NullRawMessage{RawMessage: metadata, Valid: true}, MemberSiteIds: []int64{},
	})
}

// The journal lock and transaction make each transition occur once. Stable
// event IDs add a uniqueness guard; replayed submissions and terminal workflows
// return before inserting again. Copy admission attribution rather than looking
// up a possibly renamed/deleted user or a device moved to another site.
func recordOutcomeActivity(ctx context.Context, q sqlc.Querier, row sqlc.ContainerMiningGroupState, outcome string) error {
	accepted, err := q.GetMiningGroupRequestActivity(ctx, sqlc.GetMiningGroupRequestActivityParams{
		EventID: activityID(row.OrganizationID, row.OperationID.UUID, "accepted"), OrganizationID: row.OrganizationID,
	})
	if err != nil {
		return err
	}
	var metadata activityMetadata
	if err := decode(accepted.Metadata.RawMessage, &metadata); err != nil {
		return err
	}
	metadata.Status = outcome
	encoded, err := encode(metadata)
	if err != nil {
		return err
	}
	result := models.ResultSuccess
	if outcome == "failed" {
		result = models.ResultFailure
	}
	return q.InsertActivityLog(ctx, sqlc.InsertActivityLogParams{
		EventID: activityID(row.OrganizationID, row.OperationID.UUID, outcome), EventCategory: accepted.EventCategory,
		EventType: "mining_groups." + row.OperationKind + "." + outcome, Description: activitySubject(row.OperationKind) + " " + outcome,
		Result: string(result), ActorType: accepted.ActorType, UserID: accepted.UserID, Username: accepted.Username,
		OrganizationID: accepted.OrganizationID, SiteID: accepted.SiteID,
		ScopeType: accepted.ScopeType, ScopeLabel: accepted.ScopeLabel, ScopeCount: accepted.ScopeCount,
		Metadata: pqtype.NullRawMessage{RawMessage: encoded, Valid: true}, MemberSiteIds: []int64{},
	})
}
