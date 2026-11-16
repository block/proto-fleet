package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

const CapabilityContainerPowerFeed = "container_power_feed"

// ContainerPowerFeedInspect reads one module's mapped power feed. It never
// changes controller state.
const ContainerPowerFeedInspect = "inspect"

// Mapping confidence for the feed that supplies one module. The plugin derives
// it from the controller map; Fleet never infers an outlet from positions.
const (
	// Every occupied board's switch-port wiring check verified the feed.
	PowerFeedMappingWiringVerified = "wiring_verified"
	// One configured feed, consistent across the module's slots and boards and
	// claimed by no other module, without wiring verification or wiring problems.
	PowerFeedMappingConfigured = "configured"
	// Feed evidence exists but disagrees or is reported as a wiring problem.
	PowerFeedMappingAmbiguous = "ambiguous"
	// The controller reports no feed for this module.
	PowerFeedMappingUnavailable = "unavailable"
)

// Authoritative controller target state for a feed.
const (
	PowerFeedStateEnabled  = "enabled"
	PowerFeedStateDisabled = "disabled"
	PowerFeedStateUnknown  = "unknown"
)

// Reasons the mapped feed has no current reading.
const (
	PowerFeedReadingSnapshotNotReady = "snapshot_not_ready"
	PowerFeedReadingNotInSnapshot    = "not_in_snapshot"
	PowerFeedReadingDuplicate        = "duplicate_records"
	PowerFeedReadingReadFailed       = "read_failed"
)

const (
	powerFeedPayloadLimit = 32 << 10
	powerFeedTextLimit    = 256
	powerFeedListLimit    = 128
)

// ContainerPowerFeedModule names the physical module. ReportedID is the module
// identity Fleet confirmed; the controller must still place it at Tank/Module.
type ContainerPowerFeedModule struct {
	Tank       int32  `json:"tank"`
	Module     int32  `json:"module"`
	ReportedID string `json:"reported_id,omitempty"`
}

// ContainerPowerFeedRequest is a closed controller operation, never a raw path.
type ContainerPowerFeedRequest struct {
	Action string                   `json:"action"`
	Module ContainerPowerFeedModule `json:"module"`
}

func (r ContainerPowerFeedRequest) ReadOnly() bool { return r.Action == ContainerPowerFeedInspect }

func (r ContainerPowerFeedRequest) Validate() error {
	if r.Action != ContainerPowerFeedInspect {
		return fmt.Errorf("unsupported container power feed operation")
	}
	m := r.Module
	if m.Tank < 1 || m.Tank > CT1TankCount || m.Module < 1 || m.Module > CT1ModulesPerTank {
		return fmt.Errorf("tank and module positions out of range")
	}
	if len(m.ReportedID) > powerFeedTextLimit || strings.TrimSpace(m.ReportedID) != m.ReportedID {
		return fmt.Errorf("invalid module identity")
	}
	return nil
}

// PowerFeedModuleRef is another module whose evidence names the same feed.
type PowerFeedModuleRef struct {
	Tank   int32 `json:"tank"`
	Module int32 `json:"module"`
}

// PowerFeedBoardEvidence is one slot's feed evidence from the controller map.
type PowerFeedBoardEvidence struct {
	Position       int32    `json:"position"`
	Occupied       bool     `json:"occupied"`
	Serial         string   `json:"serial,omitempty"`
	MACAddress     string   `json:"mac_address,omitempty"`
	SlotFeedID     string   `json:"slot_feed_id,omitempty"`
	ObservedFeedID string   `json:"observed_feed_id,omitempty"`
	ExpectedFeedID string   `json:"expected_feed_id,omitempty"`
	WiringStatus   string   `json:"wiring_status,omitempty"`
	WiringReasons  []string `json:"wiring_reasons,omitempty"`
}

// PowerFeedMapping is the derived module-to-feed association.
type PowerFeedMapping struct {
	Confidence string                   `json:"confidence"`
	FeedID     string                   `json:"feed_id,omitempty"`
	Reasons    []string                 `json:"reasons,omitempty"`
	SharedWith []PowerFeedModuleRef     `json:"shared_with,omitempty"`
	Boards     []PowerFeedBoardEvidence `json:"boards,omitempty"`
}

// PowerFeedReading is the controller's current snapshot of one feed. Absent
// measurements stay nil; a measured zero is kept.
type PowerFeedReading struct {
	ID         string   `json:"id"`
	State      string   `json:"state"`
	Active     *bool    `json:"active,omitempty"`
	Faulted    *bool    `json:"faulted,omitempty"`
	Overridden bool     `json:"overridden"`
	Phase      string   `json:"phase,omitempty"`
	VoltageV   *float64 `json:"voltage_v,omitempty"`
	CurrentA   *float64 `json:"current_a,omitempty"`
	PowerW     *float64 `json:"power_w,omitempty"`
	VoltageABV *float64 `json:"voltage_ab_v,omitempty"`
	VoltageBCV *float64 `json:"voltage_bc_v,omitempty"`
	VoltageCAV *float64 `json:"voltage_ca_v,omitempty"`
	CurrentAA  *float64 `json:"current_a_a,omitempty"`
	CurrentBA  *float64 `json:"current_b_a,omitempty"`
	CurrentCA  *float64 `json:"current_c_a,omitempty"`
}

func (r PowerFeedReading) measurements() []*float64 {
	return []*float64{r.VoltageV, r.CurrentA, r.PowerW, r.VoltageABV, r.VoltageBCV, r.VoltageCAV, r.CurrentAA, r.CurrentBA, r.CurrentCA}
}

// PowerRecoveryCycle is the controller's singleton durable restore obligation.
// While it exists the controller refuses every other feed operation.
type PowerRecoveryCycle struct {
	ID           string     `json:"id"`
	Scope        string     `json:"scope,omitempty"`
	Phase        string     `json:"phase,omitempty"`
	OutletIDs    []string   `json:"outlet_ids,omitempty"`
	RestoreAfter *time.Time `json:"restore_after,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

// ModulePowerFeedInspection is one read of a module's feed and its context.
type ModulePowerFeedInspection struct {
	ObservedAt time.Time        `json:"observed_at"`
	Mapping    PowerFeedMapping `json:"mapping"`
	// Feed is the mapped feed's reading; nil when no unique feed is mapped or
	// FeedUnavailableReason explains why the reading is missing.
	Feed                  *PowerFeedReading `json:"feed,omitempty"`
	FeedUnavailableReason string            `json:"feed_unavailable_reason,omitempty"`
	// ControllerWritePrimary is the answering controller's write authority.
	ControllerWritePrimary *bool `json:"controller_write_primary,omitempty"`
	// RecoveryCycleKnown is false when the durable cycle could not be read.
	RecoveryCycleKnown bool                `json:"recovery_cycle_known"`
	RecoveryCycle      *PowerRecoveryCycle `json:"recovery_cycle,omitempty"`
}

type ContainerPowerFeedResult struct {
	Inspection *ModulePowerFeedInspection `json:"inspection,omitempty"`
}

func validText(values ...string) bool {
	for _, v := range values {
		if len(v) > powerFeedTextLimit {
			return false
		}
	}
	return true
}

func (m PowerFeedMapping) validate() error {
	switch m.Confidence {
	case PowerFeedMappingWiringVerified, PowerFeedMappingConfigured:
		if m.FeedID == "" || len(m.SharedWith) > 0 {
			return fmt.Errorf("unique mapping requires one unshared feed")
		}
	case PowerFeedMappingAmbiguous, PowerFeedMappingUnavailable:
	default:
		return fmt.Errorf("unknown mapping confidence")
	}
	if !validText(m.FeedID) || len(m.Reasons) > powerFeedListLimit || len(m.SharedWith) > powerFeedListLimit || len(m.Boards) > powerFeedListLimit || !validText(m.Reasons...) {
		return fmt.Errorf("mapping evidence out of bounds")
	}
	for _, b := range m.Boards {
		if !validText(b.Serial, b.MACAddress, b.SlotFeedID, b.ObservedFeedID, b.ExpectedFeedID, b.WiringStatus) || len(b.WiringReasons) > powerFeedListLimit || !validText(b.WiringReasons...) {
			return fmt.Errorf("board evidence out of bounds")
		}
	}
	return nil
}

func (r ContainerPowerFeedResult) Validate(req ContainerPowerFeedRequest) error {
	if req.Action != ContainerPowerFeedInspect {
		return fmt.Errorf("unsupported container power feed result")
	}
	i := r.Inspection
	if i == nil || i.ObservedAt.IsZero() {
		return fmt.Errorf("power feed inspection required")
	}
	if err := i.Mapping.validate(); err != nil {
		return err
	}
	switch i.FeedUnavailableReason {
	case "", PowerFeedReadingSnapshotNotReady, PowerFeedReadingNotInSnapshot, PowerFeedReadingDuplicate, PowerFeedReadingReadFailed:
	default:
		return fmt.Errorf("unknown feed unavailable reason")
	}
	if f := i.Feed; f != nil {
		if i.FeedUnavailableReason != "" || f.ID == "" || f.ID != i.Mapping.FeedID || !validText(f.ID, f.Phase) {
			return fmt.Errorf("feed reading must belong to the mapped feed")
		}
		switch f.State {
		case PowerFeedStateEnabled, PowerFeedStateDisabled, PowerFeedStateUnknown:
		default:
			return fmt.Errorf("unknown feed state")
		}
		for _, v := range f.measurements() {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return fmt.Errorf("non-finite feed measurement")
			}
		}
	}
	if c := i.RecoveryCycle; c != nil {
		if !i.RecoveryCycleKnown || c.ID == "" || !validText(c.ID, c.Scope, c.Phase, c.LastError) || len(c.OutletIDs) > powerFeedListLimit || !validText(c.OutletIDs...) {
			return fmt.Errorf("invalid recovery cycle")
		}
	}
	return nil
}

// decodePowerFeed rejects unknown fields only when strict. Requests are strict
// because an unknown field may carry intent the receiver would drop; results
// are lenient so a newer node or plugin may add result fields without a new
// command protocol version.
func decodePowerFeed(data []byte, out any, strict bool) error {
	if len(data) > powerFeedPayloadLimit {
		return fmt.Errorf("container power feed payload too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid container power feed payload: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing container power feed payload")
	}
	return nil
}

func DecodeContainerPowerFeedRequest(data []byte) (ContainerPowerFeedRequest, error) {
	var r ContainerPowerFeedRequest
	if err := decodePowerFeed(data, &r, true); err != nil {
		return r, err
	}
	return r, r.Validate()
}

func DecodeContainerPowerFeedResult(data []byte, req ContainerPowerFeedRequest) (ContainerPowerFeedResult, error) {
	var r ContainerPowerFeedResult
	if err := decodePowerFeed(data, &r, false); err != nil {
		return r, err
	}
	return r, r.Validate(req)
}

// DeviceContainerPowerFeed reads container power feeds for one module.
type DeviceContainerPowerFeed interface {
	ContainerPowerFeed(ctx context.Context, request ContainerPowerFeedRequest) (ContainerPowerFeedResult, error)
}
