package sqlstores_test

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCT1ConcurrentDiscoveryKeepsBothEndpointsInOneContainer(t *testing.T) {
	_, store, org := ct1HAStore(t)
	devices := []*discoverymodels.DiscoveredDevice{
		ct1HADiscovery(org, "a", "10.0.0.11"),
		ct1HADiscovery(org, "b", "10.0.0.12"),
	}
	var wg sync.WaitGroup
	results := make([]*discoverymodels.DiscoveredDevice, 2)
	errs := make([]error, 2)
	for i := range devices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = store.SaveDirect(t.Context(), discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: devices[i].DeviceIdentifier}, devices[i])
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0].DeviceIdentifier, results[1].DeviceIdentifier)
	saved, err := store.GetDevice(t.Context(), discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: results[0].DeviceIdentifier})
	require.NoError(t, err)
	require.Len(t, saved.Endpoints, 2)
	count, err := store.CountActiveUnpairedDevices(t.Context(), org)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	// A later scan of only A retains B and its address.
	again, err := store.SaveDirect(t.Context(), discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: "new-scan"}, devices[0])
	require.NoError(t, err)
	require.Equal(t, saved.DeviceIdentifier, again.DeviceIdentifier)
	require.Len(t, again.Endpoints, 2)
	require.Equal(t, saved.SerialNumber, again.SerialNumber)
}

func ct1HADiscovery(org int64, id, host string) *discoverymodels.DiscoveredDevice {
	return &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: id, IpAddress: host, Port: "8080", UrlScheme: "http", RpcPort: 9095,
			SerialNumber: id, DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
			DriverName: "proto", Model: "CT1", Manufacturer: "Proto",
			Endpoints: []*commonpb.DeviceEndpoint{{Id: id, DeviceKey: "opaque-pair", Host: host, Port: 8080, UrlScheme: "http", RpcPort: 9095}},
		},
		OrgID: org, IsActive: true,
	}
}

func ct1HAStore(t *testing.T) (*sql.DB, *sqlstores.SQLDiscoveredDeviceStore, int64) {
	t.Helper()
	db := testutil.GetTestDB(t)
	org, err := sqlc.New(db).CreateOrganization(t.Context(), sqlc.CreateOrganizationParams{OrgID: "ha", Name: "HA"})
	require.NoError(t, err)
	return db, sqlstores.NewSQLDiscoveredDeviceStore(db), org
}

func saveCT1HA(t *testing.T, store *sqlstores.SQLDiscoveredDeviceStore, identifier string, device *discoverymodels.DiscoveredDevice) *discoverymodels.DiscoveredDevice {
	t.Helper()
	saved, err := store.SaveDirect(t.Context(), discoverymodels.DeviceOrgIdentifier{OrgID: device.OrgID, DeviceIdentifier: identifier}, device)
	require.NoError(t, err)
	return saved
}

func pairCT1HA(t *testing.T, db *sql.DB, device *discoverymodels.DiscoveredDevice) {
	t.Helper()
	store := sqlstores.NewSQLDeviceStore(db)
	require.NoError(t, store.InsertDevice(t.Context(), &device.Device, device.OrgID, device.DeviceIdentifier))
	require.NoError(t, store.UpsertDevicePairing(t.Context(), &device.Device, device.OrgID, "PAIRED"))
}

func setCT1HAPairing(t *testing.T, db *sql.DB, device *discoverymodels.DiscoveredDevice, status sqlc.PairingStatusEnum) {
	t.Helper()
	q := sqlc.New(db)
	d, err := q.GetDeviceByDeviceIdentifier(t.Context(), sqlc.GetDeviceByDeviceIdentifierParams{OrgID: device.OrgID, DeviceIdentifier: device.DeviceIdentifier})
	require.NoError(t, err)
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: d.ID, PairingStatus: status})
	require.NoError(t, err)
}

func TestCT1PairingSavePreservesRediscoveredEndpoints(t *testing.T) {
	db, store, org := ct1HAStore(t)
	first := saveCT1HA(t, store, "container", ct1HADiscovery(org, "a", "10.0.0.11"))
	saved := saveCT1HA(t, store, "scan-b", ct1HADiscovery(org, "b", "10.0.0.12"))
	pairCT1HA(t, db, saved)
	doi := discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: first.DeviceIdentifier}
	stale, err := store.GetDevice(t.Context(), doi)
	require.NoError(t, err)

	// Pairing retains this earlier list while discovery moves both controllers.
	setCT1HAPairing(t, db, saved, sqlc.PairingStatusEnumUNPAIRED)
	saveCT1HA(t, store, "moved-a", ct1HADiscovery(org, "a", "10.0.0.21"))
	current := saveCT1HA(t, store, "moved-b", ct1HADiscovery(org, "b", "10.0.0.22"))
	setCT1HAPairing(t, db, current, sqlc.PairingStatusEnumPAIRED)
	_, err = db.ExecContext(t.Context(), `UPDATE discovered_device SET is_active = FALSE, last_seen = CURRENT_TIMESTAMP - INTERVAL '1 day' WHERE org_id = $1 AND device_identifier = $2`, org, doi.DeviceIdentifier)
	require.NoError(t, err)
	inactive, err := store.GetDevice(t.Context(), doi)
	require.NoError(t, err)
	require.False(t, inactive.IsActive)
	require.NoError(t, store.ActivateDirect(t.Context(), doi))
	activated, err := store.GetDevice(t.Context(), doi)
	require.NoError(t, err)
	require.True(t, activated.IsActive)
	require.True(t, activated.LastSeen.After(inactive.LastSeen), "pairing activation must refresh last_seen")
	require.WithinDuration(t, time.Now(), activated.LastSeen, time.Minute)
	require.True(t, proto.Equal(&current.Device, &activated.Device), "activation must retain current endpoint and browser settings")

	stale.FirmwareVersion = "paired-v2"
	_, err = store.Save(t.Context(), doi, stale)
	require.NoError(t, err)

	after, err := store.GetDevice(t.Context(), doi)
	require.NoError(t, err)
	require.Equal(t, "paired-v2", after.FirmwareVersion, "pairing must still save device metadata")
	require.Equal(t, first.DeviceIdentifier, after.DeviceIdentifier)
	require.Len(t, after.Endpoints, 2)
	require.Equal(t, "b", after.Endpoints[1].Id)
	require.Equal(t, "10.0.0.22", after.Endpoints[1].Host)
	require.Equal(t, "a", after.Endpoints[0].Id)
	require.Equal(t, "10.0.0.21", after.Endpoints[0].Host)
	require.Equal(t, "10.0.0.21", after.IpAddress, "pairing must retain the current browser address")
}

func TestCT1DiscoveryRecoversSwappedAddressesInBothScanOrders(t *testing.T) {
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		t.Run(order[0]+" first", func(t *testing.T) {
			db, store, org := ct1HAStore(t)
			var saved *discoverymodels.DiscoveredDevice
			for _, id := range []string{"a", "b"} {
				host := "10.0.0.11"
				if id == "b" {
					host = "10.0.0.12"
				}
				device := ct1HADiscovery(org, id, host)
				device.Port, device.UrlScheme = "9443", "https"
				device.Endpoints[0].Port, device.Endpoints[0].UrlScheme = 9443, "https"
				saved = saveCT1HA(t, store, "scan-"+id, device)
			}
			pairCT1HA(t, db, saved)
			doi := discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: saved.DeviceIdentifier}
			originalID, err := store.GetDatabaseID(t.Context(), doi)
			require.NoError(t, err)
			setCT1HAPairing(t, db, saved, sqlc.PairingStatusEnumUNPAIRED)

			for _, id := range order {
				host := "10.0.0.12"
				if id == "b" {
					host = "10.0.0.11"
				}
				moved := saveCT1HA(t, store, "swapped-"+id, ct1HADiscovery(org, id, host))
				require.Equal(t, doi.DeviceIdentifier, moved.DeviceIdentifier)
			}

			after, err := store.GetDevice(t.Context(), doi)
			require.NoError(t, err)
			require.Equal(t, "a", after.SerialNumber)
			browserHost := "10.0.0.12"
			if order[0] == "b" {
				browserHost = "10.0.0.11"
			}
			require.Equal(t, browserHost, after.IpAddress)
			require.Equal(t, "9443", after.Port)
			require.Equal(t, "https", after.UrlScheme)
			require.Len(t, after.Endpoints, 2)
			require.Equal(t, "a", after.Endpoints[0].Id)
			require.Equal(t, "10.0.0.12", after.Endpoints[0].Host)
			require.Equal(t, "b", after.Endpoints[1].Id)
			require.Equal(t, "10.0.0.11", after.Endpoints[1].Host)
			for _, endpoint := range after.Endpoints {
				if endpoint.Id == order[0] {
					require.EqualValues(t, 9443, endpoint.Port, "the retained endpoint must keep its custom web port")
					require.Equal(t, "https", endpoint.UrlScheme)
				}
			}
			paired, err := sqlc.New(db).GetDeviceByDeviceIdentifier(t.Context(), sqlc.GetDeviceByDeviceIdentifierParams{OrgID: org, DeviceIdentifier: doi.DeviceIdentifier})
			require.NoError(t, err)
			require.Equal(t, originalID, paired.DiscoveredDeviceID)
			var count int
			err = db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM discovered_device WHERE org_id = $1 AND deleted_at IS NULL`, org).Scan(&count)
			require.NoError(t, err)
			require.Equal(t, 1, count, "both addresses must retain one inventory row")
		})
	}
}
