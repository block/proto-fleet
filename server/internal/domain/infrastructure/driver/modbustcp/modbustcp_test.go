package modbustcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modbussim "github.com/block/proto-fleet/server/devtools/modbussim/server"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
)

func validConfigJSON(t *testing.T, mutate func(m map[string]any)) json.RawMessage {
	t.Helper()
	m := map[string]any{
		"endpoint":         "10.20.30.40",
		"port":             502,
		"unit_id":          1,
		"register_address": 2001,
		"write_mode":       WriteModeHoldingRegister,
	}
	if mutate != nil {
		mutate(m)
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return raw
}

func TestValidateConfig_Valid(t *testing.T) {
	c := Controller{}
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, nil)))
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["write_mode"] = WriteModeCoil
		m["register_address"] = 1
	})))
	// The complete one-based application range maps onto every 16-bit wire
	// address.
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["write_mode"] = WriteModeCoil
		m["register_address"] = 1
	})))
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["register_address"] = 65536
	})))
	// Private RFC1918 ranges are allowed.
	for _, endpoint := range []string{"10.0.0.5", "172.16.4.9", "192.168.1.50"} {
		assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
			m["endpoint"] = endpoint
		})), "private endpoint %q should be accepted", endpoint)
	}
	// Private IPv6 (ULA) is allowed.
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["endpoint"] = "fd00::1"
	})))
}

func TestValidateConfig_RejectsPublicOrHostnameEndpoints(t *testing.T) {
	c := Controller{}
	for _, endpoint := range []string{
		"8.8.8.8",         // public IPv4
		"2001:4860::8888", // public IPv6
		"plc.example.com", // hostname
		"",                // missing
		"0.0.0.0",         // unspecified IPv4
		"::",              // unspecified IPv6
		"255.255.255.255", // broadcast
		"239.1.2.3",       // multicast IPv4
		"ff02::1",         // multicast IPv6
		"100.64.10.20",    // CGNAT shared space — not RFC1918, rejected
	} {
		err := c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
			m["endpoint"] = endpoint
		}))
		assert.Error(t, err, "endpoint %q should be rejected", endpoint)
		// Validation errors reach server error logs via the request
		// logger, so they must not echo the submitted endpoint — a
		// near-miss (real OT IP with a typo) would otherwise leak
		// control-network addresses despite body redaction.
		if endpoint != "" {
			assert.NotContains(t, err.Error(), endpoint,
				"validation error must not echo the submitted endpoint")
		}
	}
}

func TestValidateConfig_RejectsIPv4MappedIPv6EndpointsWithoutEcho(t *testing.T) {
	c := Controller{}
	for _, endpoint := range []string{
		"::ffff:10.20.30.40", // mapped private IPv4
		"::ffff:8.8.8.8",     // mapped public IPv4
	} {
		raw := validConfigJSON(t, func(m map[string]any) {
			m["endpoint"] = endpoint
		})

		err := c.ValidateConfig(raw)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), endpoint)
		assert.NotContains(t, err.Error(), string(raw))
	}
}

func TestValidateConfig_RejectsOutOfRangeFields(t *testing.T) {
	c := Controller{}
	// Rejected values sit outside the accepted ranges but are chosen so
	// their decimal form does not appear in the range text of the error
	// message itself, letting the no-echo assertion below hold: like
	// endpoints, unit IDs and register addresses are OT topology, and a
	// near-miss next to a real control value must not land in server
	// logs via the request logger.
	cases := []struct {
		field string
		value any
	}{
		{"unit_id", 0},
		{"unit_id", 248},
		{"port", 0},
		{"port", 78901},
		{"register_address", -1},
		{"register_address", 78901},
		{"write_mode", "toggle"},
		{"write_mode", ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s=%v", tc.field, tc.value), func(t *testing.T) {
			err := c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
				m[tc.field] = tc.value
			}))
			assert.Error(t, err)
			if s := fmt.Sprintf("%v", tc.value); s != "" && s != "0" {
				assert.NotContains(t, err.Error(), s,
					"validation error must not echo the submitted value")
			}
		})
	}
}

func TestValidateConfig_RejectsLegacyZeroBasedRegisterAddress(t *testing.T) {
	const want = "device uses the legacy zero-based register-address convention and must be recommissioned with a one-based application address"
	raw := validConfigJSON(t, func(m map[string]any) {
		m["endpoint"] = "10.88.77.66"
		m["port"] = 15020
		m["unit_id"] = 247
		m["register_address"] = 0
	})

	err := Controller{}.ValidateConfig(raw)
	require.EqualError(t, err, want)
	for _, topology := range []string{"10.88.77.66", "15020", "247", "0", string(raw)} {
		assert.NotContains(t, err.Error(), topology,
			"legacy-address error must not echo submitted topology")
	}
}

func TestValidateConfig_RejectsMalformedBlob(t *testing.T) {
	c := Controller{}
	assert.Error(t, c.ValidateConfig(nil))
	assert.Error(t, c.ValidateConfig(json.RawMessage(`not json`)))
}

func TestValidateConfig_RejectsMissingOrNullRegisterAddress(t *testing.T) {
	// Omitted and null values must not silently decode to application
	// address 0.
	c := Controller{}

	err := c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		delete(m, "register_address")
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register_address is required")

	err = c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["register_address"] = nil
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register_address is required")
}

func TestValidateConfig_DecodeErrorsDoNotEchoValues(t *testing.T) {
	// Raw json.Unmarshal errors can embed the submitted literal (e.g.
	// "cannot unmarshal number 99999999999 into ... field port") —
	// decode failures must be scrubbed the same way range failures
	// are, since both reach server logs via the request logger.
	c := Controller{}
	cases := []struct {
		name  string
		field string
		value string // raw JSON literal to splice in
	}{
		{"non-integer unit_id", "unit_id", `"7 "`},
		{"fractional register_address", "register_address", `2001.5`},
		{"overflowing port", "port", `99999999999999999999`},
		{"overflowing register_address", "register_address", `99999999999999999999`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(fmt.Sprintf(
				`{"endpoint":"10.20.30.40","port":502,"unit_id":1,"register_address":2001,"write_mode":%q,%q:%s}`,
				WriteModeHoldingRegister, tc.field, tc.value))
			err := c.ValidateConfig(raw)
			require.Error(t, err)
			stripped := strings.Trim(tc.value, `"`)
			assert.NotContains(t, err.Error(), stripped,
				"decode error must not echo the submitted value")
		})
	}
}

func TestCapabilities(t *testing.T) {
	assert.Equal(t, map[string]bool{"on_off": true}, Controller{}.Capabilities())
}

func TestSetState_WritesFC5AndFC6OnAndOffFrames(t *testing.T) {
	tests := []struct {
		name         string
		writeMode    string
		power        driver.PowerMode
		wantFunction byte
		wantValue    uint16
	}{
		{"coil off", WriteModeCoil, driver.PowerOff, 5, 0x0000},
		{"coil on", WriteModeCoil, driver.PowerOn, 5, 0xFF00},
		{"register off", WriteModeHoldingRegister, driver.PowerOff, 6, 0},
		{"register on", WriteModeHoldingRegister, driver.PowerOn, 6, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, simulator := startDevtoolModbusSimulator(t)
			controller := newTestController(t, listener)
			device := commissionedDevice(t, func(m map[string]any) {
				m["write_mode"] = tt.writeMode
			})

			err := controller.SetState(t.Context(), device, driver.DesiredState{Power: tt.power})
			require.NoError(t, err)

			switch tt.wantFunction {
			case 5:
				got, ok := simulator.Coil(1, 2000)
				require.True(t, ok, "simulator should record the FC5 write")
				assert.Equal(t, tt.wantValue == 0xFF00, got)
			case 6:
				got, ok := simulator.HoldingRegister(1, 2000)
				require.True(t, ok, "simulator should record the FC6 write")
				assert.Equal(t, tt.wantValue, got)
			default:
				t.Fatalf("unsupported test function code %d", tt.wantFunction)
			}
		})
	}
}

func TestSetState_PreservesOneBasedMappingForNonzeroApplicationAddresses(t *testing.T) {
	tests := []struct {
		applicationAddress int
		wantWireAddress    uint16
	}{
		{1, 0},
		{2001, 2000},
		{65536, 65535},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.applicationAddress), func(t *testing.T) {
			listener, simulator := startDevtoolModbusSimulator(t)
			controller := newTestController(t, listener)
			device := commissionedDevice(t, func(m map[string]any) {
				m["register_address"] = tt.applicationAddress
			})

			err := controller.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOn})
			require.NoError(t, err)

			writes := simulator.Writes()
			require.Len(t, writes, 1)
			assert.Equal(t, tt.wantWireAddress, writes[0].Address)
		})
	}
}

func TestSetState_RejectsInvalidPowerWithoutDialing(t *testing.T) {
	var dials atomic.Int32
	controller := NewConfigured(
		[]netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
		WithDialer(func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("unexpected dial")
		}),
	)

	err := controller.SetState(
		t.Context(),
		commissionedDevice(t, nil),
		driver.DesiredState{Power: driver.PowerMode(99)},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "power mode is invalid")
	assert.Zero(t, dials.Load())
}

func TestSetState_ParsesConfigAtCommandTime(t *testing.T) {
	controller := NewConfigured([]netip.Prefix{mustPrefix(t, "10.20.30.0/24")})
	device := commissionedDevice(t, nil)
	device.DriverConfig = json.RawMessage(`{"endpoint":"sensitive-ot-host"}`)

	err := controller.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOff})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration invalid")
	assert.NotContains(t, err.Error(), "sensitive-ot-host")
	assert.NotContains(t, err.Error(), string(device.DriverConfig))
}

func TestSetState_FailsClosedWithoutIdentityOrCommissioning(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*driver.Device)
		want   string
	}{
		{"organization", func(d *driver.Device) { d.OrgID = 0 }, "not commissioned"},
		{"site", func(d *driver.Device) { d.SiteID = 0 }, "not commissioned"},
		{"site allowlist", func(d *driver.Device) { d.InfrastructureControlSubnets = nil }, "not commissioned"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dials atomic.Int32
			controller := NewConfigured(
				[]netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
				WithDialer(func(context.Context, string, string) (net.Conn, error) {
					dials.Add(1)
					return nil, errors.New("unexpected dial")
				}),
			)
			device := commissionedDevice(t, nil)
			tt.mutate(&device)

			err := controller.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOff})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Zero(t, dials.Load())
		})
	}
}

func TestSetState_FailsClosedWithoutDeploymentAllowlist(t *testing.T) {
	controller := New()
	err := controller.SetState(
		t.Context(),
		commissionedDevice(t, nil),
		driver.DesiredState{Power: driver.PowerOff},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployment allowlist is missing")
}

func TestSetState_RequiresEndpointInBothAllowlists(t *testing.T) {
	tests := []struct {
		name   string
		global []netip.Prefix
		site   []netip.Prefix
	}{
		{
			name:   "site only",
			global: []netip.Prefix{mustPrefix(t, "10.99.0.0/24")},
			site:   []netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
		},
		{
			name:   "deployment only",
			global: []netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
			site:   []netip.Prefix{mustPrefix(t, "10.99.0.0/24")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dials atomic.Int32
			controller := NewConfigured(
				tt.global,
				WithDialer(func(context.Context, string, string) (net.Conn, error) {
					dials.Add(1)
					return nil, errors.New("unexpected dial")
				}),
			)
			device := commissionedDevice(t, nil)
			device.InfrastructureControlSubnets = tt.site

			err := controller.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOff})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "endpoint is not authorized")
			assert.NotContains(t, err.Error(), "10.20.30.40")
			assert.Zero(t, dials.Load())
		})
	}
}

func TestSetState_ReportsCanceledAndDeadlineContexts(t *testing.T) {
	tests := []struct {
		name       string
		context    func(t *testing.T) context.Context
		want       string
		maxElapsed time.Duration
	}{
		{
			name: "canceled",
			context: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			want:       "canceled",
			maxElapsed: time.Second,
		},
		{
			name: "earlier caller deadline",
			context: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			want:       "timed out",
			maxElapsed: 500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := NewConfigured(
				[]netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
				WithDialer(func(ctx context.Context, _, _ string) (net.Conn, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}),
			)
			started := time.Now()
			err := controller.SetState(
				tt.context(t),
				commissionedDevice(t, nil),
				driver.DesiredState{Power: driver.PowerOff},
			)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Less(t, time.Since(started), tt.maxElapsed)
		})
	}
}

func TestSetState_DefaultTimeoutBoundsStalledResponse(t *testing.T) {
	controller := NewConfigured(
		[]netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
		WithDialer(func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			t.Cleanup(func() { _ = server.Close() })
			go func() {
				request := make([]byte, 12)
				_, _ = io.ReadFull(server, request)
			}()
			return client, nil
		}),
	)

	started := time.Now()
	err := controller.SetState(
		t.Context(),
		commissionedDevice(t, nil),
		driver.DesiredState{Power: driver.PowerOn},
	)
	elapsed := time.Since(started)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.GreaterOrEqual(t, elapsed, defaultWriteTimeout-500*time.Millisecond)
	assert.Less(t, elapsed, defaultWriteTimeout+2*time.Second)
}

func TestSetState_ScrubsConnectionFailureDetails(t *testing.T) {
	const (
		endpoint = "10.88.77.66"
		port     = 15020
		unitID   = 247
		register = 54321
	)
	controller := NewConfigured(
		[]netip.Prefix{mustPrefix(t, "10.88.77.0/24")},
		WithDialer(func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf(
				"dial %s:%d unit %d register %d: connection refused",
				endpoint, port, unitID, register,
			)
		}),
	)
	device := commissionedDevice(t, func(m map[string]any) {
		m["endpoint"] = endpoint
		m["port"] = port
		m["unit_id"] = unitID
		m["register_address"] = register
	})
	device.InfrastructureControlSubnets = []netip.Prefix{mustPrefix(t, "10.88.77.0/24")}

	err := controller.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOn})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register write failed")
	for _, sensitive := range []string{
		endpoint,
		strconv.Itoa(port),
		strconv.Itoa(unitID),
		strconv.Itoa(register),
		string(device.DriverConfig),
	} {
		assert.NotContains(t, err.Error(), sensitive)
	}
}

func TestSetState_ScrubsWriteFailureDetails(t *testing.T) {
	const (
		endpoint = "10.88.77.65"
		port     = 15021
		unitID   = 246
		register = 54320
	)
	controller := NewConfigured(
		[]netip.Prefix{mustPrefix(t, "10.88.77.0/24")},
		WithDialer(func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			t.Cleanup(func() { _ = server.Close() })
			return writeFailConn{
				Conn: client,
				err: fmt.Errorf(
					"write %s:%d unit %d register %d failed",
					endpoint, port, unitID, register,
				),
			}, nil
		}),
	)
	device := commissionedDevice(t, func(m map[string]any) {
		m["endpoint"] = endpoint
		m["port"] = port
		m["unit_id"] = unitID
		m["register_address"] = register
		m["write_mode"] = WriteModeCoil
	})
	device.InfrastructureControlSubnets = []netip.Prefix{mustPrefix(t, "10.88.77.0/24")}

	err := controller.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOn})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coil write failed")
	for _, sensitive := range []string{
		endpoint,
		strconv.Itoa(port),
		strconv.Itoa(unitID),
		strconv.Itoa(register),
		string(device.DriverConfig),
	} {
		assert.NotContains(t, err.Error(), sensitive)
	}
}

func TestSetState_PerformsOneProtocolAttempt(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	var dials atomic.Int32
	controller := NewConfigured(
		[]netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
		WithDialer(func(ctx context.Context, network, _ string) (net.Conn, error) {
			dials.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
		}),
	)

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		_ = conn.Close()
	}()

	err = controller.SetState(
		t.Context(),
		commissionedDevice(t, nil),
		driver.DesiredState{Power: driver.PowerOn},
	)
	require.Error(t, err)
	assert.Equal(t, int32(1), dials.Load())
}

type writeFailConn struct {
	net.Conn
	err error
}

func (c writeFailConn) Write([]byte) (int, error) {
	return 0, c.err
}

func commissionedDevice(t *testing.T, mutate func(map[string]any)) driver.Device {
	t.Helper()
	return driver.Device{
		ID:                           9001,
		OrgID:                        101,
		SiteID:                       202,
		DriverType:                   DriverType,
		DriverConfig:                 validConfigJSON(t, mutate),
		InfrastructureControlSubnets: []netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
	}
}

func mustPrefix(t *testing.T, raw string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(raw)
	require.NoError(t, err)
	return prefix
}

func newTestController(t *testing.T, listener net.Listener) driver.Controller {
	t.Helper()
	return NewConfigured(
		[]netip.Prefix{mustPrefix(t, "10.20.30.0/24")},
		WithDialer(func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
		}),
	)
}

func startDevtoolModbusSimulator(t *testing.T) (net.Listener, *modbussim.Server) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	simulator := modbussim.New(
		modbussim.WithLogger(slog.New(slog.DiscardHandler)),
	)
	done := make(chan error, 1)
	go func() {
		done <- simulator.Serve(listener)
	}()
	t.Cleanup(func() {
		require.NoError(t, simulator.Close())
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("Modbus simulator did not stop")
		}
	})
	return listener, simulator
}
