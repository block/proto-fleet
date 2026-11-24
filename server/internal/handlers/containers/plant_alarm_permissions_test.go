package containers_test

import (
	"context"
	"database/sql"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/containers"
	"github.com/block/proto-fleet/server/internal/handlers/handlerstest"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

// seedAlarmController inserts a paired CT1 controller placed at siteID (nil
// leaves it unassigned). It has no controls observation, so a request that
// clears authorization stops at admission with a failed precondition instead of
// queueing anything.
func seedAlarmController(t *testing.T, conn *sql.DB, dbs *testutil.DatabaseService, orgID int64, siteID *int64) string {
	t.Helper()
	device := dbs.CreateDevice(orgID, "proto")
	_, err := conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2,model='CT1' WHERE device_identifier=$1`, device.ID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device.DatabaseID)
	require.NoError(t, err)
	if siteID != nil {
		_, err = conn.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, *siteID, device.DatabaseID)
		require.NoError(t, err)
	}
	return device.ID
}

// RunPlantAlarmAction must hand the command service the real equipment-write
// authorizer. The service evaluates it against the controller's current site
// inside admission, so a permissive substitute would let the denied cases pass.
func TestRunPlantAlarmActionAuthorizesEquipmentWriteAtControllerSite(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	dbs := testutil.NewDatabaseService(t, cfg)
	conn := dbs.DB
	orgID := dbs.CreateSuperAdminUser().OrganizationID
	q := sqlc.New(conn)
	site, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: orgID, Name: "North", Slug: "north"})
	require.NoError(t, err)
	other, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: orgID, Name: "South", Slug: "south"})
	require.NoError(t, err)
	sited := seedAlarmController(t, conn, dbs, orgID, &site.ID)
	unassigned := seedAlarmController(t, conn, dbs, orgID, nil)

	// Only admission is exercised, so no execution service or queue is needed.
	handler := containers.NewHandler(nil).WithCommands(command.NewService(&command.Config{}, conn, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	write := []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerSetCoolingMode, authz.PermMinerSetPowerTarget}
	run := func(ctx context.Context, identifier string) error {
		_, err := handler.RunPlantAlarmAction(ctx, connect.NewRequest(&pb.RunPlantAlarmActionRequest{
			DeviceIdentifier: identifier,
			Action:           pb.PlantAlarmAction_PLANT_ALARM_ACTION_MUTE,
		}))
		return err
	}

	t.Run("allowed site reaches admission", func(t *testing.T) {
		err := run(handlerstest.CtxWithAssignments(t, orgID, handlerstest.SiteAssignment(site.ID, write...)), sited)
		require.True(t, fleeterror.IsFailedPreconditionError(err), "authorization must pass and admission must refuse the unobserved controller, got %v", err)
	})
	t.Run("denied site is forbidden", func(t *testing.T) {
		err := run(handlerstest.CtxWithAssignments(t, orgID, handlerstest.SiteAssignment(other.ID, write...)), sited)
		require.True(t, fleeterror.IsForbiddenError(err), "got %v", err)
	})
	t.Run("missing write permission is forbidden", func(t *testing.T) {
		readOnly := write[:3]
		err := run(handlerstest.CtxWithAssignments(t, orgID, handlerstest.SiteAssignment(site.ID, readOnly...)), sited)
		require.True(t, fleeterror.IsForbiddenError(err), "got %v", err)
	})
	t.Run("no site requires org-wide authority", func(t *testing.T) {
		err := run(handlerstest.CtxWithAssignments(t, orgID, handlerstest.SiteAssignment(site.ID, write...)), unassigned)
		require.True(t, fleeterror.IsForbiddenError(err), "a site grant must not authorize an unassigned controller, got %v", err)
		err = run(handlerstest.CtxWithAssignments(t, orgID, handlerstest.OrgAssignment(write...)), unassigned)
		require.True(t, fleeterror.IsFailedPreconditionError(err), "org-wide authority must pass authorization, got %v", err)
	})
}
