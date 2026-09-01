package rollout

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/migrations"
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

func (f *fakeDispatcher) sentIdentifiers() []string {
	var all []string
	for _, batch := range f.sent {
		all = append(all, batch...)
	}
	return all
}

// fakeFirmwareFiles serves one firmware file: id "fw-2" targeting Rig 2.0.0.
type fakeFirmwareFiles struct{}

func (fakeFirmwareFiles) GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error) {
	if fileID != "fw-2" {
		return files.FirmwareMetadata{}, fmt.Errorf("unknown firmware file %q", fileID)
	}
	return files.FirmwareMetadata{TargetModel: "Rig", FirmwareVersion: "2.0.0"}, nil
}

// newRolloutTestDB creates a scratch database with all migrations applied.
// Requires DB_PASSWORD (and friends) like the other DB integration tests.
func newRolloutTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("DB_PASSWORD") == "" {
		t.Skip("DB_PASSWORD is required for rollout integration tests")
	}

	cli := struct {
		DB db.Config `envprefix:"DB_" embed:""`
	}{}
	parser, err := kong.New(&cli)
	require.NoError(t, err)
	_, err = parser.Parse(nil)
	require.NoError(t, err)

	adminConfig := cli.DB
	adminConfig.Name = "postgres"
	admin, err := db.ConnectToDatabase(&adminConfig)
	require.NoError(t, err)
	defer admin.Close()

	suffix := make([]byte, 6)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	dbName := "fleet_rollout_" + hex.EncodeToString(suffix)
	_, err = admin.ExecContext(t.Context(), `CREATE DATABASE "`+dbName+`"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.Background() // nolint:usetesting // runs after the test context is done
		cleanup, cleanupErr := db.ConnectToDatabase(&adminConfig)
		assert.NoError(t, cleanupErr)
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(ctx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		_, cleanupErr = cleanup.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+dbName+`"`)
		assert.NoError(t, cleanupErr)
	})

	config := cli.DB
	config.Name = dbName
	conn, err := db.ConnectToDatabase(&config)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })

	source, err := iofs.New(migrations.Migrations, ".")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(conn, &postgres.Config{})
	require.NoError(t, err)
	migration, err := migrate.NewWithInstance("migrations", source, dbName, driver)
	require.NoError(t, err)
	// Do not call migration.Close: it would close conn, which the test keeps
	// using. Cleanup above closes conn.
	require.NoError(t, migration.Up())
	return conn
}

type rolloutFixture struct {
	conn       *sql.DB
	svc        *Service
	dispatcher *fakeDispatcher
	orgID      int64
	laneID     int64
}

// newRolloutFixture provisions an org, a lane, and miners named miner-0..n-1
// (model Rig, firmware 1.0.0), all members of the lane.
func newRolloutFixture(t *testing.T, minerCount int) *rolloutFixture {
	t.Helper()
	conn := newRolloutTestDB(t)
	ctx := t.Context()

	var orgID int64
	require.NoError(t, conn.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name)
		VALUES ('rollout-test-org', 'Rollout test org')
		RETURNING id
	`).Scan(&orgID))

	for i := range minerCount {
		identifier := fmt.Sprintf("miner-%d", i)
		var discoveredID int64
		require.NoError(t, conn.QueryRowContext(ctx, `
			INSERT INTO discovered_device (org_id, device_identifier, model, driver_name, firmware_version, ip_address, port, url_scheme)
			VALUES ($1, $2, 'Rig', 'proto', '1.0.0', '10.0.0.1', '80', 'http')
			RETURNING id
		`, orgID, identifier).Scan(&discoveredID))
		_, err := conn.ExecContext(ctx, `
			INSERT INTO device (device_identifier, mac_address, org_id, discovered_device_id)
			VALUES ($1, $2, $3, $4)
		`, identifier, fmt.Sprintf("00:00:00:00:00:%02x", i), orgID, discoveredID)
		require.NoError(t, err)
	}

	dispatcher := &fakeDispatcher{}
	svc := NewService(sqlstores.NewSQLRolloutLaneStore(conn), dispatcher, fakeFirmwareFiles{})

	lane, err := svc.CreateLane(ctx, orgID, "Test channel")
	require.NoError(t, err)
	identifiers := make([]string, 0, minerCount)
	for i := range minerCount {
		identifiers = append(identifiers, fmt.Sprintf("miner-%d", i))
	}
	_, err = svc.UpdateMembers(ctx, orgID, lane.ID, identifiers, nil)
	require.NoError(t, err)

	return &rolloutFixture{conn: conn, svc: svc, dispatcher: dispatcher, orgID: orgID, laneID: lane.ID}
}

func (f *rolloutFixture) setReportedVersion(t *testing.T, identifier, version string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		UPDATE discovered_device SET firmware_version = $1
		WHERE org_id = $2 AND device_identifier = $3
	`, version, f.orgID, identifier)
	require.NoError(t, err)
}

func (f *rolloutFixture) activeRollout(t *testing.T) Rollout {
	t.Helper()
	rollouts, err := f.svc.ListRollouts(t.Context(), f.orgID, f.laneID)
	require.NoError(t, err)
	for _, r := range rollouts {
		if r.Status == StatusActive {
			return r
		}
	}
	t.Fatal("no active rollout")
	return Rollout{}
}

func TestPilotRolloutGatesRestBehindReview(t *testing.T) {
	f := newRolloutFixture(t, 3)
	ctx := t.Context()

	started, err := f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID,
		[]Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}},
		RolloutOptions{Method: MethodPilot, PilotCount: 1})
	require.NoError(t, err)
	require.Len(t, started, 1)
	assert.Equal(t, MethodPilot, started[0].Method)
	assert.Equal(t, StagePilot, started[0].Stage)
	assert.Equal(t, int32(1), started[0].PilotCount)

	var cohort []string
	for _, d := range started[0].Devices {
		if d.InPilotCohort {
			cohort = append(cohort, d.DeviceIdentifier)
		}
	}
	require.Equal(t, []string{"miner-0"}, cohort, "cohort is the first mismatched miner by identifier")

	// Pilot stage: only the cohort is dispatched to, even across ticks.
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

	// The cohort reporting the target parks the rollout at the gate
	// instead of completing or advancing on its own.
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	gated := f.activeRollout(t)
	assert.Equal(t, StageAwaitingReview, gated.Stage)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "gate holds: no dispatch to the rest")

	// Continue releases the gate; the rest is dispatched on the next tick.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, gated.ID)
	require.NoError(t, err)
	assert.Equal(t, StageRest, continued.Stage)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, f.dispatcher.sentIdentifiers()[1:])

	// Everyone on target completes the rollout, same as immediate ones.
	f.setReportedVersion(t, "miner-1", "2.0.0")
	f.setReportedVersion(t, "miner-2", "2.0.0")
	f.svc.EnforceTick(ctx)
	rollouts, err := f.svc.ListRollouts(ctx, f.orgID, f.laneID)
	require.NoError(t, err)
	require.Len(t, rollouts, 1)
	assert.Equal(t, StatusCompleted, rollouts[0].Status)
}

func TestPilotCountIsCappedAtMismatchedMembers(t *testing.T) {
	f := newRolloutFixture(t, 2)

	started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, 1, f.laneID,
		[]Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}},
		RolloutOptions{Method: MethodPilot, PilotCount: 10})
	require.NoError(t, err)
	require.Len(t, started, 1)
	assert.Equal(t, int32(2), started[0].PilotCount)
	for _, d := range started[0].Devices {
		assert.True(t, d.InPilotCohort)
	}
}

func TestContinueRolloutRejectsUngatedRollouts(t *testing.T) {
	f := newRolloutFixture(t, 2)
	ctx := t.Context()

	// Immediate rollout: never at the gate.
	started, err := f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID,
		[]Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}}, RolloutOptions{})
	require.NoError(t, err)
	require.Len(t, started, 1)
	assert.Equal(t, MethodImmediate, started[0].Method)
	assert.Equal(t, StageRest, started[0].Stage)

	_, err = f.svc.ContinueRollout(ctx, f.orgID, started[0].ID)
	assert.ErrorContains(t, err, "not awaiting review")
}

func TestPilotRolloutValidation(t *testing.T) {
	f := newRolloutFixture(t, 1)

	_, err := f.svc.ApplyFirmware(t.Context(), f.orgID, 1, f.laneID,
		[]Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}},
		RolloutOptions{Method: MethodPilot})
	assert.ErrorContains(t, err, "pilot count")

	_, err = f.svc.ApplyFirmware(t.Context(), f.orgID, 1, f.laneID,
		[]Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}},
		RolloutOptions{Method: "canary", PilotCount: 1})
	assert.ErrorContains(t, err, "unknown rollout method")
}
