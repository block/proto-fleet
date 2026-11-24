package hostedaccounts_test

import (
	"database/sql"
	"testing"

	activitypb "github.com/block/proto-fleet/server/generated/grpc/activity/v1"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountActivityInheritsAncestorSitePolicy(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	require.NoError(t, f.allocate(t, f.host, f.root, parent.AccountID, 1, selected(5, miner, 0)))
	require.NoError(t, f.allocate(t, f.user, parent.AccountID, child.AccountID, 1, selected(5, miner, 1)))
	batch := uuid.NewString()
	var batchID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO command_batch_log(uuid,type,created_by,organization_id,status,devices_count) VALUES($1,'reboot',$2,$3,'FINISHED',1) RETURNING id`, batch, f.host, f.org).Scan(&batchID))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO command_on_device_log(command_batch_log_id,device_id,org_id,status) VALUES($1,$2,$3,'SUCCESS')`, batchID, miner, f.org)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,batch_id) VALUES($1,$2,'device_command','reboot.completed','Reboot','success','system',$3)`, uuid.New(), f.org, batch)
	require.NoError(t, err)
	request := &pb.ListAccountActivitiesRequest{AccountId: child.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}}
	before, err := f.svc.Activities(t.Context(), f.host, f.org, request)
	require.NoError(t, err)
	require.Len(t, before.Activities, 1)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, parent.AccountID, true, nil))
	after, err := f.svc.Activities(t.Context(), f.host, f.org, request)
	require.NoError(t, err)
	require.Empty(t, after.Activities, "a child's unrestricted local policy cannot override its ancestor")
	require.Zero(t, after.TotalCount)
	require.NotEmpty(t, before.AuthorityFingerprint)
	require.NotEqual(t, before.AuthorityFingerprint, after.AuthorityFingerprint)
	for _, option := range after.FilterOptions.EventTypes {
		require.NotEqual(t, "reboot.completed", option.EventType)
	}
	exported, err := f.svc.ActivityExportPage(t.Context(), f.host, f.org, request)
	require.NoError(t, err)
	require.Empty(t, exported.Activities)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.user, f.org, parent.AccountID, false, nil))
	parentContext, err := hostedaccounts.WithRequestAccountID(t.Context(), parent.AccountID)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(parentContext, f.user, f.org, child.AccountID, true, nil))
	ceiling, err := f.svc.Activities(t.Context(), f.host, f.org, request)
	require.NoError(t, err)
	require.Empty(t, ceiling.Activities, "parent-controlled site ceilings apply independently of local policy")
}

func TestAccountActivityFingerprintTracksHostPhysicalAuthority(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Customer", 0, hostedaccounts.AllCapabilities)
	q := sqlc.New(f.db)
	site, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Allocated", Slug: "allocated"})
	require.NoError(t, err)
	miner := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site.ID, miner)
	require.NoError(t, err)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, miner, 0)))
	batch := uuid.NewString()
	var batchID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO command_batch_log(uuid,type,created_by,organization_id,status,devices_count) VALUES($1,'reboot',$2,$3,'FINISHED',1) RETURNING id`, batch, f.host, f.org).Scan(&batchID))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO command_on_device_log(command_batch_log_id,device_id,org_id,site_id,status) VALUES($1,$2,$3,$4,'SUCCESS')`, batchID, miner, f.org, site.ID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,batch_id) VALUES($1,$2,'device_command','reboot.completed','Reboot','success','system',$3)`, uuid.New(), f.org, batch)
	require.NoError(t, err)
	roles := authz.NewService(f.db, nil)
	reader, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Physical reader", "", []string{authz.PermAccountManage, authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	manager, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Account manager", "", []string{authz.PermAccountManage})
	require.NoError(t, err)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: reader.ID, ScopeType: "org"})
	require.NoError(t, err)
	request := &pb.ListAccountActivitiesRequest{AccountId: client.AccountID, PageSize: 100, Filter: &activitypb.ActivityFilter{EventCategories: []string{"device_command"}}}
	before, err := f.svc.Activities(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, before.Activities, 1)
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: manager.ID, ScopeType: "site", ScopeID: sql.NullInt64{Int64: site.ID, Valid: true}})
	require.NoError(t, err)
	after, err := f.svc.Activities(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Empty(t, after.Activities)
	require.NotEqual(t, before.AuthorityFingerprint, after.AuthorityFingerprint, "physical scope narrowing must withdraw previously loaded pages")
}

func TestAccountActivityExportPagesReauthorizeAfterRevocation(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO activity_log(event_id,organization_id,event_category,event_type,description,result,actor_type,metadata) SELECT gen_random_uuid(),$1,'auth','create_hosted_account_pool','private audit text','success','user',jsonb_build_object('account_id',$2::text,'password','secret') FROM generate_series(1,101)`, f.org, a.AccountID)
	require.NoError(t, err)
	request := &pb.ListAccountActivitiesRequest{AccountId: a.AccountID, PageSize: 100}
	first, err := f.svc.ActivityExportPage(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.Len(t, first.Activities, 100)
	require.NotEmpty(t, first.NextPageToken)
	request.PageToken = first.NextPageToken
	second, err := f.svc.ActivityExportPage(t.Context(), f.user, f.org, request)
	require.NoError(t, err)
	require.NotEmpty(t, second.Activities)
	require.Empty(t, second.NextPageToken)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, f.user))
	_, err = f.svc.ActivityExportPage(t.Context(), f.user, f.org, request)
	require.True(t, fleeterror.IsForbiddenError(err), "a subsequent export chunk cannot reuse revoked admission: %v", err)
}
