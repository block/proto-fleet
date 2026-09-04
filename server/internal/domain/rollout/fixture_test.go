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

func (f *fakeDispatcher) FirmwareUpdateArtifact(_ context.Context, selector *commandpb.DeviceSelector, _ string, _ files.FirmwareMetadata) (*command.CommandResult, error) {
	ids := selector.GetIncludeDevices().GetDeviceIdentifiers()
	f.sent = append(f.sent, ids)
	return &command.CommandResult{DispatchedCount: len(ids), DispatchedDeviceIdentifiers: ids}, nil
}

// Checksums of the two Proto Rig firmware files the fake files service
// serves: fw-1 (1.5.0) and fw-2 (2.0.0).
const (
	checksum1 = "1111111111111111111111111111111111111111111111111111111111111111"
	checksum2 = "2222222222222222222222222222222222222222222222222222222222222222"
)

func (f *fakeDispatcher) sentIdentifiers() []string {
	var all []string
	for _, batch := range f.sent {
		all = append(all, batch...)
	}
	return all
}

// fakeFirmwareFiles serves fw-1 and fw-2 for the Proto Rig; files can be
// "deleted" to exercise the artifact identity rule.
type fakeFirmwareFiles struct {
	deleted map[string]bool
}

var fakeArtifacts = map[string]files.FirmwareArtifact{
	"fw-1": {FileID: "fw-1", Checksum: checksum1, Metadata: files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "1.5.0"}},
	"fw-2": {FileID: "fw-2", Checksum: checksum2, Metadata: files.FirmwareMetadata{TargetManufacturer: "Proto", TargetModel: "Rig", FirmwareVersion: "2.0.0"}},
}

func (f *fakeFirmwareFiles) ResolveFirmwareArtifact(fileID string) (files.FirmwareArtifact, error) {
	if a, ok := fakeArtifacts[fileID]; ok && !f.deleted[fileID] {
		return a, nil
	}
	return files.FirmwareArtifact{}, fmt.Errorf("unknown firmware file %q", fileID)
}

func (f *fakeFirmwareFiles) FirmwareFileIDsByChecksum(sha256Hex string) []string {
	var ids []string
	for id, a := range fakeArtifacts {
		if a.Checksum == sha256Hex && !f.deleted[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func (f *fakeFirmwareFiles) FindFirmwareFileIDByChecksum(sha256Hex string) (string, bool) {
	ids := f.FirmwareFileIDsByChecksum(sha256Hex)
	if len(ids) == 0 {
		return "", false
	}
	return ids[0], true
}

// fakeActivity captures rollout lifecycle events.
type fakeActivity struct {
	events []activitymodels.Event
}

func (f *fakeActivity) Log(_ context.Context, event activitymodels.Event) {
	f.events = append(f.events, event)
}

func (f *fakeActivity) types() []string {
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.Type)
	}
	return out
}

type fixture struct {
	conn       *sql.DB
	svc        *Service
	dispatcher *fakeDispatcher
	files      *fakeFirmwareFiles
	activity   *fakeActivity
	clock      time.Time
	orgID      int64
	channelID  int64
	deviceIDs  map[string]int64
}

// newFixture provisions an org and miners named miner-0..n-1 (Proto Rig,
// firmware 1.0.0, status ACTIVE, hashing 100 H/s). The service clock is
// frozen and advanced with advanceClock. Call channel to create a channel.
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
		files:      &fakeFirmwareFiles{deleted: map[string]bool{}},
		activity:   &fakeActivity{},
		clock:      time.Now(),
		orgID:      orgID,
		deviceIDs:  map[string]int64{},
	}
	for i := range minerCount {
		f.addMiner(t, fmt.Sprintf("miner-%d", i), "Rig")
	}
	queries := sqlstores.NewSQLConnectionManager(conn)
	f.svc = NewService(&queries, sqlstores.NewSQLTransactor(conn), f.dispatcher, f.files, f.activity)
	f.svc.now = func() time.Time { return f.clock }
	return f
}

// channel creates a channel over the given miners with the given behavior
// and remembers it as the fixture's channel.
func (f *fixture) channel(t *testing.T, behavior Behavior, miners ...string) *Channel {
	t.Helper()
	ch, err := f.svc.CreateChannel(t.Context(), f.orgID, 1, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: miners}, Behavior: behavior,
	})
	require.NoError(t, err)
	f.channelID = ch.ID
	return ch
}

func (f *fixture) allMiners() []string {
	out := make([]string, 0, len(f.deviceIDs))
	for i := range len(f.deviceIDs) {
		out = append(out, fmt.Sprintf("miner-%d", i))
	}
	return out
}

// addMiner adds a Proto miner of the given model; the observed manufacturer
// is stored lowercase so the ASCII fold to the canonical "Proto" key is
// exercised.
func (f *fixture) addMiner(t *testing.T, identifier, model string) int64 {
	t.Helper()
	ctx := t.Context()
	var discoveredID, deviceID int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `
		INSERT INTO discovered_device (org_id, device_identifier, manufacturer, model, driver_name, firmware_version, ip_address, port, url_scheme)
		VALUES ($1, $2, 'proto', $3, 'proto', '1.0.0', '10.0.0.1', '80', 'http')
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

func (f *fixture) advanceClock(d time.Duration) { f.clock = f.clock.Add(d) }

func (f *fixture) setReportedVersion(t *testing.T, identifier, version string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		UPDATE discovered_device SET firmware_version = $1
		WHERE org_id = $2 AND device_identifier = $3
	`, version, f.orgID, identifier)
	require.NoError(t, err)
}

func (f *fixture) setStatus(t *testing.T, identifier, status string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		UPDATE device_status SET status = $1::device_status_enum
		WHERE device_id = (SELECT id FROM device WHERE device_identifier = $2)
	`, status, identifier)
	require.NoError(t, err)
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

// reportTelemetry lands a full telemetry sample for the miner.
func (f *fixture) reportTelemetry(t *testing.T, identifier string, hashRateHs, powerW, efficiencyJh, tempC float64) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, power_w, efficiency_jh, temp_c)
		VALUES (now(), $1, $2, $3, $4, $5)
	`, identifier, hashRateHs, powerW, efficiencyJh, tempC)
	require.NoError(t, err)
}

// finishUpdate makes the miner look like it came back from the update on the
// version, online and hashing.
func (f *fixture) finishUpdate(t *testing.T, identifier, version string) {
	t.Helper()
	f.setReportedVersion(t, identifier, version)
	f.setStatus(t, identifier, "ACTIVE")
}

// backdateSends makes every command sent so far look older than the resend
// interval, so the next tick may re-send.
func (f *fixture) backdateSends(t *testing.T) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		UPDATE firmware_rollout_device SET last_sent_at = last_sent_at - INTERVAL '1 hour'
		WHERE last_sent_at IS NOT NULL
	`)
	require.NoError(t, err)
}

// testActor is the operator every test action is attributed to.
var testActor = Actor{Type: ActorTypeUser, ID: 1, Name: "operator"}

// byOperator is an unconditional mutation by testActor.
var byOperator = Mutation{Actor: testActor}

// rigAssignment assigns fileID to the Proto Rig pair; the key is written with
// a different case than the observed identity to exercise the fold.
func rigAssignment(fileID string) Assignment {
	return Assignment{Manufacturer: "Proto", Model: "Rig", FirmwareFileID: fileID}
}

func (f *fixture) apply(t *testing.T, fileID string) Rollout {
	t.Helper()
	started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, testActor, f.channelID, []Assignment{rigAssignment(fileID)}, nil)
	require.NoError(t, err)
	require.Len(t, started, 1)
	return started[0]
}

func (f *fixture) rollout(t *testing.T, id int64) Rollout {
	t.Helper()
	rollouts, _, err := f.svc.ListRollouts(t.Context(), f.orgID, RolloutFilter{})
	require.NoError(t, err)
	for _, r := range rollouts {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("rollout %d not found", id)
	return Rollout{}
}

func (f *fixture) latestRollout(t *testing.T) Rollout {
	t.Helper()
	rollouts, _, err := f.svc.ListRollouts(t.Context(), f.orgID, RolloutFilter{})
	require.NoError(t, err)
	require.NotEmpty(t, rollouts)
	return rollouts[0]
}

// assignedFirmware returns the file id currently carrying the Rig pair's
// assigned checksum, or "" when the pair is unassigned.
func (f *fixture) assignedFirmware(t *testing.T) string {
	t.Helper()
	return f.rigGroup(t).FirmwareFileID
}

// rigGroup returns the Proto Rig model group of the fixture's channel.
func (f *fixture) rigGroup(t *testing.T) ModelGroup {
	t.Helper()
	groups, _, err := f.svc.ListChannelModelGroups(t.Context(), f.orgID, f.channelID, 0, "")
	require.NoError(t, err)
	for _, g := range groups {
		if g.Model == "Rig" {
			return g
		}
	}
	return ModelGroup{}
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

// --- Assertion helpers ---

func batchOf(r Rollout, identifier string) int32 {
	for _, d := range r.Devices {
		if d.DeviceIdentifier == identifier {
			return d.Batch
		}
	}
	return -1
}

func phaseOf(r Rollout, identifier string) string {
	for _, d := range r.Devices {
		if d.DeviceIdentifier == identifier {
			return d.Phase
		}
	}
	return ""
}

func deviceOf(t *testing.T, r Rollout, identifier string) RolloutDevice {
	t.Helper()
	for _, d := range r.Devices {
		if d.DeviceIdentifier == identifier {
			return d
		}
	}
	t.Fatalf("device %s not in rollout %d", identifier, r.ID)
	return RolloutDevice{}
}

func ptr[T any](v T) *T { return &v }
