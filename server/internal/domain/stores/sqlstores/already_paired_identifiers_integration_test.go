package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestGetAlreadyPairedDeviceIdentifiers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	dbService := testutil.NewDatabaseService(t, nil)
	db := dbService.DB
	store := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()
	org := dbService.CreateSuperAdminUser()
	otherOrg := dbService.CreateSuperAdminUser2()

	paired := dbService.CreateDevice(org.OrganizationID, "proto")
	authNeededController := dbService.CreateDevice(org.OrganizationID, "proto")
	defaultPassword := dbService.CreateDevice(org.OrganizationID, "proto")
	unpaired := dbService.CreateDevice(org.OrganizationID, "proto")
	pending := dbService.CreateDevice(org.OrganizationID, "proto")
	deleted := dbService.CreateDevice(org.OrganizationID, "proto")
	inactiveDiscovered := dbService.CreateDevice(org.OrganizationID, "proto")
	deletedDiscovered := dbService.CreateDevice(org.OrganizationID, "proto")
	otherOrgPaired := dbService.CreateDevice(otherOrg.OrganizationID, "proto")

	setPairingStatus := func(deviceIdentifier string, orgID int64, status sqlc.PairingStatusEnum) {
		t.Helper()
		require.NoError(t, store.UpsertDevicePairing(ctx, &pb.Device{DeviceIdentifier: deviceIdentifier}, orgID, string(status)))
	}
	setPairingStatus(paired.ID, org.OrganizationID, sqlc.PairingStatusEnumPAIRED)
	setPairingStatus(authNeededController.ID, org.OrganizationID, sqlc.PairingStatusEnumAUTHENTICATIONNEEDED)
	setPairingStatus(defaultPassword.ID, org.OrganizationID, sqlc.PairingStatusEnumDEFAULTPASSWORD)
	setPairingStatus(unpaired.ID, org.OrganizationID, sqlc.PairingStatusEnumUNPAIRED)
	setPairingStatus(pending.ID, org.OrganizationID, sqlc.PairingStatusEnumPENDING)
	setPairingStatus(deleted.ID, org.OrganizationID, sqlc.PairingStatusEnumPAIRED)
	setPairingStatus(inactiveDiscovered.ID, org.OrganizationID, sqlc.PairingStatusEnumPAIRED)
	setPairingStatus(deletedDiscovered.ID, org.OrganizationID, sqlc.PairingStatusEnumPAIRED)
	setPairingStatus(otherOrgPaired.ID, otherOrg.OrganizationID, sqlc.PairingStatusEnumPAIRED)

	_, err := db.ExecContext(ctx, `
		UPDATE discovered_device
		SET device_class = $1
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $2)
	`, int32(commonpb.DeviceClass_DEVICE_CLASS_CONTAINER), authNeededController.DatabaseID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE device SET deleted_at = NOW() WHERE id = $1`, deleted.DatabaseID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE discovered_device
		SET is_active = FALSE
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)
	`, inactiveDiscovered.DatabaseID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE discovered_device
		SET deleted_at = NOW()
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)
	`, deletedDiscovered.DatabaseID)
	require.NoError(t, err)

	identifiers, err := store.GetAlreadyPairedDeviceIdentifiers(ctx, org.OrganizationID, []string{
		paired.ID,
		authNeededController.ID,
		defaultPassword.ID,
		unpaired.ID,
		pending.ID,
		deleted.ID,
		inactiveDiscovered.ID,
		deletedDiscovered.ID,
		otherOrgPaired.ID,
		"missing-device",
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{paired.ID, authNeededController.ID, defaultPassword.ID}, identifiers)

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.GetAlreadyPairedDeviceIdentifiers(canceledCtx, org.OrganizationID, []string{paired.ID})
	require.ErrorIs(t, err, context.Canceled)
}
