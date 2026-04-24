// Package preflight is the shared pool-assignment preflight used by both
// PreviewMiningPoolAssignment (read-only) and UpdateMiningPools (commit).
//
// Running the same function in both paths gives preview/commit parity by
// construction — the dispatch worker has nothing to decide, so there is no
// capability-flip race between preview and commit.
//
// The package is intentionally free of dependencies on the command service,
// device store, and plugin manager. Callers supply resolved device
// capability views; the package does the resolution matrix.
package preflight

import (
	"errors"
	"fmt"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
)

// SlotAssignment is one (slot, pool) pair common to every device in a
// preflight request — UpdateMiningPools and the preview RPC both assign
// the same three slots to every targeted device.
type SlotAssignment struct {
	Slot rewriter.PoolSlot
	Pool rewriter.Pool
}

// Device is a single device's contribution to the preflight: its stable
// identifier (used in warnings and error payloads) plus the already-merged
// capability view the rewriter will query.
type Device struct {
	Identifier   string
	Capabilities rewriter.DeviceCapabilities
}

// Input bundles everything the preflight needs to produce a decision for
// a batch of devices. The slot set is shared across devices; capabilities
// are per-device.
type Input struct {
	Slots   []SlotAssignment
	Devices []Device
	Proxy   rewriter.ProxyConfig
}

// SlotResult is the preflight's verdict for a single (device, slot) pair.
// RewriteReason is set on success; Warning is set on failure. They are
// mutually exclusive — a successful slot never carries a warning, and a
// failed slot never carries a reason other than UNSPECIFIED.
type SlotResult struct {
	Slot             rewriter.PoolSlot
	Protocol         poolspb.PoolProtocol
	EffectiveURL     string
	RewriteReason    rewriter.RewriteReason
	Warning          commandpb.SlotWarning
	ProtoSlot        commandpb.PoolSlot // denormalized proto enum for convenient emission
	ProtoReason      commandpb.RewriteReason
}

// DeviceResult is the preflight's verdict for one device. Slots preserves
// the input order. DeviceWarning carries any warning that applies to the
// combination of slots on this device rather than to any single slot.
type DeviceResult struct {
	DeviceIdentifier string
	Slots            []SlotResult
	DeviceWarning    commandpb.DeviceWarning
}

// Output is the aggregate result of preflight. HasMismatch is true if any
// slot or device warning is set — the commit path reads this to decide
// whether to reject the request; the preview path surfaces the detail.
type Output struct {
	Devices     []DeviceResult
	HasMismatch bool
}

// Mismatch is a flat projection of every warning in Output, shaped exactly
// like the UpdateMiningPoolsMismatch proto so the commit path can attach
// it as a FAILED_PRECONDITION detail without reshaping.
type Mismatch struct {
	DeviceIdentifier string
	Slot             commandpb.PoolSlot
	SlotWarning      commandpb.SlotWarning
	DeviceWarning    commandpb.DeviceWarning
}

// Run evaluates preflight. Never returns an error for per-device/per-slot
// mismatches — those flow as typed warnings in Output. A non-nil error
// indicates a programming problem with the input itself (e.g. duplicate
// slots, unknown slot value).
func Run(input Input) (Output, error) {
	if err := validateInput(input); err != nil {
		return Output{}, err
	}

	out := Output{
		Devices: make([]DeviceResult, 0, len(input.Devices)),
	}

	for _, device := range input.Devices {
		dr := evaluateDevice(device, input.Slots, input.Proxy)
		if dr.DeviceWarning != commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED {
			out.HasMismatch = true
		}
		for _, s := range dr.Slots {
			if s.Warning != commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED {
				out.HasMismatch = true
				break
			}
		}
		out.Devices = append(out.Devices, dr)
	}

	return out, nil
}

func evaluateDevice(device Device, slots []SlotAssignment, proxy rewriter.ProxyConfig) DeviceResult {
	// Re-use the rewriter — preflight is, structurally, "run the rewriter
	// per device and translate its typed errors onto the preview/commit
	// warning enums."
	assignments := make([]rewriter.SlotAssignment, 0, len(slots))
	for _, s := range slots {
		assignments = append(assignments, rewriter.SlotAssignment{Slot: s.Slot, Pool: s.Pool})
	}

	resolved, err := rewriter.PoolURLsForDevice(assignments, device.Capabilities, proxy)
	if err == nil {
		return successfulDeviceResult(device.Identifier, resolved)
	}

	// Device-level failure (MultipleSV2SlotsRequireProxy): map to the
	// DeviceWarning enum. Per-slot successes still surface in Slots so
	// the UI can render the complete picture.
	if errors.Is(err, rewriter.ErrMultipleSV2SlotsRequireProxy) {
		return proxyCollapseDeviceResult(device, slots, proxy)
	}

	// Slot-level failure (ErrSV2PoolNotSupportedByDevice): one specific
	// slot was rejected. We still need to report per-slot results for
	// every slot so the UI can render complete context, so fall back to
	// a per-slot resolution that swallows the error onto the offending
	// slot's warning.
	return perSlotResolution(device, slots, proxy)
}

func successfulDeviceResult(identifier string, resolved []rewriter.ResolvedSlot) DeviceResult {
	out := DeviceResult{
		DeviceIdentifier: identifier,
		Slots:            make([]SlotResult, 0, len(resolved)),
	}
	for _, r := range resolved {
		out.Slots = append(out.Slots, SlotResult{
			Slot:          r.Slot,
			Protocol:      r.Protocol,
			EffectiveURL:  r.EffectiveURL,
			RewriteReason: r.RewriteReason,
			ProtoSlot:     protoSlot(r.Slot),
			ProtoReason:   protoRewriteReason(r.RewriteReason),
		})
	}
	return out
}

// proxyCollapseDeviceResult resolves each slot individually (so the UI can
// show which slots would have been proxied) but tags the device-level
// warning so the commit path rejects the batch.
func proxyCollapseDeviceResult(device Device, slots []SlotAssignment, proxy rewriter.ProxyConfig) DeviceResult {
	// Resolve each slot in isolation; the per-slot rewriter always succeeds
	// when proxy is enabled because the multi-proxy check is a batch-level
	// concern.
	out := DeviceResult{
		DeviceIdentifier: device.Identifier,
		DeviceWarning:    commandpb.DeviceWarning_DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED,
		Slots:            make([]SlotResult, 0, len(slots)),
	}
	for _, s := range slots {
		single, err := rewriter.PoolURLsForDevice(
			[]rewriter.SlotAssignment{{Slot: s.Slot, Pool: s.Pool}},
			device.Capabilities,
			proxy,
		)
		if err != nil {
			// Should not happen since per-slot only fails when proxy is
			// disabled, but defensively tag the slot warning.
			out.Slots = append(out.Slots, SlotResult{
				Slot:        s.Slot,
				Protocol:    s.Pool.Protocol,
				Warning:     commandpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED,
				ProtoSlot:   protoSlot(s.Slot),
				ProtoReason: commandpb.RewriteReason_REWRITE_REASON_UNSPECIFIED,
			})
			continue
		}
		r := single[0]
		out.Slots = append(out.Slots, SlotResult{
			Slot:          r.Slot,
			Protocol:      r.Protocol,
			EffectiveURL:  r.EffectiveURL,
			RewriteReason: r.RewriteReason,
			ProtoSlot:     protoSlot(r.Slot),
			ProtoReason:   protoRewriteReason(r.RewriteReason),
		})
	}
	return out
}

// perSlotResolution is used when the batched rewriter returned an error
// attributable to a single slot. We re-run per-slot so we can mark the
// offender and show the OK slots intact.
func perSlotResolution(device Device, slots []SlotAssignment, proxy rewriter.ProxyConfig) DeviceResult {
	out := DeviceResult{
		DeviceIdentifier: device.Identifier,
		Slots:            make([]SlotResult, 0, len(slots)),
	}
	for _, s := range slots {
		single, err := rewriter.PoolURLsForDevice(
			[]rewriter.SlotAssignment{{Slot: s.Slot, Pool: s.Pool}},
			device.Capabilities,
			proxy,
		)
		if err != nil {
			if errors.Is(err, rewriter.ErrSV2PoolNotSupportedByDevice) {
				out.Slots = append(out.Slots, SlotResult{
					Slot:        s.Slot,
					Protocol:    s.Pool.Protocol,
					Warning:     commandpb.SlotWarning_SLOT_WARNING_SV2_NOT_SUPPORTED,
					ProtoSlot:   protoSlot(s.Slot),
					ProtoReason: commandpb.RewriteReason_REWRITE_REASON_UNSPECIFIED,
				})
				continue
			}
			// Unknown slot-level error: fall through with UNSPECIFIED
			// warning so the caller at least sees something.
			out.Slots = append(out.Slots, SlotResult{
				Slot:        s.Slot,
				Protocol:    s.Pool.Protocol,
				Warning:     commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED,
				ProtoSlot:   protoSlot(s.Slot),
				ProtoReason: commandpb.RewriteReason_REWRITE_REASON_UNSPECIFIED,
			})
			continue
		}
		r := single[0]
		out.Slots = append(out.Slots, SlotResult{
			Slot:          r.Slot,
			Protocol:      r.Protocol,
			EffectiveURL:  r.EffectiveURL,
			RewriteReason: r.RewriteReason,
			ProtoSlot:     protoSlot(r.Slot),
			ProtoReason:   protoRewriteReason(r.RewriteReason),
		})
	}
	return out
}

// Mismatches flattens Output into the commit-path FAILED_PRECONDITION
// detail shape. Returns empty when HasMismatch is false.
func (o Output) Mismatches() []Mismatch {
	var out []Mismatch
	for _, d := range o.Devices {
		if d.DeviceWarning != commandpb.DeviceWarning_DEVICE_WARNING_UNSPECIFIED {
			out = append(out, Mismatch{
				DeviceIdentifier: d.DeviceIdentifier,
				Slot:             commandpb.PoolSlot_POOL_SLOT_UNSPECIFIED,
				DeviceWarning:    d.DeviceWarning,
			})
		}
		for _, s := range d.Slots {
			if s.Warning != commandpb.SlotWarning_SLOT_WARNING_UNSPECIFIED {
				out = append(out, Mismatch{
					DeviceIdentifier: d.DeviceIdentifier,
					Slot:             s.ProtoSlot,
					SlotWarning:      s.Warning,
				})
			}
		}
	}
	return out
}

func validateInput(input Input) error {
	if len(input.Slots) == 0 {
		return errors.New("preflight: at least one slot is required")
	}
	seen := make(map[rewriter.PoolSlot]struct{}, len(input.Slots))
	for _, s := range input.Slots {
		if s.Slot == rewriter.SlotUnspecified {
			return errors.New("preflight: slot is UNSPECIFIED")
		}
		if _, dup := seen[s.Slot]; dup {
			return fmt.Errorf("preflight: duplicate slot %s", s.Slot)
		}
		seen[s.Slot] = struct{}{}
	}
	return nil
}

func protoSlot(s rewriter.PoolSlot) commandpb.PoolSlot {
	switch s {
	case rewriter.SlotDefault:
		return commandpb.PoolSlot_POOL_SLOT_DEFAULT
	case rewriter.SlotBackup1:
		return commandpb.PoolSlot_POOL_SLOT_BACKUP_1
	case rewriter.SlotBackup2:
		return commandpb.PoolSlot_POOL_SLOT_BACKUP_2
	default:
		return commandpb.PoolSlot_POOL_SLOT_UNSPECIFIED
	}
}

func protoRewriteReason(r rewriter.RewriteReason) commandpb.RewriteReason {
	switch r {
	case rewriter.ReasonPassthrough:
		return commandpb.RewriteReason_REWRITE_REASON_PASSTHROUGH
	case rewriter.ReasonNative:
		return commandpb.RewriteReason_REWRITE_REASON_NATIVE
	case rewriter.ReasonProxied:
		return commandpb.RewriteReason_REWRITE_REASON_PROXIED
	default:
		return commandpb.RewriteReason_REWRITE_REASON_UNSPECIFIED
	}
}
