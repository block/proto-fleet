// Package rewriter holds the pool-assignment rewriter — a pure function
// that turns a set of (pool, device capability, proxy config) inputs into
// the URLs that will actually be pushed to each miner.
//
// The rewriter is the single source of truth for SV2 routing decisions.
// The preflight package calls it once per (device, slot) pair at commit
// time against a consistent capability snapshot, and the per-device
// resolved URLs are written into the queue payload. Dispatch never
// re-evaluates — preview and commit agree by construction.
//
// The package is a leaf: it imports proto types and the telemetry model
// for merging capabilities, but no higher-level domain packages. This
// keeps command.Service -> preflight -> rewriter acyclic even though the
// domain pools service has its own test-time helpers that chain through
// command.
package rewriter

import (
	"errors"
	"fmt"
	"maps"

	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	modelsV2 "github.com/block/proto-fleet/server/internal/domain/telemetry/models/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Errors returned by the rewriter. Preflight maps these onto the typed
// SlotWarning / DeviceWarning enums carried by the preview and commit paths.
var (
	// ErrSV2PoolNotSupportedByDevice is returned for a single (device, slot)
	// pair when the pool speaks SV2 but the device is SV1-only and the
	// bundled translator proxy is disabled. Corresponds to SLOT_WARNING_SV2_NOT_SUPPORTED.
	ErrSV2PoolNotSupportedByDevice = errors.New("sv2 pool assignment requires either native-sv2 firmware or the bundled translator proxy")

	// ErrMultipleSV2SlotsRequireProxy is returned at device scope when more
	// than one slot would route through the single bundled proxy. Since the
	// proxy has exactly one upstream pool, primary/backup semantics would
	// silently collapse, so we reject. Corresponds to DEVICE_WARNING_MULTIPLE_SV2_SLOTS_PROXIED.
	ErrMultipleSV2SlotsRequireProxy = errors.New("more than one slot on this device would route through the bundled translator proxy; multi-proxy topology is not supported in v1")
)

// ProxyConfig is the rewriter's view of the deployment-level proxy config.
// ProxyEnabled gates whether the rewriter is allowed to emit proxied URLs
// at all; MinerURL is the stratum+* URL that SV1 miners connect to.
type ProxyConfig struct {
	ProxyEnabled bool
	MinerURL     string
}

// DeviceCapabilities is the minimal capability surface the rewriter needs.
// Implemented by the merged-view that overlays telemetry-reported SV2
// support on top of the static driver + model capabilities.
type DeviceCapabilities interface {
	Has(capability string) bool
}

// PoolSlot identifies which of the three slots (default, backup_1, backup_2)
// a pool occupies on a device. Parallels the minercommand.v1.PoolSlot enum.
type PoolSlot int

const (
	SlotUnspecified PoolSlot = iota
	SlotDefault
	SlotBackup1
	SlotBackup2
)

// Pool is the rewriter's projection of a pool assignment. Just the fields
// the rewriter cares about — keeps the package free of dependencies on
// the full pools.v1.Pool message.
type Pool struct {
	URL      string
	Protocol poolspb.PoolProtocol
}

// SlotAssignment pairs a pool with its slot for a single device.
type SlotAssignment struct {
	Slot PoolSlot
	Pool Pool
}

// RewriteReason mirrors minercommand.v1.RewriteReason so preflight can map
// straight from rewriter output to the preview/mismatch proto without
// inventing a second vocabulary.
type RewriteReason int

const (
	ReasonUnspecified RewriteReason = iota
	ReasonPassthrough               // SV1 pool pushed as-is
	ReasonNative                    // SV2 pool, device speaks native SV2
	ReasonProxied                   // SV2 pool, device is SV1, URL rewritten to the proxy
)

// ResolvedSlot is a single (slot, URL, reason) outcome of the rewriter.
// Returned in slot order so callers can line it up with the input assignments.
type ResolvedSlot struct {
	Slot          PoolSlot
	EffectiveURL  string
	Protocol      poolspb.PoolProtocol
	RewriteReason RewriteReason
}

// PoolURLsForDevice resolves every slot for a single device at once so it
// can enforce the "at most one proxied slot per device" rule, which cannot
// be expressed as a per-slot check. The slot set is accepted in any order;
// resolution is per-slot independent, but the multi-proxied-slot check is
// a property of the full set.
//
// On success returns resolved slots in input order. On failure returns:
//   - ErrSV2PoolNotSupportedByDevice when a slot can't be resolved
//     (wrapped so callers can inspect which slot).
//   - ErrMultipleSV2SlotsRequireProxy when more than one slot would proxy.
func PoolURLsForDevice(assignments []SlotAssignment, caps DeviceCapabilities, proxy ProxyConfig) ([]ResolvedSlot, error) {
	if caps == nil {
		caps = noCapabilities{}
	}

	resolved := make([]ResolvedSlot, 0, len(assignments))
	for _, a := range assignments {
		url, reason, err := resolveSingle(a.Pool, caps, proxy)
		if err != nil {
			return nil, fmt.Errorf("slot %s: %w", a.Slot, err)
		}
		// Effective protocol matches the URL we're actually pushing: when
		// the rewriter swaps an SV2 pool URL for the SV1-facing tProxy
		// URL, the slot's protocol on the wire is SV1. Derive from URL
		// scheme rather than carrying a.Pool.Protocol forward, otherwise
		// downstream surfaces (preview, dispatch payload, drivers that
		// branch on protocol) see protocol=SV2 alongside a stratum+tcp
		// URL.
		effectiveProtocol, err := ProtocolFromURL(url)
		if err != nil {
			// rewriter inputs (DB pool URL, configured proxy MinerURL)
			// were validated upstream by CEL/startup; reach here only on
			// a programming bug. Fall back to the input protocol so we
			// at least preserve the operator's intent.
			effectiveProtocol = normalizeProtocol(a.Pool.Protocol)
		}
		resolved = append(resolved, ResolvedSlot{
			Slot:          a.Slot,
			EffectiveURL:  url,
			Protocol:      effectiveProtocol,
			RewriteReason: reason,
		})
	}

	if countProxied(resolved) > 1 {
		return nil, ErrMultipleSV2SlotsRequireProxy
	}
	return resolved, nil
}

func resolveSingle(pool Pool, caps DeviceCapabilities, proxy ProxyConfig) (string, RewriteReason, error) {
	switch normalizeProtocol(pool.Protocol) {
	case poolspb.PoolProtocol_POOL_PROTOCOL_SV1:
		return pool.URL, ReasonPassthrough, nil
	case poolspb.PoolProtocol_POOL_PROTOCOL_SV2:
		if caps.Has(sdk.CapabilityStratumV2Native) {
			return pool.URL, ReasonNative, nil
		}
		if proxy.ProxyEnabled && proxy.MinerURL != "" {
			return proxy.MinerURL, ReasonProxied, nil
		}
		return "", ReasonUnspecified, ErrSV2PoolNotSupportedByDevice
	case poolspb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED:
		// normalizeProtocol collapses UNSPECIFIED to SV1, so this case is
		// unreachable in practice; kept explicit so the exhaustive linter
		// can prove the switch covers every enum member.
		return pool.URL, ReasonPassthrough, nil
	default:
		return "", ReasonUnspecified, fmt.Errorf("unknown pool protocol: %v", pool.Protocol)
	}
}

// normalizeProtocol collapses UNSPECIFIED to SV1 so internal comparisons
// have two cases instead of three. UNSPECIFIED persists as SV1 in the DB
// anyway, so at dispatch time the only way to see it is through a caller
// that passed an un-initialized struct.
func normalizeProtocol(p poolspb.PoolProtocol) poolspb.PoolProtocol {
	if p == poolspb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED {
		return poolspb.PoolProtocol_POOL_PROTOCOL_SV1
	}
	return p
}

func countProxied(resolved []ResolvedSlot) int {
	n := 0
	for _, r := range resolved {
		if r.RewriteReason == ReasonProxied {
			n++
		}
	}
	return n
}

// String renders PoolSlot with the same labels the proto enum uses so that
// wrapped errors come out readable (e.g. "slot BACKUP_1: ...").
func (s PoolSlot) String() string {
	switch s {
	case SlotDefault:
		return "DEFAULT"
	case SlotBackup1:
		return "BACKUP_1"
	case SlotBackup2:
		return "BACKUP_2"
	case SlotUnspecified:
		return "UNSPECIFIED"
	default:
		return "UNSPECIFIED"
	}
}

// String renders RewriteReason using the proto enum labels.
func (r RewriteReason) String() string {
	switch r {
	case ReasonPassthrough:
		return "PASSTHROUGH"
	case ReasonNative:
		return "NATIVE"
	case ReasonProxied:
		return "PROXIED"
	case ReasonUnspecified:
		return "UNSPECIFIED"
	default:
		return "UNSPECIFIED"
	}
}

// -----------------------------------------------------------------------------
// Capability merge — static driver caps + model caps + telemetry-reported
// SV2 support, telemetry wins. Small pure function next to the rewriter.
// -----------------------------------------------------------------------------

// MergedCapabilities is the capability view the rewriter sees for a device.
// Constructed by MergeCapabilities from the three layers described in the
// SV2 design plan. Implements DeviceCapabilities.
type MergedCapabilities struct {
	flags map[string]bool
}

// Has returns whether the merged view has the capability set to true.
// Absent and explicit-false both return false.
func (m MergedCapabilities) Has(capability string) bool {
	return m.flags[capability]
}

// MergeCapabilities merges the static driver capabilities with optional
// per-model overrides and the latest telemetry-reported SV2 support.
//
// Precedence (later wins on conflict):
//  1. static driver capabilities
//  2. model capabilities (if present)
//  3. telemetry StratumV2Support — overrides CapabilityStratumV2Native
//     when its value is SUPPORTED or UNSUPPORTED. UNSPECIFIED / UNKNOWN
//     leaves the lower-precedence view intact.
//
// All three inputs are optional — a caller without telemetry should pass
// StratumV2SupportUnknown and the function will only consider the static +
// model view.
func MergeCapabilities(static, model map[string]bool, telemetrySV2 modelsV2.StratumV2SupportStatus) MergedCapabilities {
	merged := make(map[string]bool, len(static)+len(model))
	maps.Copy(merged, static)
	maps.Copy(merged, model)
	switch telemetrySV2 {
	case modelsV2.StratumV2SupportSupported:
		merged[sdk.CapabilityStratumV2Native] = true
	case modelsV2.StratumV2SupportUnsupported:
		merged[sdk.CapabilityStratumV2Native] = false
	case modelsV2.StratumV2SupportUnknown, modelsV2.StratumV2SupportUnspecified:
		// leave lower-precedence view intact
	}
	return MergedCapabilities{flags: merged}
}

// noCapabilities implements DeviceCapabilities with every flag returning
// false. Used as the zero-value fallback when a caller passes nil caps.
type noCapabilities struct{}

func (noCapabilities) Has(string) bool { return false }
