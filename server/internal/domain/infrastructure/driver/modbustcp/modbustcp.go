// Package modbustcp is the Modbus TCP driver adapter for facility
// infrastructure devices. v1 scope is config parsing and validation;
// the write path (FC5 coil / FC6 holding-register 0/1 writes) lands
// with the protocol I/O phase of the facility fan plan.
package modbustcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"

	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
)

// DriverType is the registry key for this adapter.
const DriverType = "modbus_tcp"

const (
	// WriteModeCoil writes the RUN/STOP coil via function code 5.
	WriteModeCoil = "coil"
	// WriteModeHoldingRegister writes the control word register via
	// function code 6.
	WriteModeHoldingRegister = "holding_register"

	minUnitID = 1
	maxUnitID = 247
	minPort   = 1
	maxPort   = 65535
	// Register addresses are raw application addresses (e.g. 2001 for
	// the H-Max FB Control Word, 0001 for the RUN/STOP coil) — not the
	// 4xxxx-prefixed reference convention. The adapter owns any
	// wire-level off-by-one translation.
	maxRegisterAddress = 65535
)

// Config is the adapter-owned driver_config schema.
type Config struct {
	// Endpoint is the device's IP address. Modbus TCP carries no
	// authentication, so only private (RFC1918 / IPv6 ULA) addresses
	// are accepted; loopback, link-local, and public addresses are
	// rejected — see validateEndpoint for the rationale.
	Endpoint string `json:"endpoint"`
	Port     int    `json:"port"`
	UnitID   int    `json:"unit_id"`
	// RegisterAddress is a pointer because 0 is a valid raw address
	// (the RUN/STOP coil) — unlike every other field, the zero value
	// cannot double as "missing". Without presence tracking, an
	// omitted or null register_address would silently validate as
	// register 0 and the write path would command the wrong register.
	RegisterAddress *int   `json:"register_address"`
	WriteMode       string `json:"write_mode"`
}

// Controller implements driver.Controller for Modbus TCP.
type Controller struct{}

var _ driver.Controller = Controller{}

// New is the driver.Factory for this adapter.
func New() driver.Controller {
	return Controller{}
}

// ParseConfig decodes and validates a driver_config blob.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, errors.New("driver_config is required for modbus_tcp devices")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, decodeError(err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// decodeError maps a json.Unmarshal failure to a message that never
// echoes the submitted value — the same no-echo policy as validate().
// Raw decoder errors are unsafe here: a numeric overflow error embeds
// the literal ("cannot unmarshal number 99999999999 into ... field
// port"), and syntax errors echo the offending character. Type errors
// keep the field name and expected type, which is enough to correct
// the submission without leaking what was sent.
func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return fmt.Errorf("driver_config field %q must be a valid %s", typeErr.Field, typeErr.Type)
	}
	return errors.New("driver_config is not valid JSON")
}

// validate returns field-only messages that never echo the submitted
// value — the same policy validateEndpoint documents. driver_config is
// OT control topology (unit IDs, register addresses), and validation
// errors reach server error logs even for sensitive-body procedures,
// so a near-miss submission next to a real control value must not
// leak it. The caller already knows what they sent; naming the field
// and the accepted range is enough to correct it.
func (c Config) validate() error {
	if err := validateEndpoint(c.Endpoint); err != nil {
		return err
	}
	if c.Port < minPort || c.Port > maxPort {
		return fmt.Errorf("port must be between %d and %d", minPort, maxPort)
	}
	if c.UnitID < minUnitID || c.UnitID > maxUnitID {
		return fmt.Errorf("unit_id must be between %d and %d", minUnitID, maxUnitID)
	}
	if c.RegisterAddress == nil {
		return errors.New("register_address is required")
	}
	if *c.RegisterAddress < 0 || *c.RegisterAddress > maxRegisterAddress {
		return fmt.Errorf("register_address must be between 0 and %d", maxRegisterAddress)
	}
	if c.WriteMode != WriteModeCoil && c.WriteMode != WriteModeHoldingRegister {
		return fmt.Errorf("write_mode must be %q or %q", WriteModeCoil, WriteModeHoldingRegister)
	}
	return nil
}

// validateEndpoint restricts endpoints to private (RFC1918 / IPv6 ULA)
// addresses. The server will open raw TCP connections and write
// unauthenticated Modbus frames to this address, so an unrestricted
// endpoint would be an SSRF/OT-pivot primitive for anyone holding
// site:manage. Loopback and link-local are deliberately rejected too:
// a real PLC/drive lives on a private OT subnet, whereas loopback
// targets server-local services and link-local includes cloud
// instance-metadata (169.254.169.254). Multicast, broadcast, and
// unspecified addresses are not private and fail the same check. If a
// site genuinely needs a non-RFC1918 control endpoint, that should be
// an explicit per-site allowlist decision, not a blanket allowance.
func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return errors.New("endpoint is required")
	}
	// Error messages deliberately do not echo the submitted value:
	// validation errors are recorded in server error logs (the request
	// logger logs err even for sensitive-body procedures), and a
	// near-miss submission — e.g. a real OT IP with a trailing space —
	// would otherwise leak control-network addresses into logs the
	// body redaction exists to protect.
	addr, err := netip.ParseAddr(endpoint)
	if err != nil {
		return errors.New("endpoint must be an IP address (hostnames are not supported)")
	}
	if !addr.IsPrivate() {
		return errors.New("endpoint must be a private (RFC1918 / IPv6 ULA) IP address")
	}
	return nil
}

// ValidateConfig implements driver.Controller.
func (Controller) ValidateConfig(raw json.RawMessage) error {
	_, err := ParseConfig(raw)
	return err
}

// SetState implements driver.Controller. Protocol I/O is out of scope
// for the backend phase; the reconciler sequencing work wires the
// actual FC5/FC6 write.
//
// SECURITY PRECONDITION for the write-path implementation: RFC1918 is
// a save-time bound, not a dial-time authorization. Before any frame
// is sent, the implementation must enforce a per-site commissioned
// control-subnet allowlist (and reject server-infrastructure CIDRs),
// so a site:manage caller cannot point the server's raw Modbus writer
// at unrelated private infrastructure or another site's OT segment.
// Which subnet is "the OT subnet" is per-site commissioning data that
// lands with the write path; do not enable writes without it.
func (Controller) SetState(_ context.Context, device driver.Device, _ driver.DesiredState) error {
	return fmt.Errorf("modbus_tcp write path is not implemented yet (device %q)", device.Name)
}

// Capabilities implements driver.Controller.
func (Controller) Capabilities() map[string]bool {
	return map[string]bool{"on_off": true}
}
