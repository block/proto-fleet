package preflight

import (
	"testing"

	mcpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	"github.com/stretchr/testify/assert"
)

func TestRun_SV1URLPassesAnyMiner(t *testing.T) {
	// Arrange
	devs := []Device{
		{Identifier: "a", StratumV2Support: modelsV2.StratumV2SupportSupported},
		{Identifier: "b", StratumV2Support: modelsV2.StratumV2SupportUnsupported},
		{Identifier: "c", StratumV2Support: modelsV2.StratumV2SupportUnknown},
	}
	slots := []SlotAssignment{{Slot: SlotDefault, URL: "stratum+tcp://pool.example.com:3333"}}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Empty(t, got)
}

func TestRun_SV2URLPassesNativeOnly(t *testing.T) {
	// Arrange
	devs := []Device{
		{Identifier: "native", StratumV2Support: modelsV2.StratumV2SupportSupported},
	}
	slots := []SlotAssignment{{Slot: SlotDefault, URL: "stratum2+tcp://pool.example.com:3336/ABC"}}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Empty(t, got)
}

func TestRun_SV2URLRejectsSV1AndUnknown(t *testing.T) {
	// Arrange
	devs := []Device{
		{Identifier: "sv1", StratumV2Support: modelsV2.StratumV2SupportUnsupported},
		{Identifier: "unknown", StratumV2Support: modelsV2.StratumV2SupportUnknown},
		{Identifier: "unspec", StratumV2Support: modelsV2.StratumV2SupportUnspecified},
		{Identifier: "native", StratumV2Support: modelsV2.StratumV2SupportSupported},
	}
	slots := []SlotAssignment{{Slot: SlotBackup1, URL: "stratum2+tcp://pool.example.com:3336/ABC"}}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Len(t, got, 3)
	for _, m := range got {
		assert.NotEqual(t, "native", m.DeviceIdentifier)
		assert.Equal(t, SlotBackup1, m.Slot)
		assert.Equal(t, mcpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED, m.SlotWarning)
	}
}

func TestRun_MultipleSlotsReportPerSlot(t *testing.T) {
	// Arrange
	devs := []Device{{Identifier: "sv1", StratumV2Support: modelsV2.StratumV2SupportUnsupported}}
	slots := []SlotAssignment{
		{Slot: SlotDefault, URL: "stratum2+tcp://a:3336/k"},
		{Slot: SlotBackup1, URL: "stratum+tcp://b:3333"},
		{Slot: SlotBackup2, URL: "stratum2+tcp://c:3336/k"},
	}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Len(t, got, 2)
	slotsHit := []Slot{got[0].Slot, got[1].Slot}
	assert.Contains(t, slotsHit, SlotDefault)
	assert.Contains(t, slotsHit, SlotBackup2)
}

func TestSlot_ProtoSlot(t *testing.T) {
	// Act + Assert
	assert.Equal(t, mcpb.PoolSlot_POOL_SLOT_DEFAULT, SlotDefault.ProtoSlot())
	assert.Equal(t, mcpb.PoolSlot_POOL_SLOT_BACKUP_1, SlotBackup1.ProtoSlot())
	assert.Equal(t, mcpb.PoolSlot_POOL_SLOT_BACKUP_2, SlotBackup2.ProtoSlot())
	assert.Equal(t, mcpb.PoolSlot_POOL_SLOT_UNSPECIFIED, SlotUnspecified.ProtoSlot())
}
