// Package driver defines the protocol-agnostic control boundary for
// facility infrastructure devices (fans, fan groups). The core domain
// stores a driver_type key plus an opaque driver_config JSON blob;
// everything protocol-specific (config schema, validation limits,
// wire I/O, timeouts) lives in an adapter package registered here.
//
// The Controller interface is deliberately shaped like a future
// protobuf InfrastructureDriver service so an adapter can later be
// extracted into a go-plugin subprocess (as miner drivers are) for
// OT-network locality or process isolation — e.g. Modbus RTU executed
// site-local on fleetnode — without changing the reconciler or the
// CRUD service.
package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
)

// PowerMode is the desired on/off state for a device.
type PowerMode int

const (
	// PowerOff requests the device stop (write 0).
	PowerOff PowerMode = iota
	// PowerOn requests the device run (write 1).
	PowerOn
)

// DesiredState carries the state a controller should drive a device
// to. It is struct-shaped so future capabilities stay additive: fan
// speed control will add an optional SpeedPercent field without
// breaking existing call sites.
type DesiredState struct {
	Power PowerMode
}

// Device is the protocol-blind view of an infrastructure device that
// adapters receive. DriverConfig is the adapter-owned JSON blob.
// InfrastructureControlSubnets is the site's canonical, commissioned
// positive allowlist, parsed once by the caller before command dispatch.
type Device struct {
	ID                           int64
	OrgID                        int64
	SiteID                       int64
	DriverType                   string
	DriverConfig                 json.RawMessage
	InfrastructureControlSubnets []netip.Prefix
}

// Controller is implemented by each driver adapter.
//
// NOTE(v2): a ReadStatus operation is reserved here for the fan
// status read-back requirement — confirming a fan is physically
// running (e.g. H-Max status word run bit / actual speed) before
// miners are restored, and blocking restore when airflow cannot be
// verified. v1 is deliberately delay-only and write-only.
type Controller interface {
	// ValidateConfig checks an opaque driver_config blob against the
	// adapter's schema and limits. Returned errors are plain; callers
	// wrap them into transport error codes.
	ValidateConfig(cfg json.RawMessage) error

	// SetState drives the device to the desired state. One protocol
	// attempt per call with a bounded timeout — repetition across
	// reconciler cycles (first-write retries and state re-assertion)
	// is owned by the caller, not the adapter.
	SetState(ctx context.Context, device Device, state DesiredState) error

	// Capabilities reports the adapter's supported feature flags,
	// e.g. {"on_off": true}. Lets future speed-capable drivers be
	// distinguished and the UI capability-gated.
	Capabilities() map[string]bool
}

// Factory constructs a Controller for a driver type.
type Factory func() Controller

// Registry maps driver_type keys to adapter factories. The CRUD
// service uses it to validate configs; the curtailment reconciler
// will use it to resolve the adapter that commands a device once the
// protocol I/O phase lands.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

// Register adds a driver type. Registering a duplicate key panics:
// registration happens at wire-up time and a collision is a
// programming error, not a runtime condition.
func (r *Registry) Register(driverType string, factory Factory) {
	if _, exists := r.factories[driverType]; exists {
		panic(fmt.Sprintf("infrastructure driver %q registered twice", driverType))
	}
	r.factories[driverType] = factory
}

// Controller resolves the adapter for a driver type.
func (r *Registry) Controller(driverType string) (Controller, error) {
	factory, ok := r.factories[driverType]
	if !ok {
		return nil, fmt.Errorf("unknown infrastructure driver type %q (supported: %v)", driverType, r.DriverTypes())
	}
	return factory(), nil
}

// ValidateConfig resolves the adapter and validates the config blob.
func (r *Registry) ValidateConfig(driverType string, cfg json.RawMessage) error {
	controller, err := r.Controller(driverType)
	if err != nil {
		return err
	}
	return controller.ValidateConfig(cfg)
}

// DriverTypes returns the registered keys, sorted for stable output.
func (r *Registry) DriverTypes() []string {
	out := make([]string, 0, len(r.factories))
	for key := range r.factories {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
