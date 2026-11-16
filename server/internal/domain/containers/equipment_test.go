package containers_test

import (
	"encoding/json"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	telemetry "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/block/proto-fleet/server/internal/infrastructure/timescaledb"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestEquipmentObservationsDirectAndFleetNodeTransport(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "direct"
		if remote {
			name = "FleetNode JSON"
		}
		t.Run(name, func(t *testing.T) {
			db := testutil.GetTestDB(t)
			ctx := t.Context()
			org, err := sqlc.New(db).CreateOrganization(ctx, sqlc.CreateOrganizationParams{OrgID: "equipment", Name: "Equipment"})
			require.NoError(t, err)
			d := &discoverymodels.DiscoveredDevice{Device: pairingpb.Device{DeviceIdentifier: "equipment-ct1", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, SerialNumber: "equipment", Model: "CT1", Manufacturer: "Proto", DriverName: "proto", IpAddress: "192.0.2.33", Port: "8080", UrlScheme: "http"}, OrgID: org, IsActive: true}
			_, err = sqlstores.NewSQLDiscoveredDeviceStore(db).Save(ctx, discoverymodels.DeviceOrgIdentifier{OrgID: org, DeviceIdentifier: d.DeviceIdentifier}, d)
			require.NoError(t, err)
			devices := sqlstores.NewSQLDeviceStore(db)
			require.NoError(t, devices.InsertDevice(ctx, &d.Device, org, d.DeviceIdentifier))
			require.NoError(t, devices.UpsertDevicePairing(ctx, &d.Device, org, "PAIRED"))
			store, err := timescaledb.NewTelemetryStore(db, timescaledb.DefaultConfig())
			require.NoError(t, err)
			now := time.Now().UTC().Truncate(time.Microsecond)
			snapshot := &sdk.ContainerSnapshot{HA: &sdk.ContainerHA{ObservedAt: now, Degraded: true}, Plant: &sdk.ContainerPlant{ObservedAt: now, DryCoolerInletTemperatureC: new(0.0), AlarmActive: new(true), CriticalAlarmActive: new(false)}, ObservedAt: now, Tanks: []sdk.ContainerTank{{Position: 1}}, Fans: []sdk.ContainerFan{{ComponentID: "fan-a", ObservedAt: now.Add(-time.Second), SpeedPercent: new(0.0), PowerW: new(0.0), AlarmActive: new(false)}, {ComponentID: "fan-b", ObservedAt: now.Add(-3 * time.Minute)}}, Controls: &sdk.ContainerControls{ObservedAt: now, Supported: new(true), Controls: []sdk.ContainerControl{{Name: "dry_cooler", Enabled: new(false)}, {Name: "coolant_pump"}}, Setpoints: []sdk.ContainerSetpoint{{Name: "coolant_pump_speed_hz", Value: new(0.0), Unit: "Hz"}}, MainCircuitBreakerSupported: new(true)}}
			write := func() {
				t.Helper()
				metrics := telemetry.DeviceMetrics{DeviceIdentifier: d.DeviceIdentifier, Timestamp: snapshot.ObservedAt, Container: snapshot}
				if remote {
					b, e := json.Marshal(metrics)
					require.NoError(t, e)
					metrics = telemetry.DeviceMetrics{}
					require.NoError(t, json.Unmarshal(b, &metrics))
				}
				require.NoError(t, store.StoreDeviceMetrics(ctx, metrics))
			}
			read := containers.NewService(sqlc.New(db))
			allow := func(*int64) (bool, error) { return true, nil }
			write()
			write() // transport retries are idempotent
			got, err := read.Get(ctx, org, d.DeviceIdentifier, allow)
			require.NoError(t, err)
			require.NotNil(t, got.Ha)
			require.True(t, got.Ha.Degraded)
			require.False(t, got.Ha.Stale)
			require.NotNil(t, got.Plant)
			require.False(t, got.Plant.Stale)
			require.Equal(t, 0.0, *got.Plant.DryCoolerInletTemperatureC)
			require.True(t, *got.Plant.AlarmActive)
			require.NotNil(t, got.Plant.CriticalAlarmActive)
			require.False(t, *got.Plant.CriticalAlarmActive, "an explicit false must survive projection")
			require.Nil(t, got.Plant.BuzzerActive, "an unreported buzzer must not read as silent")
			require.Len(t, got.Fans, 2)
			require.False(t, got.Fans[0].Stale)
			require.True(t, got.Fans[1].Stale)
			require.Equal(t, 0.0, *got.Fans[0].SpeedPercent)
			require.Nil(t, got.Fans[1].SpeedPercent)
			require.Equal(t, false, *got.Controls.Controls[1].Enabled)
			require.Nil(t, got.Controls.Controls[0].Enabled)
			require.Nil(t, got.Controls.MainCircuitBreakerClosed)
			require.Nil(t, got.Controls.AlarmMuteSupported)
			require.Equal(t, 0.0, *got.Controls.Setpoints[0].Value)
			_, err = read.Get(ctx, org, d.DeviceIdentifier, func(*int64) (bool, error) { return false, nil })
			require.Error(t, err)
			_, err = read.Get(ctx, org+1, d.DeviceIdentifier, allow)
			require.Error(t, err)
			// An older topology carrying a newer fan read still updates the fan.
			snapshot.ObservedAt = now.Add(-time.Minute)
			snapshot.Fans[0].ObservedAt = now
			snapshot.Fans[0].SpeedPercent = new(40.0)
			snapshot.Controls.ObservedAt = now.Add(-time.Minute)
			snapshot.Controls.Controls[0].Enabled = new(true)
			write()
			got, err = read.Get(ctx, org, d.DeviceIdentifier, allow)
			require.NoError(t, err)
			require.Equal(t, 40.0, *got.Fans[0].SpeedPercent)
			require.True(t, got.Ha.Degraded)
			require.Equal(t, false, *got.Controls.Controls[1].Enabled)
			// Newer polls cannot promote older fan samples to current readings.
			snapshot.ObservedAt = now.Add(500 * time.Millisecond)
			snapshot.HA = &sdk.ContainerHA{ObservedAt: now.Add(500 * time.Millisecond), Degraded: false}
			snapshot.Fans[0].ObservedAt = now.Add(-time.Second)
			snapshot.Fans[0].SpeedPercent = new(90.0)
			snapshot.Controls = nil
			write()
			got, err = read.Get(ctx, org, d.DeviceIdentifier, allow)
			require.NoError(t, err)
			require.Equal(t, 40.0, *got.Fans[0].SpeedPercent)
			// Failed supplementary reads leave prior observation timestamps unchanged.
			snapshot.ObservedAt = now.Add(time.Second)
			snapshot.HA = &sdk.ContainerHA{ObservedAt: now, Degraded: true}
			snapshot.Fans = nil
			snapshot.Controls = nil
			write()
			got, err = read.Get(ctx, org, d.DeviceIdentifier, allow)
			require.NoError(t, err)
			require.Equal(t, now, got.Fans[0].ObservedAt.AsTime())
			require.False(t, got.Ha.Degraded, "older failure evidence cannot overwrite recovery")
			require.Equal(t, now, got.Controls.ObservedAt.AsTime())
			require.Len(t, got.Tanks, 1)
			// A newer unsupported response replaces capabilities and old control state.
			snapshot.ObservedAt = now.Add(2 * time.Second)
			snapshot.Controls = &sdk.ContainerControls{ObservedAt: snapshot.ObservedAt, Supported: new(false)}
			write()
			got, err = read.Get(ctx, org, d.DeviceIdentifier, allow)
			require.NoError(t, err)
			require.Equal(t, false, *got.Controls.Supported)
			require.Empty(t, got.Controls.Controls)
			require.Empty(t, got.Controls.Setpoints)
		})
	}
}
