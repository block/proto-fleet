package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
)

func TestValidateConfig(t *testing.T) {
	controller := Controller{}

	for _, raw := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(" \n\t{} "),
	} {
		assert.NoError(t, controller.ValidateConfig(raw))
	}

	for _, raw := range []json.RawMessage{
		nil,
		json.RawMessage(``),
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`"object"`),
		json.RawMessage(`1`),
		json.RawMessage(`{`),
		json.RawMessage(`{"unknown":true}`),
		json.RawMessage(`{} {}`),
	} {
		assert.Error(t, controller.ValidateConfig(raw), "config %q should be rejected", raw)
	}
}

func TestSetStateLogsOnAndOffWithoutTopology(t *testing.T) {
	tests := []struct {
		name  string
		power driver.PowerMode
		want  string
	}{
		{name: "off", power: driver.PowerOff, want: "off"},
		{name: "on", power: driver.PowerOn, want: "on"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			controller := Controller{
				logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			}
			device := validDevice()

			err := controller.SetState(
				context.Background(),
				device,
				driver.DesiredState{Power: tt.power},
			)
			require.NoError(t, err)

			var entry map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &entry))
			assert.Equal(t, "sim infrastructure state set", entry["msg"])
			assert.Equal(t, float64(device.ID), entry["device_id"])
			assert.Equal(t, tt.want, entry["power"])
			for _, forbidden := range []string{
				"driver_config",
				"driver_type",
				"name",
				"org_id",
				"site_id",
			} {
				assert.NotContains(t, entry, forbidden)
			}
		})
	}
}

func TestSetStateRejectsInvalidDevice(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*driver.Device)
	}{
		{
			name: "missing ID",
			mutate: func(device *driver.Device) {
				device.ID = 0
			},
		},
		{
			name: "wrong driver type",
			mutate: func(device *driver.Device) {
				device.DriverType = "modbus_tcp"
			},
		},
		{
			name: "invalid config",
			mutate: func(device *driver.Device) {
				device.DriverConfig = json.RawMessage(`{"unknown":true}`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := validDevice()
			tt.mutate(&device)

			err := (Controller{}).SetState(
				context.Background(),
				device,
				driver.DesiredState{Power: driver.PowerOn},
			)
			require.Error(t, err)
		})
	}
}

func TestSetStateRejectsInvalidPower(t *testing.T) {
	err := (Controller{}).SetState(
		context.Background(),
		validDevice(),
		driver.DesiredState{Power: driver.PowerMode(99)},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "power mode")
}

func TestCapabilities(t *testing.T) {
	assert.Equal(t, map[string]bool{"on_off": true}, (Controller{}).Capabilities())
}

func TestRegistryResolvesExplicitTestRegistration(t *testing.T) {
	registry := driver.NewRegistry()
	registry.Register(DriverType, New)

	controller, err := registry.Controller(DriverType)
	require.NoError(t, err)
	assert.IsType(t, &Controller{}, controller)
	assert.Equal(t, []string{DriverType}, registry.DriverTypes())
}

func validDevice() driver.Device {
	return driver.Device{
		ID:           42,
		OrgID:        7,
		SiteID:       11,
		DriverType:   DriverType,
		DriverConfig: json.RawMessage(`{}`),
	}
}
