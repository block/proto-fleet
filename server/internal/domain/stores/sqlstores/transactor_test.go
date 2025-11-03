package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	miner "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTransaction_OuterRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	transactor := sqlstores.NewSQLTransactor(db)
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	_, err := db.Exec(`INSERT IGNORE INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org-1', 'Test Organization 1', 'dummy-key-for-testing')`)
	require.NoError(t, err, "Failed to create test organization")

	testDevice := &pb.Device{
		DeviceIdentifier: "test-device-rollback",
		MacAddress:       "AA:BB:CC:DD:EE:FF",
		SerialNumber:     "TEST654321",
		Model:            "Rollback Model",
		Manufacturer:     "Rollback Manufacturer",
		IpAddress:        "192.168.1.100",
		Port:             "4028",
		UrlScheme:        "https",
	}

	// Act
	err = transactor.RunInTx(ctx, func(ctx context.Context) error {
		err := transactor.RunInTx(ctx, func(ctx context.Context) error {
			return deviceStore.UpsertDevice(ctx, testDevice, 1, miner.TypeProto.String())
		})
		require.NoError(t, err)

		return sql.ErrTxDone
	})

	// Assert
	require.Error(t, err)

	_, err = deviceStore.GetDeviceByDeviceIdentifier(ctx, testDevice.DeviceIdentifier, 1)
	require.Error(t, err, "Device should not exist after transaction rollback")
	assert.Contains(t, err.Error(), "device not found with identifier=test-device-rollback")
}

func TestWithTransaction_InnerRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	transactor := sqlstores.NewSQLTransactor(db)
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	_, err := db.Exec(`INSERT IGNORE INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org-1', 'Test Organization 1', 'dummy-key-for-testing')`)
	require.NoError(t, err, "Failed to create test organization")

	testDevice := &pb.Device{
		DeviceIdentifier: "test-device-rollback",
		MacAddress:       "AA:BB:CC:DD:EE:FF",
		SerialNumber:     "TEST654321",
		Model:            "Rollback Model",
		Manufacturer:     "Rollback Manufacturer",
		IpAddress:        "192.168.1.100",
		Port:             "4028",
		UrlScheme:        "https",
	}

	// Act
	err = transactor.RunInTx(ctx, func(ctx context.Context) error {
		err := deviceStore.UpsertDevice(ctx, testDevice, 1, miner.TypeProto.String())
		require.NoError(t, err)
		return transactor.RunInTx(ctx, func(_ context.Context) error {
			return sql.ErrTxDone
		})
	})

	// Assert
	require.Error(t, err)

	_, err = deviceStore.GetDeviceByDeviceIdentifier(ctx, testDevice.DeviceIdentifier, 1)
	require.Error(t, err, "Device should not exist after transaction rollback")
	assert.Contains(t, err.Error(), "device not found with identifier=test-device-rollback")
}

func TestNestedTransactions_DatabaseLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	// Arrange
	db := testutil.GetTestDB(t)
	transactor := sqlstores.NewSQLTransactor(db)
	deviceStore := sqlstores.NewSQLDeviceStore(db)
	ctx := t.Context()

	// Ensure organization exists for testing
	_, err := db.Exec(`INSERT IGNORE INTO organization (id, org_id, name, miner_auth_private_key) VALUES (1, 'test-org-1', 'Test Organization 1', 'dummy-key-for-testing')`)
	require.NoError(t, err, "Failed to create test organization")

	outerDevice := &pb.Device{
		DeviceIdentifier: "outer-device-txn",
		MacAddress:       "AA:BB:CC:11:22:33",
		SerialNumber:     "SN-OUTER-123",
		Model:            "Outer Model",
		Manufacturer:     "Test Manufacturer",
		IpAddress:        "192.168.1.101",
		Port:             "4028",
		UrlScheme:        "https",
	}

	innerDevice := &pb.Device{
		DeviceIdentifier: "inner-device-txn",
		MacAddress:       "AA:BB:CC:44:55:66",
		SerialNumber:     "SN-INNER-456",
		Model:            "Inner Model",
		Manufacturer:     "Test Manufacturer",
		IpAddress:        "192.168.1.102",
		Port:             "4028",
		UrlScheme:        "https",
	}

	// Act - This test will deadlock if nested transactions are created at DB level
	err = transactor.RunInTx(ctx, func(outerCtx context.Context) error {
		err := deviceStore.UpsertDevice(outerCtx, outerDevice, 1, miner.TypeProto.String())
		require.NoError(t, err)

		// Attempt a nested transaction
		return transactor.RunInTx(outerCtx, func(innerCtx context.Context) error {
			err := deviceStore.UpsertDevice(innerCtx, innerDevice, 1, miner.TypeProto.String())
			require.NoError(t, err)

			// If these are truly separate transactions at DB level, this would likely cause a deadlock
			// because the inner transaction would be waiting for a lock held by the outer transaction

			// Attempt to update both records from the inner transaction context
			// This should succeed if there's proper transaction handling
			err = deviceStore.UpsertDevice(innerCtx, outerDevice, 1, miner.TypeProto.String())
			return err
		})
	})

	// Assert
	require.NoError(t, err, "Nested transaction operations should complete without deadlock")

	// Verify both devices were created in the same transaction
	// by checking if they both exist or both don't exist
	queries := sqlc.New(db)

	_, outerErr := queries.GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: outerDevice.DeviceIdentifier,
		OrgID:            1,
	})
	require.NoError(t, outerErr)

	_, innerErr := queries.GetDeviceByDeviceIdentifier(ctx, sqlc.GetDeviceByDeviceIdentifierParams{
		DeviceIdentifier: innerDevice.DeviceIdentifier,
		OrgID:            1,
	})
	require.NoError(t, innerErr)
}
