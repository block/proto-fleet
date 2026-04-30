// Package preflight gates SV2 URL assignments on per-device native-SV2
// capability reported by the plugin via GetCapabilitiesForModel.
package preflight

import (
	"github.com/block/proto-fleet/server/internal/domain/sv2"
)

type Slot int

const (
	SlotUnspecified Slot = iota
	SlotDefault
	SlotBackup1
	SlotBackup2
)

type SlotAssignment struct {
	Slot Slot
	URL  string
}

// Device pairs the operator-facing identifier with the firmware-derived
// make/model (for the rejection toast) and the native-SV2 capability.
type Device struct {
	Identifier      string
	Make            string
	Model           string
	NativeStratumV2 bool
}

type Mismatch struct {
	DeviceIdentifier string
	Make             string
	Model            string
	Slot             Slot
}

// Run returns one Mismatch per (device, slot) pair that pairs an SV2
// URL with a non-native-SV2 device. Fails closed: only NativeStratumV2
// = true passes.
func Run(devices []Device, slots []SlotAssignment) []Mismatch {
	if len(devices) == 0 || len(slots) == 0 {
		return nil
	}
	var mismatches []Mismatch
	for _, d := range devices {
		for _, s := range slots {
			if !sv2.IsSV2URL(s.URL) {
				continue
			}
			if d.NativeStratumV2 {
				continue
			}
			mismatches = append(mismatches, Mismatch{
				DeviceIdentifier: d.Identifier,
				Make:             d.Make,
				Model:            d.Model,
				Slot:             s.Slot,
			})
		}
	}
	return mismatches
}

