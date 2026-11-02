package hostedaccounts_test

import (
	"database/sql"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolRPCOperationRequiresReadAccessToEveryTarget(t *testing.T) {
	f := newRPCFixture(t)
	ctx := t.Context()
	q := sqlc.New(f.db)
	created, err := f.configurations.CreateConfiguration(ctx, rpcRequest(&pb.CreateConfigurationRequest{Configuration: &pb.ConfigurationInput{Name: "Production", Pools: []*pb.PoolInput{{Url: "stratum+tcp://pool.example", Username: "payout"}}}}, f.cookie, ""))
	require.NoError(t, err)
	config := created.Msg.Configuration
	var sites []int64
	var identifiers []string
	for i, name := range []string{"site-a", "site-b"} {
		site, err := q.CreateSite(ctx, sqlc.CreateSiteParams{OrgID: f.org, Name: name, Slug: name})
		require.NoError(t, err)
		sites = append(sites, site.ID)
		identifier := uuid.NewString()
		identifiers = append(identifiers, identifier)
		var discovery int64
		require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, f.org, identifier).Scan(&discovery))
		_, err = f.db.ExecContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id,site_id) VALUES($1,$2,$3,$4,$5)`, f.org, identifier, fmt.Sprintf("02:00:00:00:00:%02x", i), discovery, site.ID)
		require.NoError(t, err)
	}
	review, err := f.configurations.ReviewAssignment(ctx, rpcRequest(&pb.ReviewAssignmentRequest{ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version, DeviceIdentifiers: identifiers}, f.cookie, ""))
	require.NoError(t, err)
	saved, err := f.configurations.ConfirmReview(ctx, rpcRequest(&pb.ConfirmReviewRequest{ReviewId: review.Msg.ReviewId, Password: rpcPassword}, f.cookie, ""))
	require.NoError(t, err)
	username := "pool-reader-" + uuid.NewString()
	userID := f.addUser(t, username, false)
	roles := authz.NewService(f.db, nil)
	libraryRole, err := roles.CreateCustomRole(ctx, f.host, f.org, "Pool reader", "", []string{authz.PermPoolRead})
	require.NoError(t, err)
	equipmentRole, err := roles.CreateCustomRole(ctx, f.host, f.org, "Equipment reader", "", []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	_, err = q.AssignRole(ctx, sqlc.AssignRoleParams{UserID: userID, OrganizationID: f.org, RoleID: libraryRole.ID, ScopeType: "org"})
	require.NoError(t, err)
	cookie := f.login(t, username).Header().Get("Set-Cookie")
	get := func() (*connect.Response[pb.GetOperationResponse], error) {
		return f.configurations.GetOperation(ctx, rpcRequest(&pb.GetOperationRequest{OperationId: saved.Msg.OperationId}, cookie, ""))
	}
	t.Run("pool read alone is insufficient", func(t *testing.T) {
		result, err := get()
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.Nil(t, result, "denied requests must not return target metadata")
	})
	grantSite := func(siteID int64) {
		t.Helper()
		_, err := q.AssignRole(ctx, sqlc.AssignRoleParams{UserID: userID, OrganizationID: f.org, RoleID: equipmentRole.ID, ScopeType: "site", ScopeID: sql.NullInt64{Int64: siteID, Valid: true}})
		require.NoError(t, err)
	}
	grantSite(sites[0])
	t.Run("one inaccessible target rejects the whole response", func(t *testing.T) {
		result, err := get()
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.Nil(t, result)
	})
	grantSite(sites[1])
	t.Run("read access suffices without pool change permission", func(t *testing.T) {
		result, err := get()
		require.NoError(t, err)
		require.Len(t, result.Msg.Targets, 2)
		_, err = f.configurations.ReviewAssignment(ctx, rpcRequest(&pb.ReviewAssignmentRequest{ConfigurationId: config.ConfigurationId, ExpectedVersion: config.Version, DeviceIdentifiers: identifiers}, cookie, ""))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})
	_, err = f.db.ExecContext(ctx, `DELETE FROM user_organization_role WHERE user_id=$1 AND organization_id=$2 AND role_id=$3 AND scope_id=$4`, userID, f.org, equipmentRole.ID, sites[1])
	require.NoError(t, err)
	t.Run("revoked read access applies to existing history", func(t *testing.T) {
		result, err := get()
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.Nil(t, result)
	})
}
