package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	activitypb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountActivityIsolationPaginationAndRevocation(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer A", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Private sibling", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, f.svc.Rename(t.Context(), f.user, f.org, a.AccountID, "Renamed A"))
	require.NoError(t, f.svc.Rename(t.Context(), f.host, f.org, b.AccountID, "Sibling secret"))
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, a.AccountID, "Private child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.svc.Rename(t.Context(), f.host, f.org, child.AccountID, "Child secret"))
	request := &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 1,
		Filter: &activitypb.ActivityFilter{EventTypes: []string{"create_hosted_account", "rename_hosted_account"}}}
	page, err := f.svc.Activities(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalCount)
	require.Len(t, page.Activities, 1)
	require.Equal(t, "rename_hosted_account", page.Activities[0].EventType)
	require.NotEmpty(t, page.NextPageToken)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{b.AccountID, child.AccountID, "Sibling secret", "Child secret", "previous_name", "member_user_id", "administrator_user_id"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Nil(t, page.Activities[0].UserId)
	require.Nil(t, page.Activities[0].BatchId)
	require.Empty(t, page.FilterOptions.Users)
	request.PageToken = page.NextPageToken
	next, err := f.svc.Activities(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, next.Activities, 1)
	require.Equal(t, "create_hosted_account", next.Activities[0].EventType)
	require.Zero(t, next.TotalCount, "cursor pages skip summary aggregation")
	require.Nil(t, next.FilterOptions)
	request.AccountId = b.AccountID
	_, err = f.svc.Activities(t.Context(), f.user, f.org, request)
	require.Error(t, err, "sibling account cannot reuse a cursor")
	request.AccountId = a.AccountID
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, f.user))
	_, err = f.svc.Activities(t.Context(), f.user, f.org, request)
	require.Error(t, err, "every page resolves current access")
}

func TestAccountActivityAllocationBoundariesAndSafeCommandCounts(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer A", f.user, hostedaccounts.AllCapabilities)
	b := f.client(t, "Customer B", f.host, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	other := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 1, selected(5, miner, 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 2, selected(5, miner, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, b.AccountID, 1, selected(5, miner, 2), selected(5, other, 0)))
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	end := start.Add(30 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE client_id=$3`, start, end, a.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, end, b.ID)
	require.NoError(t, err)
	for _, at := range []time.Time{start.Add(-time.Second), start, end.Add(-time.Second), end, end.Add(time.Second)} {
		batch := uuid.NewString()
		var batchID int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO command_batch_log(uuid,type,created_by,organization_id,status,devices_count,created_at) VALUES($1,'reboot',$2,$3,'FINISHED',2,$4) RETURNING id`, batch, f.host, f.org, at).Scan(&batchID))
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO command_on_device_log(command_batch_log_id,device_id,org_id,status,updated_at,error_info) VALUES($1,$2,$4,'SUCCESS',$5,NULL),($1,$3,$4,'FAILED',$5,'sibling-secret')`, batchID, miner, other, f.org, at)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,created_at,batch_id,metadata,error_message,scope_label,scope_count) VALUES($1,$2,'device_command','reboot.completed','sibling-secret','failure','user',$3,$4,'{"failure_count":99,"password":"secret"}','sibling-secret','private rack',99)`, uuid.New(), f.org, at, batch)
		require.NoError(t, err)
	}
	page, err := f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalCount)
	require.Len(t, page.Activities, 2, "half-open established allocation periods only")
	for _, entry := range page.Activities {
		require.EqualValues(t, 1, entry.ScopeCount)
		require.Equal(t, "success", entry.Result, "a sibling failure cannot change this client's result")
		require.Nil(t, entry.BatchId, "cannot drill into an organization-wide batch")
		require.Nil(t, entry.ErrorMessage)
	}
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sibling-secret")
	require.NotContains(t, string(encoded), "private rack")
	assignments, err := f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventTypes: []string{"account_assets_assigned", "account_assets_recalled"}}})
	require.NoError(t, err)
	require.EqualValues(t, 2, assignments.TotalCount, "host assignment and recall are independently visible")
	require.Len(t, assignments.Activities, 2)
	descriptions := map[string]string{}
	for _, entry := range assignments.Activities {
		require.Equal(t, "asset", entry.GetScopeType())
		require.EqualValues(t, 1, entry.ScopeCount)
		descriptions[entry.EventType] = entry.Description
	}
	require.Equal(t, map[string]string{
		"account_assets_assigned": "Assigned mining assets",
		"account_assets_recalled": "Recalled mining assets",
	}, descriptions)
	recalls, err := f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventTypes: []string{"account_assets_recalled"}}})
	require.NoError(t, err)
	require.EqualValues(t, 1, recalls.TotalCount)
	require.Len(t, recalls.Activities, 1)
	require.Equal(t, "account_assets_recalled", recalls.Activities[0].EventType)
	require.Equal(t, "Recalled mining assets", recalls.Activities[0].Description)
	require.Equal(t, "asset", recalls.Activities[0].GetScopeType())
	require.EqualValues(t, 1, recalls.Activities[0].ScopeCount)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, a.AccountID, true, nil))
	page, err = f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}})
	require.NoError(t, err)
	require.Empty(t, page.Activities, "current site policy also gates retained events")
	for _, option := range page.FilterOptions.EventTypes {
		require.NotEqual(t, "reboot.completed", option.EventType, "filter options share the same boundary")
	}
}

func (f fixture) activityCommand(t *testing.T, batchAt, deviceAt, auditAt time.Time, device int64) string {
	t.Helper()
	batch := uuid.NewString()
	var batchID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO command_batch_log(uuid,type,created_by,organization_id,status,devices_count,created_at) VALUES($1,'reboot',$2,$3,'FINISHED',1,$4) RETURNING id`, batch, f.host, f.org, batchAt).Scan(&batchID))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO command_on_device_log(command_batch_log_id,device_id,org_id,status,updated_at) VALUES($1,$2,$3,'SUCCESS',$4)`, batchID, device, f.org, deviceAt)
	require.NoError(t, err)
	eventID := uuid.NewString()
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,created_at,batch_id) VALUES($1,$2,'device_command','reboot.completed','private','success','user',$3,$4)`, eventID, f.org, auditAt, batch)
	require.NoError(t, err)
	return eventID
}

func TestAccountActivityFiltersAgreeAcrossSummaryCursorAndExport(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, 1, selected(5, miner, 0)))
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2`, start, a.ID)
	require.NoError(t, err)
	first := start.Add(time.Minute)
	second := start.Add(2 * time.Minute)
	f.activityCommand(t, first, first, first, miner)
	f.activityCommand(t, second, second, second, miner)
	for _, tc := range []struct {
		name   string
		filter *activitypb.ActivityFilter
		count  int
	}{
		{"all", nil, 4},
		{"category", &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}, 2},
		{"type", &activitypb.ActivityFilter{EventTypes: []string{"create_hosted_account"}}, 1},
		{"scope", &activitypb.ActivityFilter{ScopeTypes: []string{"asset"}}, 1},
		{"search", &activitypb.ActivityFilter{SearchText: "rebooted miners"}, 2},
		{"time range", &activitypb.ActivityFilter{StartTime: timestamppb.New(first), EndTime: timestamppb.New(first)}, 1},
		{"excluded", &activitypb.ActivityFilter{EventCategories: []string{"pool"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: tc.filter}
			complete, err := f.svc.Activities(t.Context(), f.user, f.org, request)
			require.NoError(t, err)
			require.EqualValues(t, tc.count, complete.TotalCount)
			require.Len(t, complete.Activities, tc.count)
			for _, export := range []bool{false, true} {
				request.PageSize = 1
				request.PageToken = ""
				var entries []*activitypb.ActivityEntry
				for {
					var page *pb.ListAccountActivitiesResponse
					if export {
						page, err = f.svc.ActivityExportPage(t.Context(), f.user, f.org, request)
					} else {
						page, err = f.svc.Activities(t.Context(), f.user, f.org, request)
					}
					require.NoError(t, err)
					entries = append(entries, page.Activities...)
					require.LessOrEqual(t, len(entries), tc.count, "cursor must advance")
					if page.NextPageToken == "" {
						break
					}
					request.PageToken = page.NextPageToken
				}
				require.Len(t, entries, tc.count)
				for i, entry := range entries {
					require.True(t, proto.Equal(complete.Activities[i], entry), "filtered rows and sanitized counts must agree across query paths")
				}
			}
		})
	}
}

func TestAccountActivitySubclientCommandIsolation(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	first, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "First child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	siblingUser := addUser(t, f.db, f.org)
	second, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Second child", siblingUser, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	require.EqualValues(t, 2, first.Depth)
	require.EqualValues(t, 2, second.Depth)
	firstMiner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	secondMiner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	parentMiner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	require.NoError(t, f.allocate(t, f.host, f.root, parent.AccountID, assign, selected(5, firstMiner, 0), selected(5, secondMiner, 0), selected(5, parentMiner, 0)))
	require.NoError(t, f.allocate(t, f.user, parent.AccountID, first.AccountID, assign, selected(5, firstMiner, 1)))
	require.NoError(t, f.allocate(t, f.user, parent.AccountID, second.AccountID, assign, selected(5, secondMiner, 1)))
	var at time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT max(started_at)+interval '1 minute' FROM hosted_account_asset_interval WHERE client_id=$1`, parent.ID).Scan(&at))
	firstEvent := f.activityCommand(t, at, at, at, firstMiner)
	secondEvent := f.activityCommand(t, at, at, at, secondMiner)
	parentEvent := f.activityCommand(t, at, at, at, parentMiner)
	for _, account := range []struct {
		name, id string
		actor    int64
		events   []string
	}{
		{"parent", parent.AccountID, f.user, []string{firstEvent, secondEvent, parentEvent}},
		{"first child", first.AccountID, f.host, []string{firstEvent}},
		{"second child", second.AccountID, siblingUser, []string{secondEvent}},
	} {
		t.Run(account.name, func(t *testing.T) {
			page, err := f.svc.Activities(t.Context(), account.actor, f.org, &pb.ListAccountActivitiesRequest{AccountId: account.id, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}})
			require.NoError(t, err)
			require.EqualValues(t, len(account.events), page.TotalCount)
			var events []string
			for _, entry := range page.Activities {
				events = append(events, entry.EventId)
				require.EqualValues(t, 1, entry.ScopeCount)
			}
			require.ElementsMatch(t, account.events, events, "children must not see their sibling's or parent's retained miner commands")
		})
	}
}

func TestAccountActivityCommandClocksMustShareOneAllocationInterval(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_RECALL, selected(5, miner, 1)))
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN, selected(5, miner, 2)))
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	end := start.Add(20 * time.Minute)
	reassigned := start.Add(40 * time.Minute)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1,ended_at=$2 WHERE client_id=$3 AND ended_at IS NOT NULL`, start, end, a.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2 AND ended_at IS NULL`, reassigned, a.ID)
	require.NoError(t, err)
	inside := start.Add(time.Minute)
	afterReassignment := reassigned.Add(time.Minute)
	cases := []struct {
		name                       string
		batchAt, deviceAt, auditAt time.Time
		visible                    bool
	}{
		{"first interval", inside, inside, inside, true},
		{"inclusive start", start, start, start, true},
		{"before exclusive end", end.Add(-time.Second), end.Add(-time.Second), end.Add(-time.Second), true},
		{"second interval", afterReassignment, afterReassignment, afterReassignment, true},
		{"batch before start", start.Add(-time.Second), inside, inside, false},
		{"device before start", inside, start.Add(-time.Second), inside, false},
		{"audit before start", inside, inside, start.Add(-time.Second), false},
		{"batch at exclusive end", end, inside, inside, false},
		{"device at exclusive end", inside, end, inside, false},
		{"audit at exclusive end", inside, inside, end, false},
		{"recall gap", end.Add(time.Minute), end.Add(time.Minute), end.Add(time.Minute), false},
		{"batch crosses reassignment", inside, afterReassignment, afterReassignment, false},
		{"device crosses reassignment", afterReassignment, inside, afterReassignment, false},
		{"audit crosses reassignment", afterReassignment, afterReassignment, inside, false},
	}
	var eventIDs []string
	for _, tc := range cases {
		eventIDs = append(eventIDs, f.activityCommand(t, tc.batchAt, tc.deviceAt, tc.auditAt, miner))
	}
	page, err := f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}})
	require.NoError(t, err)
	require.EqualValues(t, 4, page.TotalCount)
	require.Len(t, page.Activities, 4)
	visible := map[string]bool{}
	for _, entry := range page.Activities {
		visible[entry.EventId] = true
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.visible, visible[eventIDs[i]], "batch, device and audit clocks must share one established interval")
		})
	}
}

func TestAccountActivityExcludesUnlistedEventsAndNonMinerCommands(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	module, controller := f.miningModule(t)
	assign := pb.AssetAssignmentIntent_ASSET_ASSIGNMENT_INTENT_ASSIGN
	require.NoError(t, f.allocate(t, f.host, f.root, a.AccountID, assign, selected(5, miner, 0), selected(6, module, 0)))
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	moved, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE client_id=$2 AND (kind=5 OR device_id=$3)`, start, a.ID, controller)
	require.NoError(t, err)
	rows, err := moved.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 2, rows, "the module interval covers its controller device")
	command := func(eventType string, device int64) {
		t.Helper()
		batch := uuid.NewString()
		var batchID int64
		at := start.Add(time.Minute)
		require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO command_batch_log(uuid,type,created_by,organization_id,status,devices_count,created_at) VALUES($1,'reboot',$2,$3,'FINISHED',1,$4) RETURNING id`, batch, f.host, f.org, at).Scan(&batchID))
		_, err := f.db.ExecContext(t.Context(), `INSERT INTO command_on_device_log(command_batch_log_id,device_id,org_id,status,updated_at) VALUES($1,$2,$3,'SUCCESS',$4)`, batchID, device, f.org, at)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,created_at,batch_id) VALUES($1,$2,'device_command',$3,'private','success','user',$4,$5)`, uuid.New(), f.org, eventType, at, batch)
		require.NoError(t, err)
	}
	command("reboot.completed", miner)
	command("reboot.failed", miner)
	command("reboot.completed", controller)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,metadata) VALUES($1,$2,'auth','delete_hosted_account','private','success','user',jsonb_build_object('account_id',$3::text))`, uuid.New(), f.org, a.AccountID)
	require.NoError(t, err)
	page, err := f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100})
	require.NoError(t, err)
	commands := 0
	for _, entry := range page.Activities {
		require.NotEqual(t, "delete_hosted_account", entry.EventType, "only allowlisted account events are visible")
		require.NotEqual(t, "reboot.failed", entry.EventType, "only allowlisted command outcomes are visible")
		if entry.EventCategory == "device_command" {
			commands++
		}
	}
	require.Equal(t, 1, commands, "controller commands are excluded even when its module is allocated")
	require.EqualValues(t, len(page.Activities), page.TotalCount)
	for _, option := range page.FilterOptions.EventTypes {
		require.NotContains(t, []string{"delete_hosted_account", "reboot.failed"}, option.EventType)
	}
}
