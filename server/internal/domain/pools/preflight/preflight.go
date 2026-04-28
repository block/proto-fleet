// Package preflight rejects UpdateMiningPools requests that would
// dispatch a Stratum V2 pool URL to a miner whose latest telemetry says
// it doesn't natively speak SV2.
package preflight

import (
	"strings"

	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// Slot identifies which pool slot a URL was assigned to.
type Slot int

const (
	SlotUnspecified Slot = iota
	SlotDefault
	SlotBackup1
	SlotBackup2
)

// SlotAssignment is one (slot, URL) pair for a single device.
type SlotAssignment struct {
	Slot Slot
	URL  string
}

// Device is the input shape: identifier the operator sees, the
// last-observed SV2 capability from telemetry, and the device's
// manufacturer/model so the rejection message can name distinct types.
type Device struct {
	Identifier       string
	Make             string
	Model            string
	StratumV2Support modelsV2.StratumV2SupportStatus
}

// Mismatch is one (device, slot) rejection. Carries the device's
// make/model so the rejection message can aggregate by type.
type Mismatch struct {
	DeviceIdentifier string
	Make             string
	Model            string
	Slot             Slot
}

// Run evaluates each (device, slot) pair against the SV1↔SV2 rule and
// returns one Mismatch per offending pair. Empty result means commit
// is safe to proceed.
//
// Rule: SV2 URL only dispatches to devices whose telemetry reports
// StratumV2SupportSupported. Anything else (Unsupported, Unknown,
// Unspecified) fails closed — we don't dispatch URLs the firmware
// might not be able to speak.
func Run(devices []Device, slots []SlotAssignment) []Mismatch {
	if len(devices) == 0 || len(slots) == 0 {
		return nil
	}
	var mismatches []Mismatch
	for _, d := range devices {
		for _, s := range slots {
			if !isSV2URL(s.URL) {
				continue
			}
			if d.StratumV2Support == modelsV2.StratumV2SupportSupported {
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

func isSV2URL(stratumURL string) bool {
	return strings.HasPrefix(stratumURL, "stratum2+")
}
