package preflight

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun_SV1URLPassesAnyMiner(t *testing.T) {
	// Arrange
	devs := []Device{
		{Identifier: "a", NativeStratumV2: true},
		{Identifier: "b", NativeStratumV2: false},
		{Identifier: "c", NativeStratumV2: false},
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
		{Identifier: "native", NativeStratumV2: true},
	}
	slots := []SlotAssignment{{Slot: SlotDefault, URL: "stratum2+tcp://pool.example.com:3336/ABC"}}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Empty(t, got)
}

func TestRun_SV2URLRejectsNonNative(t *testing.T) {
	// Arrange
	devs := []Device{
		{Identifier: "sv1", Make: "Antminer", Model: "S19", NativeStratumV2: false},
		{Identifier: "unknown", Make: "Whatsminer", Model: "M30S", NativeStratumV2: false},
		{Identifier: "unspec", NativeStratumV2: false},
		{Identifier: "native", Make: "Antminer", Model: "S19j Pro", NativeStratumV2: true},
	}
	slots := []SlotAssignment{{Slot: SlotBackup1, URL: "stratum2+tcp://pool.example.com:3336/ABC"}}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Len(t, got, 3)
	for _, m := range got {
		assert.NotEqual(t, "native", m.DeviceIdentifier)
		assert.Equal(t, SlotBackup1, m.Slot)
	}
}

func TestRun_PropagatesMakeAndModel(t *testing.T) {
	// Arrange
	devs := []Device{
		{Identifier: "sv1", Make: "Antminer", Model: "S19", NativeStratumV2: false},
	}
	slots := []SlotAssignment{{Slot: SlotDefault, URL: "stratum2+tcp://pool.example.com:3336/ABC"}}

	// Act
	got := Run(devs, slots)

	// Assert
	assert.Len(t, got, 1)
	assert.Equal(t, "Antminer", got[0].Make)
	assert.Equal(t, "S19", got[0].Model)
}

func TestRun_MultipleSlotsReportPerSlot(t *testing.T) {
	// Arrange
	devs := []Device{{Identifier: "sv1", NativeStratumV2: false}}
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
