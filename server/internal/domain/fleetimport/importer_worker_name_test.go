package fleetimport

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

func TestSetWorkerNames_InitializesMissingNamesAndPreservesSavedNames(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	conn := dbtest.GetTestDB(t)
	_, err := conn.Exec(`INSERT INTO organization (id, org_id, name)
 VALUES (1, '00000000-0000-0000-0000-000000000001', 'Test Org') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	imp := &Importer{deviceStore: sqlstores.NewSQLDeviceStore(conn)}
	for i, tc := range []struct {
		name     string
		stored   any
		imported string
		want     string
		changed  int32
	}{
		{name: "explicit name", imported: " \nworker-01\t ", want: "worker-01", changed: 1},
		{name: "default", imported: " \n\t ", want: "AA:BB:CC:DD:EE:02", changed: 1},
		{name: "preserve saved name", stored: "fleet.worker", imported: "external.worker", want: "fleet.worker"},
		{name: "preserve saved name without explicit import", stored: "fleet.worker", want: "fleet.worker"},
		{name: "preserve explicit no worker name", stored: "", want: ""},
		{name: "preserve non-null blank stored name", stored: "   ", want: "   "},
		{name: "explicit import cannot replace no worker name", stored: "", imported: "external.worker", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deviceID := int64(i + 1)
			identifier := fmt.Sprintf("device-%d", deviceID)
			mac := fmt.Sprintf("aa:bb:cc:dd:ee:%02d", deviceID)
			_, err := conn.Exec(`INSERT INTO discovered_device
    (id, org_id, device_identifier, driver_name, ip_address, port, url_scheme, is_active)
    VALUES ($1, 1, $2, 'proto', '192.168.10.16', '80', 'http', TRUE)`, deviceID, identifier)
			require.NoError(t, err)
			_, err = conn.Exec(`INSERT INTO device
    (id, org_id, discovered_device_id, device_identifier, mac_address, worker_name)
    VALUES ($1, 1, $1, $2, $3, $4)`, deviceID, identifier, mac, tc.stored)
			require.NoError(t, err)
			data := &ImportData{Miners: []ImportMiner{{MAC: mac, WorkerName: tc.imported}}}
			mapping := map[string]*interfaces.PairedDeviceInfo{
				networking.NormalizeMAC(mac): {DeviceIdentifier: identifier, MacAddress: mac},
			}
			assert.Zero(t, imp.setWorkerNames(t.Context(), 2, data, mapping), "another organization cannot initialize the name")
			assert.Equal(t, tc.changed, imp.setWorkerNames(t.Context(), 1, data, mapping))
			assert.Zero(t, imp.setWorkerNames(t.Context(), 1, data, mapping), "repeat import preserves the name and reports no change")
			var name string
			var syncStatus sql.NullString
			require.NoError(t, conn.QueryRow(`SELECT worker_name, worker_name_pool_sync_status::text FROM device WHERE id=$1`, deviceID).Scan(&name, &syncStatus))
			assert.Equal(t, tc.want, name)
			assert.False(t, syncStatus.Valid, "import must not request a legacy pool reapply")
		})
	}
}
