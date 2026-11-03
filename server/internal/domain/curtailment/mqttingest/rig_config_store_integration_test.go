package mqttingest

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
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
		ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
		OrganizationID:       f.orgID, RequestedBy: f.userID, DeviceIdentifiers: identifiers,
	}))
}

func (f *rigConfigStoreFixture) targets(t *testing.T, generation int64) []string {
	t.Helper()
	identifiers, err := f.queries.ListRigConfigReconciliationTargets(t.Context(), sqlc.ListRigConfigReconciliationTargetsParams{
		ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
		OrganizationID:       f.orgID, DesiredGeneration: generation,
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
	var currentDesired, fullReconcileGeneration int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `
		SELECT desired_generation, full_reconcile_generation
		FROM curtailment_rig_config_reconciliation WHERE organization_id = $1
	`, f.orgID).Scan(&currentDesired, &fullReconcileGeneration))
	require.Equal(t, desired, currentDesired)
	return fullReconcileGeneration <= enqueued
}

func TestRigConfigStore_TargetedRequestsCoalesceAndFilterEligibility(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	bitmain := f.createRig(t, "Bitmain", "PAIRED")
	authNeeded := f.createRig(t, "Proto", "AUTHENTICATION_NEEDED")
	defaultPassword := f.createRig(t, "Proto", "DEFAULT_PASSWORD")
	container := f.createRig(t, "Proto", "PAIRED")
	_, err := f.db.ExecContext(t.Context(), `UPDATE discovered_device SET device_class = $1 WHERE device_identifier = $2`, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, container.identifier)
	require.NoError(t, err)
	deleted := f.createRig(t, "Proto", "PAIRED")
	_, err = f.db.ExecContext(t.Context(), "UPDATE device SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1", deleted.id)
	require.NoError(t, err)
	otherOrg, err := f.queries.CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: uuid.NewString(), Name: "other org"})
	require.NoError(t, err)
	otherFixture := &rigConfigStoreFixture{db: f.db, queries: f.queries, orgID: otherOrg}
	foreign := otherFixture.createRig(t, "Proto", "PAIRED")

	// Invalid-only or empty requests must not create even an empty generation.
	f.request(t, bitmain.identifier, authNeeded.identifier, defaultPassword.identifier, container.identifier, deleted.identifier, foreign.identifier, "missing")
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

	// A target rediscovered as a container before delivery must be omitted.
	_, err = f.db.ExecContext(t.Context(), `UPDATE discovered_device SET device_class = $1 WHERE device_identifier = $2`, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, second.identifier)
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
		ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
		OrganizationID:       f.orgID, RequestedBy: f.userID, DeviceIdentifiers: []string{first.identifier, second.identifier},
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
		f.request(t, rig.identifier)
	}
	f.complete(t, 1)
	// Include pending targeted work: a downgrade must preserve its generation
	// for the older organization-wide worker even though target scope is removed.
	f.request(t, rig.identifier)
	down, err := migrations.Migrations.ReadFile("000155_curtailment_rig_config_targets.down.sql")
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

	up, err := migrations.Migrations.ReadFile("000155_curtailment_rig_config_targets.up.sql")
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

func TestRigConfigStore_ClaimScopeIsStableAcrossNewerSettingsRequests(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	first := f.createRig(t, "Proto", "PAIRED")
	second := f.createRig(t, "Proto", "PAIRED")
	f.request(t, first.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), claim.DesiredGeneration)
	require.Equal(t, int64(0), claim.FullReconcileGeneration)

	// A later settings request must not broaden this claimed device-scoped
	// generation. Its full-delivery watermark belongs to the next claim.
	_, err = f.queries.InsertMQTTSourceConfig(t.Context(), sqlc.InsertMQTTSourceConfigParams{
		OrganizationID: f.orgID, ServiceUserID: f.userID, SourceName: "settings", Topic: "test/curtailment",
		BrokerPrimaryHost: "primary.example", BrokerSecondaryHost: "secondary.example",
		BrokerTransport: "tcp", PayloadFormat: "target_timestamp", Enabled: true,
		MqttUsername: "test", MqttPasswordEnc: "enc:secret",
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), claim.FullReconcileGeneration)
	require.Equal(t, []string{first.identifier}, f.targets(t, claim.DesiredGeneration))
	f.complete(t, claim.DesiredGeneration)
	full, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(2), full.DesiredGeneration)
	require.Equal(t, int64(2), full.FullReconcileGeneration)
	require.Greater(t, full.FullReconcileGeneration, full.EnqueuedGeneration)
	f.complete(t, full.DesiredGeneration)

	f.request(t, second.identifier)
	current, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	require.True(t, f.isTargeted(t, current.EnqueuedGeneration, current.DesiredGeneration))
	targets, err := f.queries.ListRigConfigReconciliationTargets(t.Context(), sqlc.ListRigConfigReconciliationTargetsParams{
		ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
		OrganizationID:       f.orgID, EnqueuedGeneration: current.EnqueuedGeneration, DesiredGeneration: current.DesiredGeneration,
	})
	require.NoError(t, err)
	require.Equal(t, []string{second.identifier}, targets)
	f.complete(t, current.DesiredGeneration)
	require.Empty(t, f.targets(t, current.DesiredGeneration))
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
		FastAttempts: rigConfigFastDeliveryAttempts, SlowRetrySeconds: int32(rigConfigSlowRetryInterval / time.Second),
	}))
	require.ElementsMatch(t, []string{first.identifier, second.identifier}, f.targets(t, claim.DesiredGeneration))
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows, "retry backoff must apply")
	f.complete(t, claim.DesiredGeneration)

	require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{
		ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
		OrganizationID:       f.orgID, DeviceID: second.id,
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

	_, err = f.db.ExecContext(t.Context(), `UPDATE discovered_device SET device_class = $1 WHERE device_identifier = $2`, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, second.identifier)
	require.NoError(t, err)
	require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{
		ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER),
		OrganizationID:       f.orgID, DeviceID: second.id,
	}))
	var desiredGeneration int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT desired_generation FROM curtailment_rig_config_reconciliation WHERE organization_id = $1", f.orgID).Scan(&desiredGeneration))
	require.Equal(t, terminal.DesiredGeneration, desiredGeneration, "container failures must not reopen rig config work")
}

// A short outage must not strand stale fallback providers: once fast retries
// are spent, delivery keeps retrying on the slow period instead of stopping.
func TestRigConfigStore_DeliveryRetriesBackOffThenContinueSlowly(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	f.request(t, rig.identifier)
	retryDelay := func() time.Duration {
		t.Helper()
		claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
		require.NoError(t, err, "delivery is still claimable after earlier failures")
		require.NoError(t, f.queries.RetryRigConfigReconciliation(t.Context(), sqlc.RetryRigConfigReconciliationParams{
			OrganizationID: f.orgID, DesiredGeneration: claim.DesiredGeneration, LastError: sql.NullString{String: "queue unavailable", Valid: true},
			FastAttempts: rigConfigFastDeliveryAttempts, SlowRetrySeconds: int32(rigConfigSlowRetryInterval / time.Second),
		}))
		var seconds float64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT EXTRACT(EPOCH FROM retry_at - CURRENT_TIMESTAMP)::float8 FROM curtailment_rig_config_reconciliation WHERE organization_id=$1", f.orgID).Scan(&seconds))
		_, err = f.db.ExecContext(t.Context(), "UPDATE curtailment_rig_config_reconciliation SET retry_at=now() WHERE organization_id=$1", f.orgID)
		require.NoError(t, err)
		return time.Duration(seconds * float64(time.Second))
	}
	delays := make([]time.Duration, 0, rigConfigFastDeliveryAttempts+2)
	for range rigConfigFastDeliveryAttempts + 2 {
		delays = append(delays, retryDelay())
	}
	for attempt, delay := range delays[:rigConfigFastDeliveryAttempts-1] {
		require.InDelta(t, float64(5*(attempt+1)), delay.Seconds(), 2, "fast retry %d backs off linearly", attempt+1)
	}
	for _, delay := range delays[rigConfigFastDeliveryAttempts-1:] {
		require.InDelta(t, rigConfigSlowRetryInterval.Seconds(), delay.Seconds(), 2, "spent fast retries continue on the slow period")
	}
}

func TestRigConfigStore_DeliveryBudgetsRequireExplicitRecovery(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	f.request(t, rig.identifier)
	recovered, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	f.complete(t, recovered.DesiredGeneration)
	for range 10 {
		require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{OrganizationID: f.orgID, DeviceID: rig.id, ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)}))
		_, err = f.db.ExecContext(t.Context(), "UPDATE curtailment_rig_config_reconciliation SET retry_at=now() WHERE organization_id=$1", f.orgID)
		require.NoError(t, err)
		claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
		require.NoError(t, err)
		f.complete(t, claim.DesiredGeneration)
	}
	require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{OrganizationID: f.orgID, DeviceID: rig.id, ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)}))
	_, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.ErrorIs(t, err, sql.ErrNoRows)
	f.request(t, rig.identifier)
	recovered, err = f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	f.complete(t, recovered.DesiredGeneration)
	require.NoError(t, f.queries.RequeueRigConfigReconciliationAfterTerminalFailure(t.Context(), sqlc.RequeueRigConfigReconciliationAfterTerminalFailureParams{OrganizationID: f.orgID, DeviceID: rig.id, ContainerDeviceClass: int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)}))
	var attempts int
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT attempts FROM curtailment_rig_config_delivery_attempt WHERE organization_id=$1 AND device_id=$2", f.orgID, rig.id).Scan(&attempts))
	require.Equal(t, 1, attempts, "pairing/recovery request restarts the device's terminal retry budget")
}

// Rigs keep a fallback config delivered before scoped providers until delivery
// is requested, so the upgrade rebuilds every organization that may hold one.
func TestRigConfigStore_ScopedFallbackUpgradeRebuildsDeliveredConfigs(t *testing.T) {
	f := newRigConfigStoreFixture(t)
	rig := f.createRig(t, "Proto", "PAIRED")
	f.request(t, rig.identifier)
	claim, err := f.queries.ClaimRigConfigReconciliation(t.Context())
	require.NoError(t, err)
	f.complete(t, claim.DesiredGeneration)

	sourceOnlyOrg, err := f.queries.CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: uuid.NewString(), Name: "source only"})
	require.NoError(t, err)
	source := validSettingsSource()
	source.OrganizationID = sourceOnlyOrg
	source.ServiceUserID = f.userID
	source.MQTTPasswordEncrypted = "enc:secret"
	_, err = NewSQLCSettingsStore(f.queries).CreateSourceConfig(t.Context(), source)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "DELETE FROM curtailment_rig_config_reconciliation WHERE organization_id = $1", sourceOnlyOrg)
	require.NoError(t, err)

	for _, name := range []string{
		"current/001056_internal_curtailment_wave4_runtime.down.sql",
		"current/001056_internal_curtailment_wave4_runtime.up.sql",
	} {
		migration, err := migrations.Current.ReadFile(name)
		require.NoError(t, err)
		_, err = f.db.ExecContext(t.Context(), string(migration))
		require.NoError(t, err, name)
	}
	for _, orgID := range []int64{f.orgID, sourceOnlyOrg} {
		var desired, enqueued, full int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `
			SELECT desired_generation, enqueued_generation, full_reconcile_generation
			FROM curtailment_rig_config_reconciliation WHERE organization_id = $1
		`, orgID).Scan(&desired, &enqueued, &full))
		require.Greater(t, desired, enqueued, "the upgrade leaves delivery pending")
		require.Equal(t, desired, full, "the pending delivery covers every rig")
	}
}
