package sdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContainerSnapshotSurvivesTelemetryTransport(t *testing.T) {
	now := time.Now().UTC()
	snapshot := &ContainerSnapshot{ObservedAt: now, Tanks: []ContainerTank{{Position: 2, Modules: []ContainerModule{{Position: 3, ReportedID: "module-serial", State: ModuleAttention, ObservedAt: now.Add(-119 * time.Second)}}}}}
	pue := 1.05
	snapshot.TopologyComplete = true
	zero, hashrate := 0.0, 300e12
	snapshot.Tanks[0].Modules[0].Metrics = &ContainerModuleMetrics{ObservedAt: now, HashrateHS: &hashrate, PowerW: &zero}
	snapshot.Plant = &ContainerPlant{ObservedAt: now, PUE: &pue, AlarmActive: true}
	snapshot.Fans = []ContainerFan{{ComponentID: "fan-1", ObservedAt: now.Add(-time.Minute), SpeedPercent: &zero, AlarmActive: new(false)}, {ComponentID: "fan-2", ObservedAt: now}}
	snapshot.Controls = &ContainerControls{ObservedAt: now, Supported: new(true), Controls: []ContainerControl{{Name: "dry_cooler", Enabled: new(false)}, {Name: "coolant_pump"}}, Setpoints: []ContainerSetpoint{{Name: "coolant_pump_speed_hz", Value: &zero, Unit: "Hz"}}, MainCircuitBreakerSupported: new(true)}
	wire := deviceMetricsToProto(DeviceMetrics{DeviceID: "controller", Timestamp: now, Container: snapshot})
	got := deviceMetricsFromProto(wire)
	require.Equal(t, snapshot, got.Container)
	require.Nil(t, deviceMetricsFromProto(deviceMetricsToProto(DeviceMetrics{Timestamp: now})).Container)
}
