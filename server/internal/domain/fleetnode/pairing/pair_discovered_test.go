package pairing_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	fleetnodepairing "github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

func pairResult(identifier string, outcome gatewaypb.PairOutcome) *gatewaypb.FleetNodePairResult {
	return &gatewaypb.FleetNodePairResult{
		DeviceIdentifier: identifier,
		Outcome:          outcome,
		SerialNumber:     "sn-" + identifier,
		MacAddress:       "aa:bb:cc:" + identifier,
		Model:            "S19",
		Manufacturer:     "Bitmain",
		FirmwareVersion:  "v2",
	}
}

func devicePairingStatus(t *testing.T, db *sql.DB, orgID int64, identifier string) string {
	t.Helper()
	var status string
	err := db.QueryRow(
		`SELECT dp.pairing_status FROM device d
		 JOIN device_pairing dp ON dp.device_id = d.id
		 WHERE d.device_identifier=$1 AND d.org_id=$2 AND d.deleted_at IS NULL`,
		identifier, orgID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	require.NoError(t, err)
	return status
}

func deviceBoundToNode(t *testing.T, db *sql.DB, orgID, fleetNodeID int64, identifier string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM device d
		 JOIN fleet_node_device fnd ON fnd.device_id = d.id
		 WHERE d.device_identifier=$1 AND d.org_id=$2 AND fnd.fleet_node_id=$3`,
		identifier, orgID, fleetNodeID,
	).Scan(&n))
	return n > 0
}

func bindDeviceToNode(t *testing.T, db *sql.DB, orgID, fleetNodeID, deviceID int64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, $3)`, fleetNodeID, deviceID, orgID)
	require.NoError(t, err)
}

func hasMinerCredentials(t *testing.T, db *sql.DB, orgID int64, identifier string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM device d
		 JOIN miner_credentials mc ON mc.device_id = d.id
		 WHERE d.device_identifier=$1 AND d.org_id=$2`,
		identifier, orgID,
	).Scan(&n))
	return n > 0
}

func deviceExists(t *testing.T, db *sql.DB, orgID int64, identifier string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM device WHERE device_identifier=$1 AND org_id=$2 AND deleted_at IS NULL`,
		identifier, orgID,
	).Scan(&n))
	return n > 0
}

func TestPersistFleetNodePairResult_PairedWithoutReportedCredentials(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-no-creds")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-no-creds")
	assignedBy := int64(1)

	// Act: a paired report may omit credentials when the driver did not use
	// reportable auth material.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:p-no-creds", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED), &assignedBy)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, "mac:p-no-creds"))
	assert.True(t, deviceBoundToNode(t, db, orgID, node, "mac:p-no-creds"), "PAIRED device must be bound to the node")
	assert.False(t, hasMinerCredentials(t, db, orgID, "mac:p-no-creds"), "pairing without reported credentials stores no credentials")
}

func TestPersistFleetNodePairResult_PairedBasicAuthStoresCredentials(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-basic")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-basic")
	result := pairResult("mac:p-basic", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	result.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: "hunter2"}
	assignedBy := int64(1)

	// Act: the node reports the basic-auth credentials it authenticated with.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, result, &assignedBy)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, "mac:p-basic"))
	assert.True(t, deviceBoundToNode(t, db, orgID, node, "mac:p-basic"))
	assert.True(t, hasMinerCredentials(t, db, orgID, "mac:p-basic"), "basic-auth credentials must be stored encrypted")
}

func TestPersistFleetNodePairResult_StoresNodeReportedDefaultCredentials(t *testing.T) {
	// Arrange: a basic-auth pairing with no operator credentials; the node
	// reports the plugin default credentials it authenticated with.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-default")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-default")
	result := pairResult("mac:p-default", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	result.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: "admin"}
	assignedBy := int64(1)

	// Act: nil operator credentials.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, result, &assignedBy)

	// Assert: paired, bound, and the node-reported default creds are stored.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.True(t, deviceBoundToNode(t, db, orgID, node, "mac:p-default"))
	assert.True(t, hasMinerCredentials(t, db, orgID, "mac:p-default"), "default credentials the node used must be stored")
}

func TestPersistFleetNodePairResult_DefaultPasswordActivePersistsRemediationState(t *testing.T) {
	// Arrange: the node paired with basic-auth credentials and the plugin reports
	// the miner is still using its factory default password.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-default-active")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-default-active")
	active := true
	result := pairResult("mac:p-default-active", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	result.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: "admin"}
	result.DefaultPasswordActive = &active
	assignedBy := int64(1)

	// Act
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, result, &assignedBy)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusDefaultPassword, status)
	assert.Equal(t, "DEFAULT_PASSWORD", devicePairingStatus(t, db, orgID, "mac:p-default-active"))
	assert.True(t, deviceBoundToNode(t, db, orgID, node, "mac:p-default-active"))
	assert.True(t, hasMinerCredentials(t, db, orgID, "mac:p-default-active"), "default credentials the node used must be stored")
}

func TestPersistFleetNodePairResult_DefaultPasswordUnknownPreservesExistingRemediationState(t *testing.T) {
	// Arrange: the cloud already knows this device still has default credentials,
	// but the node/plugin reports a successful pair without a default-password
	// determination.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-default-unknown")
	identifier := "mac:p-default-unknown"
	upsertNodeDiscovered(t, pairing, orgID, node, identifier)
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, identifier).Scan(&ddID))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		identifier, "aa:bb:cc:00:d0:01", "sn-default-unknown", orgID, ddID).Scan(&dev))
	bindDeviceToNode(t, db, orgID, node, dev)
	setPairingStatus(t, db, dev, "DEFAULT_PASSWORD")
	result := pairResult(identifier, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	result.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: "admin"}
	assignedBy := int64(1)

	// Act
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, result, &assignedBy)

	// Assert: absent default_password_active preserves the existing remediation
	// state instead of treating "unknown" as explicit false.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusDefaultPassword, status)
	assert.Equal(t, "DEFAULT_PASSWORD", devicePairingStatus(t, db, orgID, identifier))
	assert.True(t, deviceBoundToNode(t, db, orgID, node, identifier))
	assert.True(t, hasMinerCredentials(t, db, orgID, identifier), "credentials the node used must still be stored")
}

func TestPersistFleetNodePairResult_DefaultPasswordInactiveClearsExistingRemediationState(t *testing.T) {
	// Arrange: DEFAULT_PASSWORD is currently persisted, and the node/plugin
	// explicitly reports that factory credentials are no longer active.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-default-inactive")
	identifier := "mac:p-default-inactive"
	upsertNodeDiscovered(t, pairing, orgID, node, identifier)
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, identifier).Scan(&ddID))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		identifier, "aa:bb:cc:00:d1:01", "sn-default-inactive", orgID, ddID).Scan(&dev))
	bindDeviceToNode(t, db, orgID, node, dev)
	setPairingStatus(t, db, dev, "DEFAULT_PASSWORD")
	inactive := false
	result := pairResult(identifier, gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	result.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: "changed"}
	result.DefaultPasswordActive = &inactive
	assignedBy := int64(1)

	// Act
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, result, &assignedBy)

	// Assert: explicit false clears DEFAULT_PASSWORD back to ordinary PAIRED.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, identifier))
	assert.True(t, deviceBoundToNode(t, db, orgID, node, identifier))
}

func TestPersistFleetNodePairResult_StoresBlankPasswordCredentials(t *testing.T) {
	// Arrange: a basic-auth pairing where the node authenticated with a blank
	// password (common for miners). The node reports a username with an empty
	// password; that is valid auth material and must be stored, not dropped.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-blankpw")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-blankpw")
	result := pairResult("mac:p-blankpw", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	result.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: ""}
	assignedBy := int64(1)

	// Act
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, result, &assignedBy)

	// Assert: paired and the blank-password credential is stored.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.True(t, hasMinerCredentials(t, db, orgID, "mac:p-blankpw"), "a valid blank-password basic-auth credential must be stored")
}

func TestPersistFleetNodePairResult_AuthNeededThenRetrySucceeds(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-retry")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-retry")
	assignedBy := int64(1)

	// Act 1: no credentials -> AUTHENTICATION_NEEDED, not bound.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:p-retry", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)
	require.NoError(t, err)

	// Assert 1
	assert.Equal(t, fleetnodepairing.StatusAuthenticationNeeded, status)
	assert.Equal(t, "AUTHENTICATION_NEEDED", devicePairingStatus(t, db, orgID, "mac:p-retry"))
	assert.False(t, deviceBoundToNode(t, db, orgID, node, "mac:p-retry"), "auth-needed device is not bound")

	// Act 2: retry; the node now reports the credentials it authenticated with ->
	// PAIRED, bound, creds stored.
	retryResult := pairResult("mac:p-retry", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED)
	retryResult.UsedCredentials = &gatewaypb.UsedCredentials{Username: "root", Password: "pw"}
	status, err = pairing.PersistFleetNodePairResult(ctx, node, orgID, retryResult, &assignedBy)
	require.NoError(t, err)

	// Assert 2
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, "mac:p-retry"))
	assert.True(t, deviceBoundToNode(t, db, orgID, node, "mac:p-retry"))
	assert.True(t, hasMinerCredentials(t, db, orgID, "mac:p-retry"))
}

func TestPersistFleetNodePairResult_AuthNeededDoesNotDowngradeCloudPaired(t *testing.T) {
	// Arrange: a node-discovered device that is also cloud-PAIRED (PAIRED with no
	// fleet_node_device binding), simulating a device cloud-paired since target
	// resolution.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-guard-authneeded")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:cloud-authneeded")
	// A device keyed by the discovered identifier (as PersistFleetNodePairResult
	// keys it), cloud-PAIRED with no fleet_node_device binding.
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:cloud-authneeded").Scan(&ddID))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"mac:cloud-authneeded", "aa:bb:cc:00:cc:01", "sn-cloud-authneeded", orgID, ddID).Scan(&dev))
	setPairingStatus(t, db, dev, "PAIRED")
	assignedBy := int64(1)

	// Act: the node reports AUTH_NEEDED with different identity fields (pairResult
	// reports sn-/aa:bb:cc:<id>, distinct from the seeded values).
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:cloud-authneeded", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)
	require.NoError(t, err)

	// Assert: the cloud-dialed device keeps PAIRED (no downgrade), the returned
	// status reflects reality rather than AUTHENTICATION_NEEDED, and the node
	// report did not overwrite the device's learned identity.
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, "mac:cloud-authneeded"))
	var serial, mac string
	require.NoError(t, db.QueryRow(
		`SELECT serial_number, mac_address FROM device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:cloud-authneeded").Scan(&serial, &mac))
	assert.Equal(t, "sn-cloud-authneeded", serial, "node report must not overwrite a cloud-paired device's serial")
	assert.Equal(t, "aa:bb:cc:00:cc:01", mac, "node report must not overwrite a cloud-paired device's MAC")
}

func TestPersistFleetNodePairResult_AuthNeededDoesNotDowngradeCloudDefaultPassword(t *testing.T) {
	// Arrange: DEFAULT_PASSWORD is paired-like and cloud-dialed when not bound to
	// a fleet node, so a stale node AUTH_NEEDED report must not demote it.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-guard-default-authneeded")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:cloud-default-authneeded")
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:cloud-default-authneeded").Scan(&ddID))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"mac:cloud-default-authneeded", "aa:bb:cc:00:cd:01", "sn-cloud-default-authneeded", orgID, ddID).Scan(&dev))
	setPairingStatus(t, db, dev, "DEFAULT_PASSWORD")
	assignedBy := int64(1)

	// Act: the node reports AUTH_NEEDED after the cloud already marked the rig
	// DEFAULT_PASSWORD.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:cloud-default-authneeded", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)
	require.NoError(t, err)

	// Assert: the cloud-dialed device keeps DEFAULT_PASSWORD (no downgrade).
	assert.Equal(t, fleetnodepairing.StatusDefaultPassword, status)
	assert.Equal(t, "DEFAULT_PASSWORD", devicePairingStatus(t, db, orgID, "mac:cloud-default-authneeded"))
}

func TestPersistFleetNodePairResult_AuthNeededDoesNotDowngradeNodeBound(t *testing.T) {
	// Arrange: a device that became PAIRED and bound to a fleet node (node-dialed)
	// since target resolution -- e.g. a re-issued command paired it while a stale
	// AUTH_NEEDED from the first command is still in flight. DeviceHasActiveCloudPairing
	// returns false for node-bound devices, so the broader "already paired" guard
	// must catch this and refuse the downgrade.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-guard-nodebound")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:node-bound")
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:node-bound").Scan(&ddID))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"mac:node-bound", "aa:bb:cc:00:nb:01", "sn-node-bound", orgID, ddID).Scan(&dev))
	setPairingStatus(t, db, dev, "PAIRED")
	// Bound to the same node (node-dialed PAIRED), which the cloud-pairing guard ignores.
	_, err := db.Exec(`INSERT INTO fleet_node_device (fleet_node_id, device_id, org_id) VALUES ($1, $2, $3)`, node, dev, orgID)
	require.NoError(t, err)
	assignedBy := int64(1)

	// Act: a stale AUTH_NEEDED for the now-paired device.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:node-bound", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)
	require.NoError(t, err)

	// Assert: the bound device stays PAIRED; the stale report did not downgrade it.
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, "PAIRED", devicePairingStatus(t, db, orgID, "mac:node-bound"))
}

func TestSetDevicePairingAuthNeededIfNotPaired_DoesNotDowngradePairedLike(t *testing.T) {
	for _, pairingStatus := range []string{"PAIRED", "DEFAULT_PASSWORD"} {
		t.Run(pairingStatus, func(t *testing.T) {
			// Arrange: a paired-like device -- the state a concurrent pair could commit
			// during the PersistFleetNodePairResult guard window, after the read guard
			// saw it unpaired.
			ctx := t.Context()
			db, orgID, pairing, enrollment := setupPairingTest(t)
			node := createFleetNode(t, enrollment, orgID, "node-cond-"+pairingStatus)
			identifier := "mac:cond-" + pairingStatus
			upsertNodeDiscovered(t, pairing, orgID, node, identifier)
			var ddID int64
			require.NoError(t, db.QueryRow(
				`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
				orgID, identifier).Scan(&ddID))
			var dev int64
			require.NoError(t, db.QueryRow(
				`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
				 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				identifier, "aa:bb:cc:00:cp:01", "sn-cp", orgID, ddID).Scan(&dev))
			setPairingStatus(t, db, dev, pairingStatus)
			store := sqlstores.NewSQLDeviceStore(db)

			// Act
			applied, err := store.SetDevicePairingAuthNeededIfNotPaired(ctx, &pairingpb.Device{DeviceIdentifier: identifier}, orgID)
			require.NoError(t, err)

			// Assert: the conditional write is a no-op and the row stays paired-like.
			assert.False(t, applied)
			assert.Equal(t, pairingStatus, devicePairingStatus(t, db, orgID, identifier))
		})
	}
}

func TestSetDevicePairingAuthNeededIfNotPaired_WritesWhenNotPaired(t *testing.T) {
	// Arrange: an existing device with no pairing row yet.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-cond-unpaired")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:cond-unpaired")
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:cond-unpaired").Scan(&ddID))
	_, err := db.Exec(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		"mac:cond-unpaired", "aa:bb:cc:00:cu:01", "sn-cu", orgID, ddID)
	require.NoError(t, err)
	store := sqlstores.NewSQLDeviceStore(db)

	// Act
	applied, err := store.SetDevicePairingAuthNeededIfNotPaired(ctx, &pairingpb.Device{DeviceIdentifier: "mac:cond-unpaired"}, orgID)
	require.NoError(t, err)

	// Assert
	assert.True(t, applied)
	assert.Equal(t, "AUTHENTICATION_NEEDED", devicePairingStatus(t, db, orgID, "mac:cond-unpaired"))
}

func TestPersistFleetNodePairResult_AuthNeededPreservesExistingSerial(t *testing.T) {
	// Arrange: an existing unpaired device with a previously learned serial. A
	// later AUTH_NEEDED report (which carries no serial) must not erase it.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-keep-serial")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:keep-serial")
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:keep-serial").Scan(&ddID))
	_, err := db.Exec(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		"mac:keep-serial", "aa:bb:cc:00:ks:01", "sn-learned-earlier", orgID, ddID)
	require.NoError(t, err)
	assignedBy := int64(1)

	// Act: an AUTH_NEEDED result with no serial_number (common for auth failures).
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID,
		&gatewaypb.FleetNodePairResult{DeviceIdentifier: "mac:keep-serial", Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED},
		&assignedBy)
	require.NoError(t, err)

	// Assert: AUTHENTICATION_NEEDED recorded, but the prior serial is preserved.
	assert.Equal(t, fleetnodepairing.StatusAuthenticationNeeded, status)
	var serial string
	require.NoError(t, db.QueryRow(
		`SELECT serial_number FROM device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:keep-serial").Scan(&serial))
	assert.Equal(t, "sn-learned-earlier", serial, "an auth-needed report omitting the serial must not erase it")
}

func TestPersistFleetNodePairResult_RejectsForeignNode(t *testing.T) {
	// Arrange: device discovered by nodeA; nodeB reports a result for it.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	nodeA := createFleetNode(t, enrollment, orgID, "node-owner")
	nodeB := createFleetNode(t, enrollment, orgID, "node-foreign")
	upsertNodeDiscovered(t, pairing, orgID, nodeA, "mac:p-foreign")
	assignedBy := int64(1)

	// Act
	_, err := pairing.PersistFleetNodePairResult(ctx, nodeB, orgID, pairResult("mac:p-foreign", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED), &assignedBy)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not discovered by this fleet node")
	assert.False(t, deviceExists(t, db, orgID, "mac:p-foreign"), "a rejected result must not create a device")
}

func TestPersistFleetNodePairResult_SerialConflictFailsCleanly(t *testing.T) {
	// Arrange: an existing device already owns serial "sn-dup". A fleet-node device
	// discovered under an auto: identifier (no serial known pre-auth) then pairs and
	// reports that same serial, which collides with uq_device_serial_number.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-serial-conflict")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:owner")
	var ownerDD int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:owner").Scan(&ownerDD))
	_, err := db.Exec(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		"mac:owner", "aa:bb:cc:00:ow:01", "sn-dup", orgID, ownerDD)
	require.NoError(t, err)
	upsertNodeDiscovered(t, pairing, orgID, node, "auto:abcd1234")
	assignedBy := int64(1)

	// Act: a PAIRED report for the auto: device carrying the already-registered serial.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID,
		&gatewaypb.FleetNodePairResult{
			DeviceIdentifier: "auto:abcd1234",
			Outcome:          gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED,
			SerialNumber:     "sn-dup",
			MacAddress:       "aa:bb:cc:00:au:02",
		}, &assignedBy)

	// Assert: the serial conflict surfaces as a clean FAILED (no error), and the tx
	// rolled back so no device row was created for the auto: identifier.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusFailed, status)
	assert.False(t, deviceExists(t, db, orgID, "auto:abcd1234"), "a conflicting pair must not create a device")
}

func TestPersistFleetNodePairResult_UpdateSerialConflictFailsCleanly(t *testing.T) {
	// Arrange: device B owns serial "sn-taken"; device A already exists under its
	// own identifier with no serial. A pair retry for A reporting "sn-taken" hits
	// uq_device_serial_number via the UPDATE path (UpdateDeviceInfo), which must
	// surface the same clean FAILED as the insert path.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-upd-serial-conflict")
	devices := []struct {
		id, mac string
		serial  sql.NullString
	}{
		{id: "mac:serial-owner", mac: "aa:bb:cc:00:ud:01", serial: sql.NullString{String: "sn-taken", Valid: true}},
		{id: "mac:upd-dup", mac: "aa:bb:cc:00:ud:02"},
	}
	for _, d := range devices {
		upsertNodeDiscovered(t, pairing, orgID, node, d.id)
		var ddID int64
		require.NoError(t, db.QueryRow(
			`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
			orgID, d.id).Scan(&ddID))
		_, err := db.Exec(
			`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
			 VALUES ($1, $2, $3, $4, $5)`,
			d.id, d.mac, d.serial, orgID, ddID)
		require.NoError(t, err)
	}
	assignedBy := int64(1)

	// Act: an AUTH_NEEDED retry for the existing device reports the taken serial.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID,
		&gatewaypb.FleetNodePairResult{
			DeviceIdentifier: "mac:upd-dup",
			Outcome:          gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED,
			SerialNumber:     "sn-taken",
		}, &assignedBy)

	// Assert: clean FAILED (no Internal error), and the device's serial is unchanged.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusFailed, status)
	var serial sql.NullString
	require.NoError(t, db.QueryRow(
		`SELECT serial_number FROM device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:upd-dup").Scan(&serial))
	assert.False(t, serial.Valid && serial.String == "sn-taken", "the conflicting serial must not be written")
}

func TestPersistFleetNodePairResult_ErrorPersistsNothing(t *testing.T) {
	// Arrange
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-error")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-error")
	assignedBy := int64(1)

	// Act
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:p-error", gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR), &assignedBy)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "FAILED", status)
	assert.False(t, deviceExists(t, db, orgID, "mac:p-error"), "an ERROR outcome persists nothing")
}

func deviceIDByIdentifier(t *testing.T, db *sql.DB, orgID int64, identifier string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM device WHERE device_identifier=$1 AND org_id=$2 AND deleted_at IS NULL`,
		identifier, orgID,
	).Scan(&id))
	return id
}

func TestPersistFleetNodePairResult_PairedInvalidatesMinerCache(t *testing.T) {
	// Arrange: a node-discovered device and a recorder for cache invalidations.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-invalidate")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-invalidate")
	var invalidated []int64
	pairing.WithMinerInvalidator(func(_ context.Context, id int64) { invalidated = append(invalidated, id) })
	assignedBy := int64(1)

	// Act
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:p-invalidate", gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED), &assignedBy)

	// Assert: the node-reported pair evicts the device's stale direct handle so the next
	// command re-resolves over the ControlStream instead of dialing the cached handle.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusPaired, status)
	assert.Equal(t, []int64{deviceIDByIdentifier(t, db, orgID, "mac:p-invalidate")}, invalidated)
}

func TestPersistFleetNodePairResult_AuthNeededDoesNotInvalidate(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-persist-noinvalidate")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:p-noinvalidate")
	var invalidated []int64
	pairing.WithMinerInvalidator(func(_ context.Context, id int64) { invalidated = append(invalidated, id) })
	assignedBy := int64(1)

	// Act: a non-PAIRED outcome binds no device.
	status, err := pairing.PersistFleetNodePairResult(ctx, node, orgID, pairResult("mac:p-noinvalidate", gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_NEEDED), &assignedBy)

	// Assert: nothing bound, so nothing is evicted.
	require.NoError(t, err)
	assert.Equal(t, fleetnodepairing.StatusAuthenticationNeeded, status)
	assert.Empty(t, invalidated)
}

func TestResolvePairTargets(t *testing.T) {
	// Arrange
	ctx := t.Context()
	_, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-resolve")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:r-1")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:r-2")

	// Act
	all, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, nil)
	require.NoError(t, err)
	explicit, err := pairing.ResolvePairTargets(ctx, node, orgID, []string{"mac:r-1"}, false, nil)
	require.NoError(t, err)

	// Assert
	allIDs := targetIdentifiers(all)
	assert.Subset(t, allIDs, []string{"mac:r-1", "mac:r-2"})
	assert.Equal(t, []string{"mac:r-1"}, targetIdentifiers(explicit))
	for _, tg := range all {
		assert.Equal(t, "virtual", tg.GetDriverName(), "targets carry the discovery driver for plugin routing")
	}
}

func TestResolvePairTargets_ExcludesPairedDeviceWithDivergedDiscoveryLinkage(t *testing.T) {
	// Arrange: a PAIRED device whose original discovery row was soft-deleted and
	// then re-discovered by a node under the same identifier. The device row links
	// to the old discovery row, not the new one, so a linkage-only exclusion would
	// dispatch it for pairing; the node would mutate the miner's credentials and
	// persistence would then reject it as already paired.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-diverged-linkage")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:diverged")
	var oldDD int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:diverged").Scan(&oldDD))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"mac:diverged", "aa:bb:cc:00:dl:01", "sn-dl", orgID, oldDD).Scan(&dev))
	setPairingStatus(t, db, dev, "PAIRED")
	// Soft-delete the linked discovery row, then re-discover: the partial unique
	// index lets a fresh live row with the same identifier coexist.
	_, err := db.Exec(`UPDATE discovered_device SET deleted_at = now() WHERE id = $1`, oldDD)
	require.NoError(t, err)
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:diverged")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:fresh")

	// Act
	all, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, nil)
	require.NoError(t, err)
	explicit, err := pairing.ResolvePairTargets(ctx, node, orgID, []string{"mac:diverged"}, false, nil)
	require.NoError(t, err)

	// Assert: the already-paired identifier is never dispatched, by pair-all or
	// explicit selection; the genuinely new device still is.
	assert.NotContains(t, targetIdentifiers(all), "mac:diverged")
	assert.Contains(t, targetIdentifiers(all), "mac:fresh")
	assert.Empty(t, explicit)
}

func TestResolvePairTargets_AuthNeededExclusionSurvivesDivergedLinkage(t *testing.T) {
	// Arrange: an AUTHENTICATION_NEEDED device whose discovery row was soft-deleted
	// and re-created by the node under the same identifier. The starvation guard
	// must still recognize it by identifier, or credential-less pair-all re-selects
	// the same unsatisfiable row and starves never-attempted devices.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-an-diverged")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:an-diverged")
	var oldDD int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:an-diverged").Scan(&oldDD))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"mac:an-diverged", "aa:bb:cc:00:ad:01", "sn-ad", orgID, oldDD).Scan(&dev))
	setPairingStatus(t, db, dev, "AUTHENTICATION_NEEDED")
	_, err := db.Exec(`UPDATE discovered_device SET deleted_at = now() WHERE id = $1`, oldDD)
	require.NoError(t, err)
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:an-diverged")

	// Act
	noCreds, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, nil)
	require.NoError(t, err)
	pw := "pw"
	withCreds, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, &pairingpb.Credentials{Username: "root", Password: &pw})
	require.NoError(t, err)

	// Assert: excluded from credential-less pair-all, still retryable with creds.
	assert.NotContains(t, targetIdentifiers(noCreds), "mac:an-diverged")
	assert.Contains(t, targetIdentifiers(withCreds), "mac:an-diverged")
}

func TestResolvePairTargetsByFilter_AuthNeededIncludesDivergedLinkage(t *testing.T) {
	// Arrange: an AUTHENTICATION_NEEDED device whose discovery row was soft-deleted
	// and re-created by the node under the same identifier. A status-filtered
	// pair-all with credentials must still see the live device's status even though
	// it is linked to the deleted discovery row.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-filter-an-diverged")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:filter-an-diverged")
	var oldDD int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:filter-an-diverged").Scan(&oldDD))
	var dev int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		"mac:filter-an-diverged", "aa:bb:cc:00:fd:01", "sn-fd", orgID, oldDD).Scan(&dev))
	setPairingStatus(t, db, dev, "AUTHENTICATION_NEEDED")
	_, err := db.Exec(`UPDATE discovered_device SET deleted_at = now() WHERE id = $1`, oldDD)
	require.NoError(t, err)
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:filter-an-diverged")

	// Act
	pw := "pw"
	targets, _, err := pairing.ResolvePairTargetsByFilterPage(ctx, node, orgID, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"mac:filter-an-diverged"}, targetIdentifiers(targets))
}

func TestResolvePairTargets_PairAllExcludesAuthNeededWithoutCredentials(t *testing.T) {
	// Arrange: one never-attempted device and one AUTHENTICATION_NEEDED device.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-resolve-authneeded")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:new")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:authneeded")
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:authneeded").Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, 'aa:bb:cc:00:an:01', 'sn-an', $2, $3) RETURNING id`,
		"mac:authneeded", orgID, ddID).Scan(&devID))
	setPairingStatus(t, db, devID, "AUTHENTICATION_NEEDED")

	// Act + Assert: pair-all WITHOUT credentials can't satisfy the auth-needed row,
	// so it's excluded (won't starve never-attempted devices on re-issue).
	noCreds, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, nil)
	require.NoError(t, err)
	ids := targetIdentifiers(noCreds)
	assert.Contains(t, ids, "mac:new")
	assert.NotContains(t, ids, "mac:authneeded")

	// Act + Assert: a username-only message (password unset) is unusable for
	// basic-auth, so it must not re-enable the auth-needed row like real creds would.
	userOnly, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, &pairingpb.Credentials{Username: "root"})
	require.NoError(t, err)
	assert.NotContains(t, targetIdentifiers(userOnly), "mac:authneeded")

	// Act + Assert: pair-all WITH credentials retries the auth-needed row.
	pw := "pw"
	withCreds, err := pairing.ResolvePairTargets(ctx, node, orgID, nil, true, &pairingpb.Credentials{Username: "root", Password: &pw})
	require.NoError(t, err)
	assert.Contains(t, targetIdentifiers(withCreds), "mac:authneeded")
}

func TestResolvePairTargetsByFilter_AuthenticationNeeded(t *testing.T) {
	// Arrange: a bulk auth-needed filter must retry only auth-needed rows, not
	// every unpaired row discovered by the node.
	ctx := t.Context()
	db, orgID, pairing, enrollment := setupPairingTest(t)
	node := createFleetNode(t, enrollment, orgID, "node-resolve-filter-authneeded")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:new")
	upsertNodeDiscovered(t, pairing, orgID, node, "mac:authneeded")
	var ddID int64
	require.NoError(t, db.QueryRow(
		`SELECT id FROM discovered_device WHERE org_id=$1 AND device_identifier=$2 AND deleted_at IS NULL`,
		orgID, "mac:authneeded").Scan(&ddID))
	var devID int64
	require.NoError(t, db.QueryRow(
		`INSERT INTO device (device_identifier, mac_address, serial_number, org_id, discovered_device_id)
		 VALUES ($1, 'aa:bb:cc:00:af:01', 'sn-af', $2, $3) RETURNING id`,
		"mac:authneeded", orgID, ddID).Scan(&devID))
	setPairingStatus(t, db, devID, "AUTHENTICATION_NEEDED")

	// Act
	pw := "pw"
	targets, _, err := pairing.ResolvePairTargetsByFilterPage(ctx, node, orgID, &minercommandv1.DeviceFilter{
		PairingStatus: []fleetmanagementv1.PairingStatus{fleetmanagementv1.PairingStatus_PAIRING_STATUS_AUTHENTICATION_NEEDED},
	}, &pairingpb.Credentials{Username: "root", Password: &pw}, nil)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"mac:authneeded"}, targetIdentifiers(targets))
}

func targetIdentifiers(targets []*pairingpb.FleetNodePairTarget) []string {
	out := make([]string, 0, len(targets))
	for _, tg := range targets {
		out = append(out, tg.GetDeviceIdentifier())
	}
	return out
}
