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
	snapshot.Plant = &ContainerPlant{ObservedAt: now, PUE: &pue, AlarmActive: true}
	wire := deviceMetricsToProto(DeviceMetrics{DeviceID: "controller", Timestamp: now, Container: snapshot})
	got := deviceMetricsFromProto(wire)
	require.Equal(t, snapshot, got.Container)
	require.Nil(t, deviceMetricsFromProto(deviceMetricsToProto(DeviceMetrics{Timestamp: now})).Container)
}
