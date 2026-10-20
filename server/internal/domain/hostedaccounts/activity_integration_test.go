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
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, a.AccountID, true, nil))
	page, err = f.svc.Activities(t.Context(), f.user, f.org, &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}})
	require.NoError(t, err)
	require.Empty(t, page.Activities, "current site policy also gates retained events")
	for _, option := range page.FilterOptions.EventTypes {
		require.NotEqual(t, "reboot.completed", option.EventType, "filter options share the same boundary")
	}
}
