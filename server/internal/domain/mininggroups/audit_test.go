package mininggroups

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type recordedGroupActivity struct {
	Type, Description, Result, UserID, Username string
	Metadata                                    json.RawMessage
	SiteID                                      sql.NullInt64
}

func groupActivities(t *testing.T, f retryFixture) []recordedGroupActivity {
	t.Helper()
	rows, err := f.conn.QueryContext(t.Context(), `SELECT event_type,description,result,user_id,username,metadata,site_id FROM activity_log WHERE organization_id=$1 AND event_type LIKE 'mining_groups.%' ORDER BY id`, f.target.OrganizationID)
	require.NoError(t, err)
	defer rows.Close()
	var events []recordedGroupActivity
	for rows.Next() {
		var event recordedGroupActivity
		require.NoError(t, rows.Scan(&event.Type, &event.Description, &event.Result, &event.UserID, &event.Username, &event.Metadata, &event.SiteID))
		events = append(events, event)
	}
	require.NoError(t, rows.Err())
	return events
}

func TestPoolAssignmentActivityPreservesHistoryAndDeduplicatesReplay(t *testing.T) {
	ctx, f, m, config, boards := newTransferFailureFixture(t)
	initial := f.state(t)
	events := groupActivities(t, f)
	require.Len(t, events, 1)
	require.Equal(t, "mining_groups.pools.accepted", events[0].Type)
	require.Contains(t, events[0].Description, "request accepted")
	var metadata struct {
		OperationID    string  `json:"operation_id"`
		DeviceID       int64   `json:"device_id"`
		HashboardIDs   []int64 `json:"hashboard_ids"`
		PoolRevisionID string  `json:"pool_revision_id"`
	}
	require.NoError(t, json.Unmarshal(events[0].Metadata, &metadata))
	require.Equal(t, initial.OperationID.UUID.String(), metadata.OperationID)
	require.Equal(t, f.target.DeviceID, metadata.DeviceID)
	require.Equal(t, boards, metadata.HashboardIDs)
	require.Equal(t, config, metadata.PoolRevisionID)
	_, err := f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, initial.OperationID.UUID.String(), config, boards, initial.Revision-1)
	require.NoError(t, err)
	require.Len(t, groupActivities(t, f), 1)
	m.transferPhase = transferPhaseComplete
	for range 5 {
		f.reconcile(t, ctx)
	}
	require.Equal(t, statusComplete, f.state(t).Status)
	completed := groupActivities(t, f)
	require.Len(t, completed, 2)
	require.Equal(t, "mining_groups.pools.completed", completed[1].Type)
	require.Equal(t, "success", completed[1].Result)
	require.Equal(t, events[0].UserID, completed[1].UserID)
	for _, event := range completed {
		for _, private := range []string{"pool.example", "worker", "ciphertext", "password"} {
			require.NotContains(t, string(event.Metadata), private)
		}
	}
	// A later request must append history rather than replacing the earlier record.
	_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, f.state(t).Revision)
	require.NoError(t, err)
	require.Equal(t, completed, groupActivities(t, f)[:2])
	require.Len(t, groupActivities(t, f), 3)
}

func TestMiningGroupActivityScopesActivationToItsSiteAndOrganization(t *testing.T) {
	ctx, f := newIdleRetryFixture(t)
	q := f.service.queries
	site, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: f.target.OrganizationID, Name: "Mining site", Slug: "mining-site"})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE device SET site_id=$1 WHERE id=$2`, site.ID, f.target.DeviceID)
	require.NoError(t, err)
	operation, err := f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), "", nil, 0)
	require.NoError(t, err)
	// Later placement changes must not move the operation's audit history.
	_, err = f.conn.ExecContext(ctx, `UPDATE device SET site_id=NULL WHERE id=$1`, f.target.DeviceID)
	require.NoError(t, err)
	f.miner.enabled = true
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	events := groupActivities(t, f)
	require.Len(t, events, 2)
	require.Equal(t, "mining_groups.activate.completed", events[1].Type)
	for _, event := range events {
		require.Equal(t, sql.NullInt64{Int64: site.ID, Valid: true}, event.SiteID)
	}
	filter := sqlc.ListActivityLogsParams{OrgID: sql.NullInt64{Int64: f.target.OrganizationID, Valid: true}, SiteIds: []int64{site.ID}, OrgLevelCategories: models.OrgLevelCategories(), PageSize: 100}
	visible, err := q.ListActivityLogs(ctx, filter)
	require.NoError(t, err)
	require.Len(t, visible, 2)
	filter.SiteIds = []int64{site.ID + 1}
	visible, err = q.ListActivityLogs(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, visible)
	_, err = q.GetMiningGroupRequestActivity(ctx, sqlc.GetMiningGroupRequestActivityParams{EventID: activityID(f.target.OrganizationID, operation, "accepted"), OrganizationID: f.target.OrganizationID + 1})
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestMiningGroupActivityIdentityScopesClientRequestIDs(t *testing.T) {
	request := uuid.New()
	accepted := activityID(1, request, "accepted")
	require.Equal(t, accepted, activityID(1, request, "accepted"))
	require.NotEqual(t, accepted, activityID(2, request, "accepted"), "different organizations may submit the same request UUID")
	require.NotEqual(t, accepted, activityID(1, request, "completed"))
	require.NotEqual(t, accepted, activityID(1, uuid.New(), "accepted"))
}

func TestActivationActivityRetainsOriginalActorAfterRevocation(t *testing.T) {
	ctx, f := newRetryFixture(t)
	accepted := groupActivities(t, f)
	require.Len(t, accepted, 1)
	require.Equal(t, "mining_groups.activate.accepted", accepted[0].Type)
	info, err := session.GetInfo(ctx)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE "user" SET username='renamed@example.com' WHERE id=$1`, info.UserID)
	require.NoError(t, err)
	setMiningGroupPermissions(t, ctx, f, nil)
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	require.Equal(t, "failed", f.state(t).Status)
	events := groupActivities(t, f)
	require.Len(t, events, 2)
	require.Equal(t, "mining_groups.activate.failed", events[1].Type)
	require.Equal(t, "failure", events[1].Result)
	require.Equal(t, accepted[0].UserID, events[1].UserID)
	require.Equal(t, accepted[0].Username, events[1].Username)
}

func TestMiningGroupAdmissionRollsBackWhenAuditFails(t *testing.T) {
	ctx, f, _, config, boards := newIdlePoolAssignmentFixture(t)
	before := f.state(t)
	_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_group_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type LIKE 'mining_groups.%' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_group_audit BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION reject_group_audit()`)
	require.NoError(t, err)
	_, err = f.service.submitLegacyRequest(ctx, f.target.DeviceIdentifier, uuid.NewString(), config, boards, before.Revision)
	require.ErrorContains(t, err, "audit unavailable")
	require.Equal(t, before, f.state(t))
	var requests, intents, attempts int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM mining_group_request WHERE device_id=$1`, f.target.DeviceID).Scan(&requests))
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM hashboard_pool_intent WHERE device_id=$1`, f.target.DeviceID).Scan(&intents))
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.target.DeviceID).Scan(&attempts))
	require.Zero(t, requests)
	require.Zero(t, intents)
	require.Zero(t, attempts)
}

func TestMiningGroupOutcomeAndAuditCommitTogether(t *testing.T) {
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, f, m, _, _ := newTransferFailureFixture(t)
			passes := 6
			if outcome == "completed" {
				m.transferPhase = transferPhaseComplete
				passes = 2
			}
			for range passes {
				f.reconcile(t, ctx)
			}
			before := f.state(t)
			_, err := f.conn.ExecContext(ctx, `CREATE FUNCTION reject_group_outcome() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type LIKE 'mining_groups.%' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_group_outcome BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION reject_group_outcome()`)
			require.NoError(t, err)
			err = f.service.Reconcile(ctx, f.target.OrganizationID, f.target.DeviceID, f.target.DeviceIdentifier)
			require.ErrorContains(t, err, "audit unavailable")
			require.Equal(t, before, f.state(t))
			_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.Error(t, err, "audit failure must roll back admission release")
			require.Len(t, groupActivities(t, f), 1)
			_, err = f.conn.ExecContext(ctx, `DROP TRIGGER reject_group_outcome ON activity_log`)
			require.NoError(t, err)
			f.reconcile(t, ctx)
			f.reconcile(t, ctx)
			events := groupActivities(t, f)
			require.Len(t, events, 2)
			require.Equal(t, "mining_groups.pools."+outcome, events[1].Type)
			_, err = f.service.writes.Reserve(ctx, f.target.OrganizationID, f.target.DeviceID)
			require.NoError(t, err)
		})
	}
}
