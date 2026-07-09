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
	// Loopback and link-local endpoints are allowed.
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["endpoint"] = "127.0.0.1"
	})))
	assert.NoError(t, c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
		m["endpoint"] = "169.254.10.20"
	})))
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
	} {
		err := c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
			m["endpoint"] = endpoint
		}))
		assert.Error(t, err, "endpoint %q should be rejected", endpoint)
	}
}

func TestValidateConfig_RejectsOutOfRangeFields(t *testing.T) {
	c := Controller{}
	cases := []struct {
		field string
		value any
	}{
		{"unit_id", 0},
		{"unit_id", 248},
		{"port", 0},
		{"port", 65536},
		{"register_address", -1},
		{"register_address", 65536},
		{"write_mode", "toggle"},
		{"write_mode", ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s=%v", tc.field, tc.value), func(t *testing.T) {
			err := c.ValidateConfig(validConfigJSON(t, func(m map[string]any) {
				m[tc.field] = tc.value
			}))
			assert.Error(t, err)
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
