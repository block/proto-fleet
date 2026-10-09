package command

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	minerDomain "github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type executionTankController struct {
	writes   int
	received sdk.TankMiningRequest
	err      error
	onWrite  func(context.Context) error
}

func (c *executionTankController) SetTankMining(ctx context.Context, r sdk.TankMiningRequest) error {
	c.writes++
	c.received = r
	if c.onWrite != nil {
		return c.onWrite(ctx)
	}
	return c.err
}

func setupTankMiningExecution(t *testing.T) (*ExecutionService, *sql.DB, int64, string, tankMiningPayload) {
	t.Helper()
	svc, conn, _, deviceID, identifier := setupPasswordCommandDevice(t, "proto", false)
	const orgID int64 = 1
	var err error
	_, err = conn.ExecContext(t.Context(), `UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, deviceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,observed_at) VALUES($1,now())`, deviceID)
	require.NoError(t, err)
	var tank int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, deviceID).Scan(&tank))
	payload := tankMiningPayload{TankMiningRequest: sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)}, TankIDs: []int64{tank}, OrgID: orgID}
	return svc, conn, deviceID, identifier, payload
}

func TestTankMiningExecutionRechecksPresenceSiteAndScope(t *testing.T) {
	svc, conn, deviceID, _, payload := setupTankMiningExecution(t)
	orgID := payload.OrgID
	controller := &executionTankController{}
	require.NoError(t, svc.executeTankMining(t.Context(), deviceID, orgID, payload, controller))
	require.Equal(t, 1, controller.writes)
	require.False(t, *controller.received.Mining)
	controller.err = errors.New("connection lost")
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, payload, controller)))
	require.Equal(t, 2, controller.writes)
	for _, sql := range []string{`UPDATE container_tank SET present=false`, `UPDATE container_tank SET last_seen_at=now()-interval '3 minutes'`, `UPDATE container_tank SET position=2`} {
		_, err := conn.ExecContext(t.Context(), `UPDATE container_tank SET present=true,position=1,last_seen_at=now()`)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), sql)
		require.NoError(t, err)
		require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, payload, controller)))
		require.Equal(t, 2, controller.writes)
	}
	_, err := conn.ExecContext(t.Context(), `UPDATE container_tank SET present=true,position=1,last_seen_at=now()`)
	require.NoError(t, err)
	wrongSite := payload
	wrongSite.SiteID = 55
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, wrongSite, controller)))
	require.Equal(t, 2, controller.writes)
}

type executionTankDevice struct {
	sdk.Device
	*executionTankController
}

type executionTankPluginManager struct{ commandTestPluginManager }

func (m *executionTankPluginManager) GetCapabilitiesForDriverName(string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityTankMining: true}
}

func (m *executionTankPluginManager) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return m.GetCapabilitiesForDriverName("proto")
}

func TestTankMiningExecutionUsesAdmittedSiteWithCachedController(t *testing.T) {
	es, conn, deviceID, identifier, oldPayload := setupTankMiningExecution(t)
	ctrl := gomock.NewController(t)
	controller := &executionTankController{}
	driver := sdkMocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), identifier, gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{
		Device: &executionTankDevice{Device: sdkMocks.NewMockDevice(ctrl), executionTankController: controller},
	}, nil).Times(1)
	pluginManager := &executionTankPluginManager{commandTestPluginManager{driverName: "proto", driver: driver}}
	filesSvc, err := files.NewService(files.Config{})
	require.NoError(t, err)
	minerSvc := minerDomain.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), es.encryptService, filesSvc, pluginManager)
	es.minerService = minerSvc
	cached, err := minerSvc.GetMiner(t.Context(), deviceID)
	require.NoError(t, err)
	require.Zero(t, cached.GetSiteID())
	q := sqlc.New(conn)
	site, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: oldPayload.OrgID, Name: "New site", Slug: "new-site"})
	require.NoError(t, err)
	_, err = q.AssignDevicesToSite(t.Context(), sqlc.AssignDevicesToSiteParams{
		OrgID: oldPayload.OrgID, DeviceIdentifiers: []string{identifier}, TargetSiteID: sql.NullInt64{Int64: site.ID, Valid: true},
	})
	require.NoError(t, err)
	userID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: "tank-operator", Username: "tank-operator", PasswordHash: "test", CreatedAt: time.Now()})
	require.NoError(t, err)
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: userID, OrganizationID: oldPayload.OrgID})
	messageQueue := queue.NewDatabaseMessageQueue(&queue.Config{MaxFailureRetries: 1}, conn)
	es.run = newExecutionRun(t.Context())
	t.Cleanup(es.run.cancelAdmission)
	t.Cleanup(es.run.cancelWork)
	svc := NewService(es.config, conn, es, messageQueue, NewStatusService(conn, messageQueue), es.encryptService, filesSvc, nil, nil, nil, nil, nil, nil)
	svc.SetPluginCapabilitiesProvider(pluginManager)
	svc.startStatusUpdateRoutineOverride = func(string, onFinishedCallbackFunc) {}
	_, err = svc.SetTankMining(ctx, oldPayload.TankIDs, new(false), func(_ context.Context, admittedSite *int64, _ bool) error {
		require.NotNil(t, admittedSite)
		require.Equal(t, site.ID, *admittedSite, "admission must authorize the new site")
		return nil
	})
	require.NoError(t, err)
	messages, err := messageQueue.Dequeue(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	// Placement validation and the adapter gate must share the leased slot.
	conn.SetMaxOpenConns(1)
	dispatchCtx, cancelDispatch := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelDispatch()
	orgID, siteID, err := es.executeQueuedCommand(dispatchCtx, messages[0])
	require.NoError(t, err, "the cached old site must not reject a command admitted at the new site")
	require.Equal(t, oldPayload.OrgID, orgID)
	require.Equal(t, site.ID, siteID)
	require.Equal(t, 1, controller.writes)
	stillCached, err := minerSvc.GetMiner(t.Context(), deviceID)
	require.NoError(t, err)
	require.Same(t, cached, stillCached)
	require.Zero(t, stillCached.GetSiteID(), "the test must retain stale cached metadata")
	require.True(t, fleeterror.IsFailedPreconditionError(es.executeTankMining(t.Context(), deviceID, oldPayload.OrgID, oldPayload, controller)))
	require.Equal(t, 1, controller.writes, "a command admitted before reassignment must still be rejected")
}

func TestTankMiningExecutionSerializesSiteReassignmentThroughDispatch(t *testing.T) {
	svc, conn, deviceID, identifier, payload := setupTankMiningExecution(t)
	q := sqlc.New(conn)
	site, err := q.CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: payload.OrgID, Name: "Other", Slug: "other"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered := make(chan struct{})
	proceed := make(chan struct{})
	var siteAtWrite sql.NullInt64
	controller := &executionTankController{onWrite: func(ctx context.Context) error {
		close(entered)
		select {
		case <-proceed:
		case <-ctx.Done():
			return ctx.Err()
		}
		return conn.QueryRowContext(ctx, `SELECT site_id FROM device WHERE id=$1`, deviceID).Scan(&siteAtWrite)
	}}
	dispatched := make(chan error, 1)
	go func() {
		dispatched <- svc.executeTankMining(ctx, deviceID, payload.OrgID, payload, controller)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("tank dispatch did not reach the controller")
	}
	reassigned := make(chan error, 1)
	go func() {
		_, err := q.AssignDevicesToSite(ctx, sqlc.AssignDevicesToSiteParams{
			OrgID: payload.OrgID, DeviceIdentifiers: []string{identifier},
			TargetSiteID: sql.NullInt64{Int64: site.ID, Valid: true},
		})
		reassigned <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND query LIKE '%AssignDevicesToSite%'
			AND wait_event_type='Lock' AND cardinality(pg_blocking_pids(pid)) > 0)`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "site reassignment must wait while tank dispatch is paused")
	close(proceed)
	require.NoError(t, <-dispatched)
	require.False(t, siteAtWrite.Valid, "the physical write must use the admitted site")
	require.NoError(t, <-reassigned)
	device, err := q.GetDeviceByID(ctx, sqlc.GetDeviceByIDParams{ID: deviceID, OrgID: payload.OrgID})
	require.NoError(t, err)
	require.Equal(t, site.ID, device.SiteID.Int64)
	staleController := &executionTankController{}
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(ctx, deviceID, payload.OrgID, payload, staleController)))
	require.Zero(t, staleController.writes, "a command admitted before reassignment must not dispatch afterward")
}

func TestTankMiningExecutionDoesNotReplayPhysicalWrite(t *testing.T) {
	svc, _, deviceID, _, payload := setupTankMiningExecution(t)
	controller := &executionTankController{err: &pgconn.PgError{Code: "40001", Message: "serialization failure after dispatch"}}
	err := svc.executeTankMining(t.Context(), deviceID, payload.OrgID, payload, controller)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.ErrorContains(t, err, "tank mining outcome unconfirmed")
	require.NotContains(t, sanitizedErrorInfo(err).String, controller.err.Error(), "controller diagnostics must not enter operator results")
	require.Equal(t, 1, controller.writes, "even a retryable error must not replay a physical write")
}

func TestTankMiningExecutionRetriesDatabaseErrorsBeforeDispatch(t *testing.T) {
	for _, failure := range []struct {
		name string
		lock string
	}{
		{name: "get device", lock: `LOCK TABLE device IN ACCESS EXCLUSIVE MODE`},
		{name: "lock controller", lock: `LOCK TABLE device_pairing IN ACCESS EXCLUSIVE MODE`},
		{name: "list tanks", lock: `LOCK TABLE container_tank IN ACCESS EXCLUSIVE MODE`},
	} {
		t.Run(failure.name, func(t *testing.T) {
			svc, conn, deviceID, _, payload := setupTankMiningExecution(t)
			blocker, err := conn.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer blocker.Rollback()
			_, err = blocker.ExecContext(t.Context(), failure.lock)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			controller := &executionTankController{}
			err = svc.executeTankMining(ctx, deviceID, payload.OrgID, payload, controller)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			require.Equal(t, connect.CodeInternal, fleetErr.GRPCCode, "a pre-dispatch database failure must remain retryable")
			require.Zero(t, controller.writes)
			require.Equal(t, "internal: tank mining preparation failed; retry shortly", sanitizedErrorInfo(err).String)
			require.NoError(t, blocker.Rollback())
			require.NoError(t, svc.executeTankMining(t.Context(), deviceID, payload.OrgID, payload, controller))
			require.Equal(t, 1, controller.writes, "retry after database recovery may dispatch once")
		})
	}
}

func TestTankMiningExecutionMissingControllerIsTerminal(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE device SET deleted_at=now()`,
		`UPDATE device_pairing SET pairing_status='UNPAIRED'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			svc, conn, deviceID, _, payload := setupTankMiningExecution(t)
			_, err := conn.ExecContext(t.Context(), mutation)
			require.NoError(t, err)
			controller := &executionTankController{}
			err = svc.executeTankMining(t.Context(), deviceID, payload.OrgID, payload, controller)
			require.True(t, fleeterror.IsFailedPreconditionError(err))
			require.ErrorContains(t, err, "selected tank controller unavailable")
			require.Zero(t, controller.writes)
		})
	}
}

func TestTankMiningExecutionCommitFailureDoesNotReplayPhysicalWrite(t *testing.T) {
	svc, conn, deviceID, _, payload := setupTankMiningExecution(t)
	controller := &executionTankController{onWrite: func(ctx context.Context) error {
		// Terminate the transaction after the controller write succeeds, so
		// Commit fails with an unknown outcome rather than replaying the write.
		var terminated bool
		err := conn.QueryRowContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
			WHERE datname=current_database() AND state='idle in transaction'
			AND query LIKE '%ListTankCommandTargets%'`).Scan(&terminated)
		require.NoError(t, err)
		require.True(t, terminated)
		return nil
	}}
	err := svc.executeTankMining(t.Context(), deviceID, payload.OrgID, payload, controller)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Equal(t, "failed_precondition: tank mining outcome unconfirmed", sanitizedErrorInfo(err).String)
	require.Equal(t, 1, controller.writes)
}

func TestTankMiningExecutionSurfacesFleetNodeUpgradeBeforeEnqueue(t *testing.T) {
	svc, _, deviceID, identifier, payload := setupTankMiningExecution(t)
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "session", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V3)
	require.NoError(t, err)
	defer stream.Unregister()
	controller, err := remotenode.New(remotenode.Config{
		Sender: registry, FleetNodeID: 7, OrgID: payload.OrgID,
		DeviceIdentifier: identifier, DriverName: "proto", IPAddress: "192.0.2.10", Port: "443", URLScheme: "https",
	})
	require.NoError(t, err)
	err = svc.executeTankMining(t.Context(), deviceID, payload.OrgID, payload, controller)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Equal(t, "failed_precondition: tank mining requires Fleet Node command protocol version 4; upgrade Fleet Node before retrying", sanitizedErrorInfo(err).String)
	select {
	case <-stream.Outgoing:
		t.Fatal("an unsupported tank command must not reach the Fleet Node")
	default:
	}
}

func TestTankMiningExecutionSanitizesControllerFleetErrors(t *testing.T) {
	svc, _, deviceID, _, payload := setupTankMiningExecution(t)
	controller := &executionTankController{err: fleeterror.NewFailedPreconditionError("untrusted controller diagnostic")}
	err := svc.executeTankMining(t.Context(), deviceID, payload.OrgID, payload, controller)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.Equal(t, "failed_precondition: tank mining outcome unconfirmed", sanitizedErrorInfo(err).String)
	require.Equal(t, 1, controller.writes)
}

func TestTankMiningRetainsPhysicalOutcomeEvidence(t *testing.T) {
	for _, scenario := range []string{"accepted", "failed", "stale topology"} {
		t.Run(scenario, func(t *testing.T) {
			svc, conn, deviceID, _, payload := setupTankMiningExecution(t)
			message, _ := enqueueFenceTestCommand(t, conn, deviceID, commandtype.SetTankMining, payload)
			fences := devicewrite.NewService(conn)
			attempt, err := fences.BeginQueuedHost(t.Context(), message)
			require.NoError(t, err)
			controller := &executionTankController{}
			if scenario == "failed" {
				controller.err = errors.New("connection lost after send")
			}
			if scenario == "stale topology" {
				_, err = conn.ExecContext(t.Context(), `UPDATE container_tank SET present=false WHERE device_id=$1`, deviceID)
				require.NoError(t, err)
			}
			outcome := svc.executeTankMining(attempt.Context(t.Context()), deviceID, payload.OrgID, payload, controller)
			if scenario == "accepted" {
				require.NoError(t, outcome)
			} else {
				require.Error(t, outcome)
			}
			require.NoError(t, attempt.Finish(t.Context(), outcome))
			var status string
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT status FROM device_write_attempt WHERE queue_message_id=$1`, message.ID).Scan(&status))
			if scenario == "stale topology" {
				require.Zero(t, controller.writes)
				require.Equal(t, "complete", status)
				_, err = fences.Reserve(t.Context(), payload.OrgID, deviceID)
				require.NoError(t, err)
			} else {
				require.Equal(t, 1, controller.writes)
				require.Equal(t, "unknown", status)
				_, err = fences.Reserve(t.Context(), payload.OrgID, deviceID)
				require.True(t, fleeterror.IsFailedPreconditionError(err))
			}
		})
	}
}
