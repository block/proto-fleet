// Package modbustcp is the Modbus TCP driver adapter for facility
// infrastructure devices.
package modbustcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/grid-x/modbus"

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
	// Register addresses use the one-based application convention (e.g.
	// 2001 for the H-Max FB Control Word, 0001 for the RUN/STOP coil), not
	// the 4xxxx-prefixed reference convention or zero-based wire address.
	// Supporting 1..65536 makes every 16-bit wire address reachable without
	// allowing 0 and 1 to alias the same physical target.
	minRegisterAddress = 1
	maxRegisterAddress = 65536

	defaultWriteTimeout = 3 * time.Second
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
	// RegisterAddress is a pointer so omitted/null can be distinguished from
	// an explicitly submitted out-of-range value.
	RegisterAddress *int   `json:"register_address"`
	WriteMode       string `json:"write_mode"`
}

// DialFunc opens the one connection used by a SetState call.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Option configures a Modbus TCP controller.
type Option func(*Controller)

// WithDialer supplies the connection seam used by SetState. Production uses a
// net.Dialer; tests use it to inspect real Modbus framing without authorizing
// loopback as an OT endpoint.
func WithDialer(dial DialFunc) Option {
	return func(c *Controller) {
		c.dial = dial
	}
}

// Controller implements driver.Controller for Modbus TCP.
type Controller struct {
	globalControlSubnets []netip.Prefix
	dial                 DialFunc
}

var _ driver.Controller = (*Controller)(nil)

// New returns a fail-closed controller without a deployment allowlist.
func New() driver.Controller {
	return NewConfigured(nil)
}

// NewConfigured returns a controller protected by the deployment-global
// positive OT control-subnet allowlist.
func NewConfigured(globalControlSubnets []netip.Prefix, options ...Option) driver.Controller {
	controller := &Controller{
		globalControlSubnets: append([]netip.Prefix(nil), globalControlSubnets...),
		dial:                 (&net.Dialer{}).DialContext,
	}
	for _, option := range options {
		option(controller)
	}
	return controller
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
	if *c.RegisterAddress < minRegisterAddress || *c.RegisterAddress > maxRegisterAddress {
		return fmt.Errorf(
			"register_address must be between %d and %d",
			minRegisterAddress,
			maxRegisterAddress,
		)
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

// SetState performs one bounded FC5 or FC6 request. It reparses the opaque
// config and applies both positive allowlists immediately before dialing.
func (c Controller) SetState(ctx context.Context, device driver.Device, state driver.DesiredState) error {
	cfg, err := ParseConfig(device.DriverConfig)
	if err != nil {
		return fmt.Errorf("modbus_tcp configuration invalid for device %d: %w", device.ID, err)
	}

	if device.OrgID <= 0 || device.SiteID <= 0 || !validPrefixes(device.InfrastructureControlSubnets) {
		return fmt.Errorf("device %d is not commissioned for infrastructure control", device.ID)
	}
	if !validPrefixes(c.globalControlSubnets) {
		return errors.New("deployment allowlist is missing for infrastructure control")
	}

	var registerValue uint16
	switch state.Power {
	case driver.PowerOff:
		registerValue = 0
	case driver.PowerOn:
		registerValue = 1
	default:
		return fmt.Errorf("power mode is invalid for device %d", device.ID)
	}

	endpoint, err := netip.ParseAddr(cfg.Endpoint)
	if err != nil {
		// ParseConfig already enforces this. Keep a fail-closed guard at the
		// authorization boundary in case its contract changes.
		return fmt.Errorf("modbus_tcp configuration invalid for device %d", device.ID)
	}
	if !contains(device.InfrastructureControlSubnets, endpoint) ||
		!contains(c.globalControlSubnets, endpoint) {
		return fmt.Errorf("device %d endpoint is not authorized for infrastructure control", device.ID)
	}

	timeout := defaultWriteTimeout
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := writeCtx.Err(); err != nil {
		return contextError(device.ID, err)
	}

	requestTimeout := timeout
	if deadline, ok := writeCtx.Deadline(); ok {
		requestTimeout = time.Until(deadline)
		if requestTimeout <= 0 {
			return contextError(device.ID, context.DeadlineExceeded)
		}
	}

	handler := modbus.NewTCPClientHandler(
		net.JoinHostPort(cfg.Endpoint, strconv.Itoa(cfg.Port)),
		modbus.WithDialer(c.contextDialer(writeCtx)),
	)
	handler.Timeout = requestTimeout
	// grid-x uses zero idle timeout for a single non-cached Send, and zero
	// recovery budgets disable its reconnect and response-recovery loops.
	handler.IdleTimeout = 0
	handler.LinkRecoveryTimeout = 0
	handler.ProtocolRecoveryTimeout = 0
	handler.SetSlave(byte(cfg.UnitID))
	client := modbus.NewClient(handler)

	address := applicationAddressToWire(*cfg.RegisterAddress)
	switch cfg.WriteMode {
	case WriteModeCoil:
		coilValue := uint16(0x0000)
		if registerValue == 1 {
			coilValue = 0xFF00
		}
		_, err = client.WriteSingleCoil(writeCtx, address, coilValue)
		if err != nil {
			return writeError(writeCtx, device.ID, "coil", err)
		}
	case WriteModeHoldingRegister:
		_, err = client.WriteSingleRegister(writeCtx, address, registerValue)
		if err != nil {
			return writeError(writeCtx, device.ID, "register", err)
		}
	default:
		// ParseConfig already enforces this; retain a fail-closed command-time
		// guard if validation is ever loosened independently.
		return fmt.Errorf("modbus_tcp configuration invalid for device %d", device.ID)
	}
	return nil
}

func (c Controller) contextDialer(ctx context.Context) modbus.DialFunc {
	dial := c.dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	return func(_ context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		context.AfterFunc(ctx, func() {
			_ = conn.SetDeadline(time.Now())
		})
		return conn, nil
	}
}

func validPrefixes(prefixes []netip.Prefix) bool {
	if len(prefixes) == 0 {
		return false
	}
	for _, prefix := range prefixes {
		if !prefix.IsValid() {
			return false
		}
	}
	return true
}

func contains(prefixes []netip.Prefix, endpoint netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(endpoint) {
			return true
		}
	}
	return false
}

func applicationAddressToWire(address int) uint16 {
	if address < minRegisterAddress || address > maxRegisterAddress {
		return 0
	}
	return uint16(address - 1) // #nosec G115 -- the bounds check above proves this conversion safe.
}

func writeError(ctx context.Context, deviceID int64, kind string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return contextError(deviceID, ctxErr)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("modbus_tcp write timed out for device %d", deviceID)
	}
	return fmt.Errorf("modbus_tcp %s write failed for device %d", kind, deviceID)
}

func contextError(deviceID int64, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("modbus_tcp write timed out for device %d", deviceID)
	}
	return fmt.Errorf("modbus_tcp write canceled for device %d", deviceID)
}

// Capabilities implements driver.Controller.
func (Controller) Capabilities() map[string]bool {
	return map[string]bool{"on_off": true}
}
