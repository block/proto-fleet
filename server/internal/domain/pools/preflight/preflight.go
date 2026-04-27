// Package preflight rejects UpdateMiningPools requests that would
// dispatch a Stratum V2 pool URL to a miner whose latest telemetry says
// it doesn't natively speak SV2.
package preflight

import (
	"strings"

	mcpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
)

// Slot mirrors mcpb.PoolSlot at the package boundary so callers don't
// have to import the proto package just to express priority.
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

// Device is the input shape: identifier the operator sees, plus the
// last-observed SV2 capability from telemetry.
type Device struct {
	Identifier       string
	StratumV2Support modelsV2.StratumV2SupportStatus
}

// Mismatch is one (device, slot) rejection. Maps directly to the proto
// detail attached to the FAILED_PRECONDITION error.
type Mismatch struct {
	DeviceIdentifier string
	Slot             Slot
	SlotWarning      mcpb.SlotWarning
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
				Slot:             s.Slot,
				SlotWarning:      mcpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED,
			})
		}
	}
	return mismatches
}

func isSV2URL(stratumURL string) bool {
	return strings.HasPrefix(stratumURL, "stratum2+")
}

// ProtoSlot projects a Slot into the proto enum.
func (s Slot) ProtoSlot() mcpb.PoolSlot {
	switch s {
	case SlotDefault:
		return mcpb.PoolSlot_POOL_SLOT_DEFAULT
	case SlotBackup1:
		return mcpb.PoolSlot_POOL_SLOT_BACKUP_1
	case SlotBackup2:
		return mcpb.PoolSlot_POOL_SLOT_BACKUP_2
	default:
		return mcpb.PoolSlot_POOL_SLOT_UNSPECIFIED
	}
}
