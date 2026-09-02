package rollout

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
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

func (f *fakeActivity) types() []string {
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.Type)
	}
	return out
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
	activity   *fakeActivity
	clock      time.Time
	orgID      int64
	laneID     int64
}

// newRolloutFixture provisions an org, a lane, and miners named miner-0..n-1
// (model Rig, firmware 1.0.0, status ACTIVE, hashing 100 H/s), all members
// of the lane. The service clock is frozen and advanced with advanceClock.
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
		var discoveredID, deviceID int64
		require.NoError(t, conn.QueryRowContext(ctx, `
			INSERT INTO discovered_device (org_id, device_identifier, model, driver_name, firmware_version, ip_address, port, url_scheme)
			VALUES ($1, $2, 'Rig', 'proto', '1.0.0', '10.0.0.1', '80', 'http')
			RETURNING id
		`, orgID, identifier).Scan(&discoveredID))
		require.NoError(t, conn.QueryRowContext(ctx, `
			INSERT INTO device (device_identifier, mac_address, org_id, discovered_device_id)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, identifier, fmt.Sprintf("00:00:00:00:00:%02x", i), orgID, discoveredID).Scan(&deviceID))
		_, err := conn.ExecContext(ctx, `INSERT INTO device_status (device_id, status) VALUES ($1, 'ACTIVE')`, deviceID)
		require.NoError(t, err)
	}

	f := &rolloutFixture{
		conn:       conn,
		dispatcher: &fakeDispatcher{},
		activity:   &fakeActivity{},
		clock:      time.Now(),
		orgID:      orgID,
	}
	f.svc = NewService(sqlstores.NewSQLRolloutLaneStore(conn), f.dispatcher, fakeFirmwareFiles{}, f.activity)
	f.svc.now = func() time.Time { return f.clock }

	for i := range minerCount {
		f.reportHashrate(t, fmt.Sprintf("miner-%d", i), 100)
	}

	lane, err := f.svc.CreateLane(ctx, orgID, "Test channel")
	require.NoError(t, err)
	identifiers := make([]string, 0, minerCount)
	for i := range minerCount {
		identifiers = append(identifiers, fmt.Sprintf("miner-%d", i))
	}
	_, err = f.svc.UpdateMembers(ctx, orgID, lane.ID, identifiers, nil)
	require.NoError(t, err)
	f.laneID = lane.ID
	return f
}

func (f *rolloutFixture) advanceClock(d time.Duration) { f.clock = f.clock.Add(d) }

func (f *rolloutFixture) setReportedVersion(t *testing.T, identifier, version string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		UPDATE discovered_device SET firmware_version = $1
		WHERE org_id = $2 AND device_identifier = $3
	`, version, f.orgID, identifier)
	require.NoError(t, err)
}

func (f *rolloutFixture) setStatus(t *testing.T, identifier, status string) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		UPDATE device_status SET status = $1::device_status_enum
		WHERE device_id = (SELECT id FROM device WHERE device_identifier = $2)
	`, status, identifier)
	require.NoError(t, err)
}

// reportHashrate lands a fresh hashrate sample for the miner.
func (f *rolloutFixture) reportHashrate(t *testing.T, identifier string, hashRateHs float64) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `
		INSERT INTO device_metrics (time, device_identifier, hash_rate_hs)
		VALUES (now(), $1, $2)
	`, identifier, hashRateHs)
	require.NoError(t, err)
}

// finishUpdate makes the miner look like it came back from the update on the
// version, online and hashing.
func (f *rolloutFixture) finishUpdate(t *testing.T, identifier, version string) {
	t.Helper()
	f.setReportedVersion(t, identifier, version)
	f.setStatus(t, identifier, "ACTIVE")
}

func (f *rolloutFixture) apply(t *testing.T, fileID string, opts RolloutOptions) Rollout {
	t.Helper()
	started, err := f.svc.ApplyFirmware(t.Context(), f.orgID, 1, f.laneID,
		[]Assignment{{Model: "Rig", FirmwareFileID: fileID}}, opts)
	require.NoError(t, err)
	require.Len(t, started, 1)
	return started[0]
}

func (f *rolloutFixture) rollout(t *testing.T, id int64) Rollout {
	t.Helper()
	rollouts, err := f.svc.ListRollouts(t.Context(), f.orgID, f.laneID)
	require.NoError(t, err)
	for _, r := range rollouts {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("rollout %d not found", id)
	return Rollout{}
}

func (f *rolloutFixture) assignedFirmware(t *testing.T) string {
	t.Helper()
	lane, err := f.svc.getLane(t.Context(), f.orgID, f.laneID)
	require.NoError(t, err)
	for _, g := range lane.ModelGroups {
		if g.Model == "Rig" {
			return g.FirmwareFileID
		}
	}
	return ""
}

func batchOf(r Rollout, identifier string) int32 {
	for _, d := range r.Devices {
		if d.DeviceIdentifier == identifier {
			return d.Batch
		}
	}
	return -1
}

func stateOf(r Rollout, identifier string) string {
	for _, d := range r.Devices {
		if d.DeviceIdentifier == identifier {
			return d.State
		}
	}
	return ""
}

func TestPilotRolloutGatesRestBehindReview(t *testing.T) {
	f := newRolloutFixture(t, 3)
	ctx := t.Context()

	started := f.apply(t, "fw-2", RolloutOptions{Method: MethodPilot, BatchSize: 1})
	assert.Equal(t, MethodPilot, started.Method)
	assert.Equal(t, StageBatch, started.Stage)
	assert.Equal(t, int32(1), started.BatchSize)
	assert.Equal(t, int32(1), started.BatchCount)
	assert.Equal(t, int32(1), batchOf(started, "miner-0"), "first mismatched miner by identifier forms the pilot batch")
	assert.Equal(t, int32(0), batchOf(started, "miner-1"))
	assert.Equal(t, []string{EventRolloutStarted}, f.activity.types())

	// Batch stage: only the pilot is dispatched to, even across ticks.
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())

	// Reporting the version is not enough: the miner has to come back online.
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.setStatus(t, "miner-0", "OFFLINE")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageBatch, f.rollout(t, started.ID).Stage)
	assert.Equal(t, DeviceStateVerifying, stateOf(f.rollout(t, started.ID), "miner-0"))

	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Equal(t, DeviceStateUpdated, stateOf(gated, "miner-0"))
	assert.Contains(t, f.activity.types(), EventRolloutReviewReady)
	require.NotNil(t, gated.Evidence)
	assert.Equal(t, int32(1), gated.Evidence.Verified)
	assert.Equal(t, "Manual review", gated.Evidence.HoldReason)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers(), "gate holds: no dispatch to the rest")

	// Continue releases the gate; the rest is dispatched on the next tick.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, gated.ID)
	require.NoError(t, err)
	assert.Equal(t, StageRest, continued.Stage)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-1", "miner-2"}, f.dispatcher.sentIdentifiers()[1:])

	f.finishUpdate(t, "miner-1", "2.0.0")
	f.finishUpdate(t, "miner-2", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
	assert.Contains(t, f.activity.types(), EventRolloutCompleted)
}

func TestBatchesGateAfterEveryBatch(t *testing.T) {
	f := newRolloutFixture(t, 4)
	ctx := t.Context()

	started := f.apply(t, "fw-2", RolloutOptions{Method: MethodBatches, BatchSize: 2})
	assert.Equal(t, int32(2), started.BatchCount)
	assert.Equal(t, int32(1), batchOf(started, "miner-1"))
	assert.Equal(t, int32(2), batchOf(started, "miner-2"))

	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.finishUpdate(t, "miner-1", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	// Continuing after the first batch starts the second, not the rest.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, StageBatch, continued.Stage)
	assert.Equal(t, int32(1), continued.CurrentBatch)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-2", "miner-3"}, f.dispatcher.sentIdentifiers()[2:])
	f.finishUpdate(t, "miner-2", "2.0.0")
	f.finishUpdate(t, "miner-3", "2.0.0")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	// Continuing after the last batch enters the rest stage, which has
	// nothing left to do and completes.
	continued, err = f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, StageRest, continued.Stage)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
}

func TestAutoAdvanceWaitsForStabilizationAndHoldsOnDegradedHashrate(t *testing.T) {
	f := newRolloutFixture(t, 3)
	ctx := t.Context()

	started := f.apply(t, "fw-2", RolloutOptions{
		Method: MethodBatches, BatchSize: 1,
		AutoAdvance: true, MaxHashrateDropPercent: 10, StabilizationSeconds: 60,
	})
	assert.Equal(t, int32(3), started.BatchCount)

	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	require.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Equal(t, "Stabilizing", gated.Evidence.HoldReason)
	// stage_changed_at is stamped by the database clock, which runs a hair
	// ahead of the frozen service clock.
	assert.InDelta(t, 60, gated.Evidence.StabilizationRemainingSeconds, 1)

	// Still within the stabilization window: holds even though healthy.
	f.advanceClock(30 * time.Second)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StageAwaitingReview, f.rollout(t, started.ID).Stage)

	// Window elapsed, hashrate steady: advances on its own to batch 2.
	f.advanceClock(31 * time.Second)
	f.svc.EnforceTick(ctx)
	advanced := f.rollout(t, started.ID)
	assert.Equal(t, StageBatch, advanced.Stage)
	assert.Equal(t, int32(1), advanced.CurrentBatch)
	var autoContinued bool
	for _, e := range f.activity.events {
		if e.Type == EventRolloutContinued && e.ActorType == activitymodels.ActorSystem {
			autoContinued = true
		}
	}
	assert.True(t, autoContinued, "auto-advance is recorded as a system continue")

	// Batch 2 comes back hashing at half its baseline: evidence is degraded,
	// so the gate holds for a human even after the stabilization window.
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-1", "2.0.0")
	f.reportHashrate(t, "miner-1", 50)
	f.svc.EnforceTick(ctx)
	f.advanceClock(2 * time.Minute)
	f.svc.EnforceTick(ctx)
	held := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, held.Stage)
	assert.Equal(t, int32(1), held.CurrentBatch)
	require.NotNil(t, held.Evidence)
	assert.True(t, held.Evidence.HasHashrateEvidence)
	assert.InDelta(t, -50, held.Evidence.HashrateChangePercent, 0.01)
	assert.Contains(t, held.Evidence.HoldReason, "Hashrate down 50.0%")
	assert.False(t, held.Evidence.ReadyToAdvance)

	// A human can still continue past a degraded gate.
	continued, err := f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(2), continued.CurrentBatch)
}

func TestAutoAdvanceHoldsOnMissingEvidence(t *testing.T) {
	f := newRolloutFixture(t, 2)
	ctx := t.Context()

	started := f.apply(t, "fw-2", RolloutOptions{Method: MethodPilot, BatchSize: 1, AutoAdvance: true})
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	// Drop the miner's samples: it has a baseline hashrate (snapshotted on
	// the rollout) but no current one.
	_, err := f.conn.ExecContext(ctx, `DELETE FROM device_metrics WHERE device_identifier = 'miner-0'`)
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	f.svc.EnforceTick(ctx)
	gated := f.rollout(t, started.ID)
	assert.Equal(t, StageAwaitingReview, gated.Stage)
	assert.Contains(t, gated.Evidence.HoldReason, "No recent hashrate sample")
}

func TestDoneIsRelativeToBaselineHealth(t *testing.T) {
	f := newRolloutFixture(t, 2)
	ctx := t.Context()
	// miner-1 was not hashing before the update; miner-0 was.
	f.setStatus(t, "miner-1", "NEEDS_MINING_POOL")

	started := f.apply(t, "fw-2", RolloutOptions{})
	f.svc.EnforceTick(ctx)
	f.setReportedVersion(t, "miner-0", "2.0.0")
	f.setStatus(t, "miner-0", "NEEDS_MINING_POOL")
	f.setReportedVersion(t, "miner-1", "2.0.0")
	f.svc.EnforceTick(ctx)

	r := f.rollout(t, started.ID)
	assert.Equal(t, StatusActive, r.Status, "miner-0 used to hash and does not yet: not done")
	assert.Equal(t, DeviceStateVerifying, stateOf(r, "miner-0"))
	assert.Equal(t, DeviceStateUpdated, stateOf(r, "miner-1"), "miner-1 is as healthy as before")

	f.setStatus(t, "miner-0", "ACTIVE")
	f.svc.EnforceTick(ctx)
	assert.Equal(t, StatusCompleted, f.rollout(t, started.ID).Status)
}

func TestPauseHoldsEnforcementUntilResumed(t *testing.T) {
	f := newRolloutFixture(t, 2)
	ctx := t.Context()

	started := f.apply(t, "fw-2", RolloutOptions{})
	paused, err := f.svc.PauseRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	require.NotNil(t, paused.PausedAt)
	f.svc.EnforceTick(ctx)
	assert.Empty(t, f.dispatcher.sentIdentifiers(), "paused: nothing is sent")

	_, err = f.svc.PauseRollout(ctx, f.orgID, started.ID)
	assert.ErrorContains(t, err, "already paused")

	resumed, err := f.svc.ResumeRollout(ctx, f.orgID, started.ID)
	require.NoError(t, err)
	assert.Nil(t, resumed.PausedAt)
	f.svc.EnforceTick(ctx)
	assert.ElementsMatch(t, []string{"miner-0", "miner-1"}, f.dispatcher.sentIdentifiers())
	assert.Contains(t, f.activity.types(), EventRolloutPaused)
	assert.Contains(t, f.activity.types(), EventRolloutResumed)
}

func TestAbortRestoresPreviousAssignment(t *testing.T) {
	f := newRolloutFixture(t, 2)
	ctx := t.Context()

	// Settle the group on fw-1 first so there is a previous assignment.
	first := f.apply(t, "fw-1", RolloutOptions{})
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "1.5.0")
	f.finishUpdate(t, "miner-1", "1.5.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, StatusCompleted, f.rollout(t, first.ID).Status)

	second := f.apply(t, "fw-2", RolloutOptions{Method: MethodPilot, BatchSize: 1})
	assert.Equal(t, "fw-1", second.PreviousFirmwareFileID)
	assert.Equal(t, "1.5.0", second.PreviousFirmwareVersion)
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, StageAwaitingReview, f.rollout(t, second.ID).Stage)

	result, err := f.svc.AbortRollout(ctx, f.orgID, 1, second.ID)
	require.NoError(t, err)
	assert.True(t, result.RestoredPrevious)
	assert.Equal(t, StatusCanceled, result.Rollout.Status)
	assert.Equal(t, CancelReasonAborted, result.Rollout.CancelReason)
	assert.Equal(t, "fw-1", f.assignedFirmware(t))
	// The pilot miner is on 2.0.0, so a rollout back to 1.5.0 starts for it.
	require.Len(t, result.Started, 1)
	assert.Equal(t, "1.5.0", result.Started[0].FirmwareVersion)
	assert.Equal(t, MethodImmediate, result.Started[0].Method)
	assert.Contains(t, f.activity.types(), EventRolloutAborted)

	f.svc.EnforceTick(ctx)
	assert.Equal(t, "miner-0", f.dispatcher.sentIdentifiers()[len(f.dispatcher.sentIdentifiers())-1])

	_, err = f.svc.AbortRollout(ctx, f.orgID, 1, second.ID)
	assert.ErrorContains(t, err, "not active")
}

func TestAbortClearsAssignmentWithoutPrevious(t *testing.T) {
	f := newRolloutFixture(t, 2)
	ctx := t.Context()

	started := f.apply(t, "fw-2", RolloutOptions{Method: MethodPilot, BatchSize: 1})
	result, err := f.svc.AbortRollout(ctx, f.orgID, 1, started.ID)
	require.NoError(t, err)
	assert.False(t, result.RestoredPrevious)
	assert.Empty(t, result.Started)
	assert.Equal(t, "", f.assignedFirmware(t), "no previous assignment: cleared so enforcement does not restart it")

	// Nothing to enforce any more.
	f.svc.EnforceTick(ctx)
	assert.Empty(t, f.dispatcher.sentIdentifiers())
}

func TestSupersededRolloutsRecordTheReason(t *testing.T) {
	f := newRolloutFixture(t, 1)
	ctx := t.Context()

	first := f.apply(t, "fw-1", RolloutOptions{})
	f.apply(t, "fw-2", RolloutOptions{})
	assert.Equal(t, CancelReasonSuperseded, f.rollout(t, first.ID).CancelReason)

	_, err := f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID, []Assignment{{Model: "Rig", FirmwareFileID: ""}}, RolloutOptions{})
	require.NoError(t, err)
	rollouts, err := f.svc.ListRollouts(ctx, f.orgID, f.laneID)
	require.NoError(t, err)
	assert.Equal(t, CancelReasonCleared, rollouts[0].CancelReason)
}

func TestRolloutOptionValidation(t *testing.T) {
	f := newRolloutFixture(t, 1)
	ctx := t.Context()
	assignments := []Assignment{{Model: "Rig", FirmwareFileID: "fw-2"}}

	_, err := f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID, assignments, RolloutOptions{Method: MethodPilot})
	assert.ErrorContains(t, err, "batch size")
	_, err = f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID, assignments, RolloutOptions{Method: MethodBatches})
	assert.ErrorContains(t, err, "batch size")
	_, err = f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID, assignments, RolloutOptions{Method: "canary", BatchSize: 1})
	assert.ErrorContains(t, err, "unknown rollout method")
	_, err = f.svc.ApplyFirmware(ctx, f.orgID, 1, f.laneID, assignments, RolloutOptions{Method: MethodPilot, BatchSize: 1, MaxHashrateDropPercent: 150})
	assert.ErrorContains(t, err, "between 0 and 100")

	started := f.apply(t, "fw-2", RolloutOptions{Method: MethodPilot, BatchSize: 10})
	assert.Equal(t, int32(1), started.BatchSize, "batch size is capped at the mismatched member count")
	_, err = f.svc.ContinueRollout(ctx, f.orgID, started.ID)
	assert.ErrorContains(t, err, "not awaiting review")
}
