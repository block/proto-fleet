package rollout

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

// fakeDispatcher records every firmware-update dispatch instead of talking
// to miners. Sends succeed and report all devices as dispatched.
type fakeDispatcher struct {
	sent [][]string
}

func (f *fakeDispatcher) FirmwareUpdate(_ context.Context, selector *commandpb.DeviceSelector, _ string) (*command.CommandResult, error) {
	ids := selector.GetIncludeDevices().GetDeviceIdentifiers()
	f.sent = append(f.sent, ids)
	return &command.CommandResult{DispatchedCount: len(ids), DispatchedDeviceIdentifiers: ids}, nil
}

// fakeFirmwareFiles serves two Rig firmware files: fw-1 (1.5.0) and fw-2 (2.0.0).
type fakeFirmwareFiles struct{}

func (fakeFirmwareFiles) GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error) {
	switch fileID {
	case "fw-1":
		return files.FirmwareMetadata{TargetModel: "Rig", FirmwareVersion: "1.5.0"}, nil
	case "fw-2":
		return files.FirmwareMetadata{TargetModel: "Rig", FirmwareVersion: "2.0.0"}, nil
	}
	return files.FirmwareMetadata{}, fmt.Errorf("unknown firmware file %q", fileID)
}

// fakeActivity captures rollout lifecycle events.
type fakeActivity struct {
	events []activitymodels.Event
}

func (f *fakeActivity) Log(_ context.Context, event activitymodels.Event) {
	f.events = append(f.events, event)
}

type fixture struct {
	conn       *sql.DB
	svc        *Service
	dispatcher *fakeDispatcher
	activity   *fakeActivity
	clock      time.Time
	orgID      int64
	deviceIDs  map[string]int64
}

// newFixture provisions an org and miners named miner-0..n-1 (model Rig,
// firmware 1.0.0, status ACTIVE, hashing 100 H/s). The service clock is
// frozen.
func newFixture(t *testing.T, minerCount int) *fixture {
	t.Helper()
	if testing.Short() || os.Getenv("DB_PASSWORD") == "" {
		t.Skip("rollout integration tests need a database (DB_PASSWORD)")
	}
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()

	var orgID int64
	require.NoError(t, conn.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name)
		VALUES ('rollout-test-org', 'Rollout test org')
		RETURNING id
	`).Scan(&orgID))

	f := &fixture{
		conn:       conn,
		dispatcher: &fakeDispatcher{},
		activity:   &fakeActivity{},
		clock:      time.Now(),
		orgID:      orgID,
		deviceIDs:  map[string]int64{},
	}
	for i := range minerCount {
		f.addMiner(t, fmt.Sprintf("miner-%d", i), "Rig")
	}
	f.svc = NewService(sqlstores.NewSQLReleaseChannelStore(conn), sqlstores.NewSQLTransactor(conn), f.dispatcher, fakeFirmwareFiles{}, f.activity)
	f.svc.now = func() time.Time { return f.clock }
	return f
}

func (f *fixture) addMiner(t *testing.T, identifier, model string) int64 {
	t.Helper()
	ctx := t.Context()
	var discoveredID, deviceID int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `
		INSERT INTO discovered_device (org_id, device_identifier, model, driver_name, firmware_version, ip_address, port, url_scheme)
		VALUES ($1, $2, $3, 'proto', '1.0.0', '10.0.0.1', '80', 'http')
		RETURNING id
	`, f.orgID, identifier, model).Scan(&discoveredID))
	n := len(f.deviceIDs)
	require.NoError(t, f.conn.QueryRowContext(ctx, `
		INSERT INTO device (device_identifier, mac_address, org_id, discovered_device_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, identifier, fmt.Sprintf("00:00:00:00:%02x:%02x", n/256, n%256), f.orgID, discoveredID).Scan(&deviceID))
	_, err := f.conn.ExecContext(ctx, `INSERT INTO device_status (device_id, status) VALUES ($1, 'ACTIVE')`, deviceID)
	require.NoError(t, err)
	f.deviceIDs[identifier] = deviceID
	f.reportHashrate(t, identifier, 100)
	return deviceID
}

// reportHashrate lands a fresh hashrate sample for the miner.
func (f *fixture) reportHashrate(t *testing.T, identifier string, hashRateHs float64) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO device_metrics (time, device_identifier, hash_rate_hs)
		VALUES (now(), $1, $2)
	`, identifier, hashRateHs)
	require.NoError(t, err)
}

// --- Fleet placement fixtures ---

func (f *fixture) addSite(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(),
		`INSERT INTO site (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id`,
		f.orgID, name, strings.ToLower(strings.ReplaceAll(name, " ", "-"))).Scan(&id))
	return id
}

func (f *fixture) addBuilding(t *testing.T, siteID int64, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(),
		`INSERT INTO building (org_id, site_id, name) VALUES ($1, $2, $3) RETURNING id`, f.orgID, siteID, name).Scan(&id))
	return id
}

func (f *fixture) addRack(t *testing.T, siteID, buildingID int64, label string) int64 {
	t.Helper()
	ctx := t.Context()
	var id int64
	require.NoError(t, f.conn.QueryRowContext(ctx,
		`INSERT INTO device_set (org_id, type, label) VALUES ($1, 'rack', $2) RETURNING id`, f.orgID, label).Scan(&id))
	_, err := f.conn.ExecContext(ctx, `
		INSERT INTO device_set_rack (device_set_id, org_id, rows, columns, site_id, building_id)
		VALUES ($1, $2, 4, 4, $3, $4)
	`, id, f.orgID, siteID, buildingID)
	require.NoError(t, err)
	return id
}

func (f *fixture) addGroup(t *testing.T, label string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(),
		`INSERT INTO device_set (org_id, type, label) VALUES ($1, 'group', $2) RETURNING id`, f.orgID, label).Scan(&id))
	return id
}

func (f *fixture) placeInSet(t *testing.T, setID int64, setType, identifier string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO device_set_membership (org_id, device_set_id, device_set_type, device_id, device_identifier)
		VALUES ($1, $2, $3::device_set_type, $4, $5)
	`, f.orgID, setID, setType, f.deviceIDs[identifier], identifier)
	require.NoError(t, err)
}

func (f *fixture) removeFromSet(t *testing.T, setID int64, identifier string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(),
		`DELETE FROM device_set_membership WHERE device_set_id = $1 AND device_id = $2`, setID, f.deviceIDs[identifier])
	require.NoError(t, err)
}

func (f *fixture) placeAtSite(t *testing.T, siteID int64, identifier string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(),
		`UPDATE device SET site_id = $1 WHERE id = $2`, siteID, f.deviceIDs[identifier])
	require.NoError(t, err)
}

func ptr[T any](v T) *T { return &v }
