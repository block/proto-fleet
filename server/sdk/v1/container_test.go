package sdk

import (
	"encoding/json"
	"testing"
	"time"

	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"

	"github.com/stretchr/testify/require"
)

func TestContainerSnapshotSurvivesTelemetryTransport(t *testing.T) {
	now := time.Now().UTC()
	snapshot := &ContainerSnapshot{ObservedAt: now, Tanks: []ContainerTank{{Position: 2, Modules: []ContainerModule{{Position: 3, ReportedID: "module-serial", IdentityConfirmed: true, State: MinerStatusError, ObservedAt: now.Add(-119 * time.Second)}}}}}
	pue := 1.05
	snapshot.TopologyComplete = true
	snapshot.Metrics = &ContainerRollup{UnlocatedHashboards: 6, SourceTimes: map[string]time.Time{}}
	zero, hashrate := 0.0, 300e12
	snapshot.Tanks[0].Modules[0].Metrics = &ContainerModuleMetrics{ObservedAt: now, HashrateHS: &hashrate, PowerW: &zero}
	snapshot.Tanks[0].Modules[0].Issues = &ContainerModuleIssues{SourceTimes: map[string]time.Time{"hashboard:board-mac": now}, ObservedAt: now, Complete: true, Items: []ContainerModuleIssue{{Component: "hashboard", ComponentID: "board-mac", Cause: "BOARD_OVER_TEMPERATURE", CanonicalError: sdkerrors.HashboardOverTemperature, Severity: "fault", ObservedAt: now}}}
	snapshot.Tanks[0].Modules[0].Hashboards = []ContainerHashboard{
		{Position: 1, Occupancy: "occupied", MACAddress: "aa:bb:cc:dd:ee:01", Serial: "board-a", State: MinerStatusActive, MiningState: "MINING_STATE_MINING", Isolated: new(false), ObservedAt: now, StateObservedAt: now, HashrateHS: &zero, HashrailOK: new(false), MetadataJSON: json.RawMessage(`{"serial":"board-a"}`), HealthJSON: json.RawMessage(`{"uptime":"9007199254740993","errors":[]}`)},
		{Position: 2, Occupancy: "empty", State: MinerStatusUnknown},
		UnavailableHashboard(3, HashboardIdentityConflict),
	}
	snapshot.UnlocatedHashboards = []ContainerHashboard{
		{Occupancy: "occupied", MACAddress: "aa:bb:cc:dd:ee:02", Serial: "unlocated", ObservedAt: now, HashrateHS: &hashrate, TemperatureC: new(65.0)},
		{Occupancy: "unknown", IdentityClaims: []HashboardIdentityClaim{{MACAddress: "rejected"}}, UnavailableReason: HashboardIdentityConflict},
	}
	snapshot.Tanks[0].Modules[0].Hashboards[2].IdentityClaims = []HashboardIdentityClaim{{MACAddress: "rejected-mac", Serial: "rejected-serial"}}
	snapshot.Plant = &ContainerPlant{ObservedAt: now, PUE: &pue, AlarmActive: true, DryCoolerInletTemperatureC: &zero}
	snapshot.Fans = []ContainerFan{{ComponentID: "fan-1", ObservedAt: now.Add(-time.Minute), SpeedPercent: &zero, AlarmActive: new(false)}, {ComponentID: "fan-2", ObservedAt: now}}
	snapshot.Controls = &ContainerControls{ObservedAt: now, Supported: new(true), Controls: []ContainerControl{{Name: "dry_cooler", Enabled: new(false)}, {Name: "coolant_pump"}}, Setpoints: []ContainerSetpoint{{Name: "coolant_pump_speed_hz", Value: &zero, Unit: "Hz"}}, MainCircuitBreakerSupported: new(true)}
	wire := deviceMetricsToProto(DeviceMetrics{DeviceID: "controller", Timestamp: now, Container: snapshot})
	got := deviceMetricsFromProto(wire)
	require.Equal(t, snapshot, got.Container)
	snapshot.Tanks[0].Modules[0].IdentityConfirmed = false
	require.Equal(t, snapshot, deviceMetricsFromProto(deviceMetricsToProto(DeviceMetrics{DeviceID: "controller", Timestamp: now, Container: snapshot})).Container)
	require.Nil(t, deviceMetricsFromProto(deviceMetricsToProto(DeviceMetrics{Timestamp: now})).Container)
}
