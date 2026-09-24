package mqttingest

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/block/proto-fleet/server/migrations"
)

type rigConfigStoreFixture struct {
	db      *sql.DB
	queries *sqlc.Queries
	userID  int64
	orgID   int64
	nextRig int
}

type rigConfigTestRig struct {
	id         int64
	identifier string
}

func newRigConfigStoreFixture(t *testing.T) *rigConfigStoreFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	db := dbtest.GetTestDB(t)
	f := &rigConfigStoreFixture{db: db, queries: sqlc.New(db)}
	var err error
	f.orgID, err = f.queries.CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{
		OrgID: uuid.NewString(), Name: "rig config test",
	})
	require.NoError(t, err)
	f.userID, err = f.queries.CreateUser(t.Context(), sqlc.CreateUserParams{
		UserID: uuid.NewString(), Username: uuid.NewString(), PasswordHash: "test", CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	return f
}

func (f *rigConfigStoreFixture) createRig(t *testing.T, manufacturer, status string) rigConfigTestRig {
	t.Helper()
	f.nextRig++
	rig := rigConfigTestRig{identifier: uuid.NewString()}
	discoveredID, err := f.queries.UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID: f.orgID, DeviceIdentifier: rig.identifier,
		Manufacturer: sql.NullString{String: manufacturer, Valid: true},
		Model:        sql.NullString{String: "test", Valid: true},
		IpAddress:    fmt.Sprintf("127.0.0.%d", f.nextRig), Port: "8080", UrlScheme: "http",
		DriverName: "proto", IsActive: true,
	})
	require.NoError(t, err)
	rig.id, err = f.queries.InsertDevice(t.Context(), sqlc.InsertDeviceParams{
		OrgID: f.orgID, DiscoveredDeviceID: discoveredID, DeviceIdentifier: rig.identifier, MacAddress: "00:00:00:00:00:00",
	})
	require.NoError(t, err)
	_, err = f.queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{
		DeviceID: rig.id, PairingStatus: sqlc.PairingStatusEnum(status),
	})
	require.NoError(t, err)
	return rig
}

func (f *rigConfigStoreFixture) request(t *testing.T, identifiers ...string) {
	t.Helper()
	require.NoError(t, f.queries.RequestRigConfigReconciliationForDevices(t.Context(), sqlc.RequestRigConfigReconciliationForDevicesParams{
		OrganizationID: f.orgID, RequestedBy: f.userID, DeviceIdentifiers: identifiers,
	}))
}

func (f *rigConfigStoreFixture) targets(t *testing.T, generation int64) []string {
	t.Helper()
	identifiers, err := f.queries.ListRigConfigReconciliationTargets(t.Context(), sqlc.ListRigConfigReconciliationTargetsParams{
		OrganizationID: f.orgID, DesiredGeneration: generation,
	})
	require.NoError(t, err)
	return identifiers
}

func (f *rigConfigStoreFixture) complete(t *testing.T, generation int64) {
	t.Helper()
	require.NoError(t, f.queries.CompleteRigConfigReconciliation(t.Context(), sqlc.CompleteRigConfigReconciliationParams{
		OrganizationID: f.orgID, EnqueuedGeneration: generation,
	}))
}

func (f *rigConfigStoreFixture) isTargeted(t *testing.T, enqueued, desired int64) bool {
	t.Helper()
	targeted, err := f.queries.IsRigConfigReconciliationTargeted(t.Context(), sqlc.IsRigConfigReconciliationTargetedParams{
		OrganizationID: f.orgID, EnqueuedGeneration: enqueued, DesiredGeneration: desired,
	})
	require.NoError(t, err)
	return targeted
}

func TestRigConfigStore_TargetedRequestsCoalesceAndFilterEligibility(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	bitmain := f.createRig(t, "Bitmain", "PAIRED")
	authNeeded := f.createRig(t, "Proto", "AUTHENTICATION_NEEDED")
	defaultPassword := f.createRig(t, "Proto", "DEFAULT_PASSWORD")
	deleted := f.createRig(t, "Proto", "PAIRED")
	_, err := f.db.ExecContext(t.Context(), "UPDATE device SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1", deleted.id)
	require.NoError(t, err)
	otherOrg, err := f.queries.CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: uuid.NewString(), Name: "other org"})
	require.NoError(t, err)
	otherFixture := &rigConfigStoreFixture{db: f.db, queries: f.queries, orgID: otherOrg}
	foreign := otherFixture.createRig(t, "Proto", "PAIRED")

	// Invalid-only or empty requests must not create even an empty generation.
	f.request(t, bitmain.identifier, authNeeded.identifier, defaultPassword.identifier, deleted.identifier, foreign.identifier, "missing")
	f.request(t)
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows)

	f.request(t, first.identifier, first.identifier, bitmain.identifier, foreign.identifier)
	f.request(t, second.identifier, first.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), claim.DesiredGeneration)
	require.True(t, f.isTargeted(t, claim.EnqueuedGeneration, claim.DesiredGeneration))
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, f.targets(t, claim.DesiredGeneration))

	// A target that is unpaired between request and delivery must be omitted.
	_, err = f.queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: second.id, PairingStatus: "UNPAIRED"})
	require.NoError(t, err)
	require.Equal(t, []string{first.identifier}, f.targets(t, claim.DesiredGeneration))
	f.complete(t, claim.DesiredGeneration)
	require.Empty(t, f.targets(t, claim.DesiredGeneration))
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestRigConfigStore_RequestsDuringClaimSurviveCompletion(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	f.request(t, first.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{first.identifier}, f.targets(t, claim.DesiredGeneration))

	// Refreshing the same device while its older command is being prepared must
	// survive completion just like a request for a newly paired second device.
	f.request(t, first.identifier, second.identifier)
	f.complete(t, claim.DesiredGeneration)
	newer, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), newer.DesiredGeneration)
	require.True(t, f.isTargeted(t, newer.EnqueuedGeneration, newer.DesiredGeneration))
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, f.targets(t, newer.DesiredGeneration))

	// Duplicate completion of the old generation must not clear the newer lease.
	f.complete(t, claim.DesiredGeneration)
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.True(t, f.isTargeted(t, newer.EnqueuedGeneration, newer.DesiredGeneration))
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, f.targets(t, newer.DesiredGeneration))
	f.complete(t, newer.DesiredGeneration)
	require.Empty(t, f.targets(t, newer.DesiredGeneration))
}

func TestRigConfigStore_ConcurrentRequestSurvivesWaitingCompletion(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	f.request(t, first.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)

	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck // May already be committed.
	require.NoError(t, f.queries.WithTx(tx).RequestRigConfigReconciliationForDevices(t.Context(), sqlc.RequestRigConfigReconciliationForDevicesParams{
		OrganizationID: f.orgID, RequestedBy: f.userID, DeviceIdentifiers: []string{first.identifier, second.identifier},
	}))

	completionConn, err := f.db.Conn(t.Context())
	require.NoError(t, err)
	defer func() {
		_ = tx.Rollback()
		_ = completionConn.Close()
	}()
	var completionPID int
	require.NoError(t, completionConn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&completionPID))
	completed := make(chan error, 1)
	go func() {
		completed <- sqlc.New(completionConn).CompleteRigConfigReconciliation(t.Context(), sqlc.CompleteRigConfigReconciliationParams{
			OrganizationID: f.orgID, EnqueuedGeneration: claim.DesiredGeneration,
		})
	}()

	// Hold the newer request uncommitted until completion's older statement
	// snapshot has actually started and is waiting on the organization row.
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), "SELECT cardinality(pg_blocking_pids($1)) > 0", completionPID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	select {
	case err := <-completed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("completion did not finish after the newer request committed")
	}
	newer, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), newer.DesiredGeneration)
	require.True(t, f.isTargeted(t, newer.EnqueuedGeneration, newer.DesiredGeneration))
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, f.targets(t, newer.DesiredGeneration))
}

func TestRigConfigStore_MigrationPreservesPendingWorkAcrossUpgradeAndDowngrade(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	for range 3 {
		require.NoError(t, f.queries.RequestRigConfigReconciliation(t.Context(), sqlc.RequestRigConfigReconciliationParams{
			OrganizationID: f.orgID, RequestedBy: f.userID,
		}))
	}
	f.complete(t, 1)
	// Include pending targeted work: a downgrade must preserve its generation
	// for the older organization-wide worker even though target scope is removed.
	f.request(t, rig.identifier)
	down, err := migrations.Migrations.ReadFile("000151_curtailment_rig_config_targets.down.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	var desired, enqueued int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		SELECT desired_generation, enqueued_generation
		FROM curtailment_rig_config_reconciliation WHERE organization_id = $1
	`, f.orgID).Scan(&desired, &enqueued))
	require.Equal(t, int64(4), desired)
	require.Equal(t, int64(1), enqueued)

	up, err := migrations.Migrations.ReadFile("000151_curtailment_rig_config_targets.up.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(up))
	require.NoError(t, err)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, desired, claim.DesiredGeneration)
	require.Equal(t, enqueued, claim.EnqueuedGeneration)
	require.False(t, f.isTargeted(t, enqueued, desired), "all existing pending generations must retain full scope after upgrade")
	f.complete(t, claim.DesiredGeneration)
	f.request(t, rig.identifier)
	targeted, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.True(t, f.isTargeted(t, targeted.EnqueuedGeneration, targeted.DesiredGeneration))
	require.Equal(t, []string{rig.identifier}, f.targets(t, targeted.DesiredGeneration))
}

func TestRigConfigStore_LegacyWritersAndClaimsRemainCompatibleDuringHAUpgrade(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	f.request(t, first.identifier)
	f.complete(t, 1)

	// The old settings writer only increments the original organization row.
	// A later device request must not disguise this unmarked full generation.
	_, err := f.db.ExecContext(t.Context(), `
		UPDATE curtailment_rig_config_reconciliation
		SET desired_generation = desired_generation + 1, retry_at = CURRENT_TIMESTAMP
		WHERE organization_id = $1
	`, f.orgID)
	require.NoError(t, err)
	f.request(t, first.identifier)

	// Older sqlc bindings use RETURNING * with exactly these nine scan fields.
	// Execute their original shape against the upgraded schema to detect any
	// incompatible addition/reordering of the organization outbox's columns.
	var legacy sqlc.CurtailmentRigConfigReconciliation
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		WITH candidate AS (
			SELECT organization_id
			FROM curtailment_rig_config_reconciliation
			WHERE desired_generation > enqueued_generation
			  AND retry_at <= CURRENT_TIMESTAMP
			  AND (lease_expires_at IS NULL OR lease_expires_at <= CURRENT_TIMESTAMP)
			ORDER BY retry_at, organization_id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE curtailment_rig_config_reconciliation reconciliation
		SET lease_expires_at = CURRENT_TIMESTAMP + INTERVAL '90 seconds'
		FROM candidate
		WHERE reconciliation.organization_id = candidate.organization_id
		RETURNING reconciliation.*
	`).Scan(&legacy.OrganizationID, &legacy.RequestedBy, &legacy.DesiredGeneration,
		&legacy.EnqueuedGeneration, &legacy.RetryAt, &legacy.LeaseExpiresAt,
		&legacy.LastError, &legacy.CreatedAt, &legacy.UpdatedAt))
	require.Equal(t, int64(3), legacy.DesiredGeneration)
	require.False(t, f.isTargeted(t, legacy.EnqueuedGeneration, legacy.DesiredGeneration))

	// An older worker acknowledges the full pass without knowing either new
	// table. Its leftover targets and markers must not contaminate the next pass.
	_, err = f.db.ExecContext(t.Context(), `
		UPDATE curtailment_rig_config_reconciliation
		SET enqueued_generation = desired_generation, lease_expires_at = NULL
		WHERE organization_id = $1
	`, f.orgID)
	require.NoError(t, err)
	f.request(t, second.identifier)
	current, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.True(t, f.isTargeted(t, current.EnqueuedGeneration, current.DesiredGeneration))
	targets, err := f.queries.ListRigConfigReconciliationTargets(t.Context(), sqlc.ListRigConfigReconciliationTargetsParams{
		OrganizationID: f.orgID, EnqueuedGeneration: current.EnqueuedGeneration, DesiredGeneration: current.DesiredGeneration,
	})
	require.NoError(t, err)
	require.Equal(t, []string{second.identifier}, targets)
	f.complete(t, current.DesiredGeneration)
	require.Empty(t, f.targets(t, current.DesiredGeneration))
	var markerCount int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		SELECT count(*) FROM curtailment_rig_config_target_generation WHERE organization_id = $1
	`, f.orgID).Scan(&markerCount))
	require.Zero(t, markerCount, "current workers also clean up markers acknowledged by older workers")
}

func TestRigConfigStore_SettingsChangesPreserveFullScopeAcrossTargetRequests(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	f.request(t, rig.identifier)
	targeted, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)

	created, err := f.queries.InsertMQTTSourceConfig(t.Context(), sqlc.InsertMQTTSourceConfigParams{
		OrganizationID: f.orgID, ServiceUserID: f.userID, SourceName: "test", Topic: "test/curtailment",
		BrokerPrimaryHost: "primary.example", BrokerSecondaryHost: "secondary.example",
		BrokerTransport: "tcp", PayloadFormat: "target_timestamp", Enabled: true,
		MqttUsername: "test", MqttPasswordEnc: "enc:secret",
	})
	require.NoError(t, err)
	f.request(t, rig.identifier)
	f.complete(t, targeted.DesiredGeneration)
	full, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.False(t, f.isTargeted(t, full.EnqueuedGeneration, full.DesiredGeneration))
	require.Equal(t, int64(3), full.DesiredGeneration)
	f.complete(t, full.DesiredGeneration)

	changes := []struct {
		name  string
		apply func() error
	}{
		{"update", func() error {
			_, err := f.queries.UpdateMQTTSourceConfig(t.Context(), sqlc.UpdateMQTTSourceConfigParams{
				ID: created.ID, OrganizationID: f.orgID, ServiceUserID: f.userID, SourceName: "updated", Topic: "test/new",
				BrokerPrimaryHost: "primary.example", BrokerSecondaryHost: "secondary.example",
				BrokerTransport: "tcp", PayloadFormat: "target_timestamp",
				MqttUsername: "test", MqttPasswordEnc: "enc:secret",
			})
			return err
		}},
		{"disable", func() error {
			_, err := f.queries.SetMQTTSourceConfigEnabled(t.Context(), sqlc.SetMQTTSourceConfigEnabledParams{ID: created.ID, OrganizationID: f.orgID, Enabled: false})
			return err
		}},
		{"delete", func() error {
			_, err := f.queries.DeleteDisabledMQTTSourceConfigByOrg(t.Context(), sqlc.DeleteDisabledMQTTSourceConfigByOrgParams{ID: created.ID, OrganizationID: f.orgID})
			return err
		}},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			require.NoError(t, change.apply())
			f.request(t, rig.identifier)
			claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
			require.NoError(t, err)
			require.False(t, f.isTargeted(t, claim.EnqueuedGeneration, claim.DesiredGeneration))
			f.complete(t, claim.DesiredGeneration)
		})
	}
	// Once a settings generation is completed, pairing is targeted again.
	f.request(t, rig.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.True(t, f.isTargeted(t, claim.EnqueuedGeneration, claim.DesiredGeneration))
}

func TestRigConfigStore_RetryAndTerminalFailureRetainOnlyFailedTargets(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	f.request(t, first.identifier, second.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.queries.RetryRigConfigReconciliation(t.Context(), sqlc.RetryRigConfigReconciliationParams{
		OrganizationID: f.orgID, DesiredGeneration: claim.DesiredGeneration, LastError: sql.NullString{String: "transient", Valid: true},
	}))
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, f.targets(t, claim.DesiredGeneration))
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows, "retry backoff must apply")
	f.complete(t, claim.DesiredGeneration)

	require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{
		OrganizationID: f.orgID, DeviceID: second.id,
	}))
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows, "terminal failures must back off")
	_, err = f.db.ExecContext(t.Context(), "UPDATE curtailment_rig_config_reconciliation SET retry_at = CURRENT_TIMESTAMP WHERE organization_id = $1", f.orgID)
	require.NoError(t, err)
	terminal, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.True(t, f.isTargeted(t, terminal.EnqueuedGeneration, terminal.DesiredGeneration))
	require.Equal(t, []string{second.identifier}, f.targets(t, terminal.DesiredGeneration))
	f.complete(t, terminal.DesiredGeneration)

	_, err = f.queries.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: second.id, PairingStatus: "UNPAIRED"})
	require.NoError(t, err)
	require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{
		OrganizationID: f.orgID, DeviceID: second.id,
	}))
	var desiredGeneration int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT desired_generation FROM curtailment_rig_config_reconciliation WHERE organization_id = $1", f.orgID).Scan(&desiredGeneration))
	require.Equal(t, terminal.DesiredGeneration, desiredGeneration, "terminal failures after unpairing must not reopen work")
}
