package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMigration000087_MigratesPairedAntminerDiscoveredDevicesToAsicrs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	db := testutil.GetTestDB(t)
	ctx := t.Context()
	queries := sqlc.New(db)

	orgID, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		OrgID:               "migration-000087",
		Name:                "Migration 000087",
		MinerAuthPrivateKey: "test-key",
	})
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO discovered_device
			(org_id, device_identifier, model, manufacturer, driver_name, ip_address, port, url_scheme, is_active)
		VALUES
			($1, 'stock-antminer', 'Antminer S19', 'Bitmain', 'antminer', '192.168.1.10', '4028', 'http', TRUE),
			($1, 'deleted-antminer', 'Antminer S19', 'Bitmain', 'antminer', '192.168.1.11', '4028', 'http', TRUE),
			($1, 'proto-miner', 'Proto Rig', 'Proto', 'proto', '192.168.1.12', '50051', 'grpc', TRUE)
	`, orgID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO device (org_id, discovered_device_id, device_identifier, mac_address)
		SELECT org_id, id, device_identifier, 'AA:BB:CC:DD:EE:01'
		FROM discovered_device
		WHERE org_id = $1 AND device_identifier = 'stock-antminer'
	`, orgID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		UPDATE discovered_device
		SET deleted_at = NOW()
		WHERE org_id = $1 AND device_identifier = 'deleted-antminer'
	`, orgID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		UPDATE discovered_device
		SET driver_name = 'asicrs',
			port = CASE WHEN port = '4028' THEN '80' ELSE port END
		WHERE driver_name = 'antminer'
			AND deleted_at IS NULL
	`)
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, `
		SELECT device_identifier, driver_name, port
		FROM discovered_device
		WHERE org_id = $1
	`, orgID)
	require.NoError(t, err)
	defer rows.Close()

	got := map[string]struct {
		driverName string
		port       string
	}{}
	for rows.Next() {
		var deviceIdentifier, driverName, port string
		require.NoError(t, rows.Scan(&deviceIdentifier, &driverName, &port))
		got[deviceIdentifier] = struct {
			driverName string
			port       string
		}{driverName: driverName, port: port}
	}
	require.NoError(t, rows.Err())

	require.Equal(t, "asicrs", got["stock-antminer"].driverName)
	require.Equal(t, "80", got["stock-antminer"].port)
	require.Equal(t, "antminer", got["deleted-antminer"].driverName)
	require.Equal(t, "4028", got["deleted-antminer"].port)
	require.Equal(t, "proto", got["proto-miner"].driverName)
	require.Equal(t, "50051", got["proto-miner"].port)

	var pairedDriverName string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT dd.driver_name
		FROM device d
		JOIN discovered_device dd ON dd.id = d.discovered_device_id
		WHERE d.org_id = $1 AND d.device_identifier = 'stock-antminer'
	`, orgID).Scan(&pairedDriverName))
	require.Equal(t, "asicrs", pairedDriverName)

	_, err = db.ExecContext(ctx, `
		UPDATE discovered_device
		SET driver_name = 'antminer',
			port = CASE WHEN port = '80' THEN '4028' ELSE port END
		WHERE driver_name = 'asicrs'
			AND deleted_at IS NULL
			AND (
				LOWER(COALESCE(manufacturer, '')) = 'bitmain'
				OR LOWER(COALESCE(model, '')) LIKE 'antminer%'
			)
	`)
	require.NoError(t, err)

	var downDriverName, downPort string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT driver_name, port
		FROM discovered_device
		WHERE org_id = $1 AND device_identifier = 'stock-antminer'
	`, orgID).Scan(&downDriverName, &downPort))
	require.Equal(t, "antminer", downDriverName)
	require.Equal(t, "4028", downPort)

	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT driver_name, port
		FROM discovered_device
		WHERE org_id = $1 AND device_identifier = 'proto-miner'
	`, orgID).Scan(&downDriverName, &downPort))
	require.Equal(t, "proto", downDriverName)
	require.Equal(t, "50051", downPort)
}
