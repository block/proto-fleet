package command

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, svc.executeTankMining(t.Context(), deviceID, orgID, 0, payload, controller))
	require.Equal(t, 1, controller.writes)
	require.False(t, *controller.received.Mining)
	controller.err = errors.New("connection lost")
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, 0, payload, controller)))
	require.Equal(t, 2, controller.writes)
	for _, sql := range []string{`UPDATE container_tank SET present=false`, `UPDATE container_tank SET last_seen_at=now()-interval '3 minutes'`, `UPDATE container_tank SET position=2`} {
		_, err := conn.ExecContext(t.Context(), `UPDATE container_tank SET present=true,position=1,last_seen_at=now()`)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), sql)
		require.NoError(t, err)
		require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, 0, payload, controller)))
		require.Equal(t, 2, controller.writes)
	}
	_, err := conn.ExecContext(t.Context(), `UPDATE container_tank SET present=true,position=1,last_seen_at=now()`)
	require.NoError(t, err)
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(t.Context(), deviceID, orgID, 55, payload, controller)))
	require.Equal(t, 2, controller.writes)
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
		dispatched <- svc.executeTankMining(ctx, deviceID, payload.OrgID, 0, payload, controller)
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
	require.True(t, fleeterror.IsFailedPreconditionError(svc.executeTankMining(ctx, deviceID, payload.OrgID, 0, payload, staleController)))
	require.Zero(t, staleController.writes, "a command admitted before reassignment must not dispatch afterward")
}

func TestTankMiningExecutionDoesNotReplayPhysicalWrite(t *testing.T) {
	svc, _, deviceID, _, payload := setupTankMiningExecution(t)
	controller := &executionTankController{err: &pgconn.PgError{Code: "40001", Message: "serialization failure after dispatch"}}
	err := svc.executeTankMining(t.Context(), deviceID, payload.OrgID, 0, payload, controller)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	require.ErrorContains(t, err, "tank mining outcome unconfirmed")
	require.Equal(t, 1, controller.writes, "even a retryable error must not replay a physical write")
}
