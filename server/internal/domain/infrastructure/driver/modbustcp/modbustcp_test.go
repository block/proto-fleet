package modbustcp

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		"::ffff:8.8.8.8",  // IPv4-mapped IPv6 public (netip unmaps before checks)
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

func TestValidateConfig_RejectsMalformedBlob(t *testing.T) {
	c := Controller{}
	assert.Error(t, c.ValidateConfig(nil))
	assert.Error(t, c.ValidateConfig(json.RawMessage(`not json`)))
}

func TestCapabilities(t *testing.T) {
	assert.Equal(t, map[string]bool{"on_off": true}, Controller{}.Capabilities())
}

func TestSetState_NotImplementedYet(t *testing.T) {
	// Protocol I/O is deliberately out of scope for the backend
	// phase; the write path lands with reconciler sequencing.
	device := driver.Device{ID: 1, Name: "Zone A exhaust", DriverType: DriverType}
	err := Controller{}.SetState(t.Context(), device, driver.DesiredState{Power: driver.PowerOff})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}
