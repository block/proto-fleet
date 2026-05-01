package miner

import (
	"context"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
)

type capabilityPluginManager struct {
	baseCaps sdk.Capabilities
	driver   sdk.Driver
}

func (m *capabilityPluginManager) HasPluginForDriverName(string) bool {
	return true
}

func (m *capabilityPluginManager) GetCapabilitiesForDriverName(string) sdk.Capabilities {
	return m.baseCaps
}

func (m *capabilityPluginManager) GetDriverByDriverName(string) (sdk.Driver, error) {
	return m.driver, nil
}

type modelCapabilityDriver struct {
	sdk.Driver
	modelCaps        sdk.Capabilities
	seenManufacturer string
	seenModel        string
	callCount        int
}

func (d *modelCapabilityDriver) GetCapabilitiesForModel(_ context.Context, manufacturer, model string) sdk.Capabilities {
	d.seenManufacturer = manufacturer
	d.seenModel = model
	d.callCount++
	return d.modelCaps
}

func TestEffectiveCapabilitiesForDeviceUsesDriverCapsWithModelOverrides(t *testing.T) {
	baseCaps := sdk.Capabilities{
		sdk.CapabilityCurtailFull:    true,
		sdk.CapabilityAsymmetricAuth: true,
	}
	driver := &modelCapabilityDriver{
		modelCaps: sdk.Capabilities{
			sdk.CapabilityCurtailFull: false,
		},
	}
	service := &Service{
		pluginManager: &capabilityPluginManager{
			baseCaps: baseCaps,
			driver:   driver,
		},
	}

	caps := service.effectiveCapabilitiesForDevice(t.Context(), "antminer", "Bitmain", "Antminer S21")

	assert.Equal(t, "Bitmain", driver.seenManufacturer)
	assert.Equal(t, "Antminer S21", driver.seenModel)
	assert.Equal(t, 1, driver.callCount)
	assert.False(t, caps[sdk.CapabilityCurtailFull])
	assert.True(t, caps[sdk.CapabilityAsymmetricAuth])
	_, hasCurtailEfficiency := caps[sdk.CapabilityCurtailEfficiency]
	assert.False(t, hasCurtailEfficiency)
	assert.True(t, baseCaps[sdk.CapabilityCurtailFull], "base capability map should not be mutated")
}

func TestEffectiveCapabilitiesForDeviceSkipsModelProviderWhenModelUnknown(t *testing.T) {
	baseCaps := sdk.Capabilities{
		sdk.CapabilityCurtailFull:    true,
		sdk.CapabilityAsymmetricAuth: true,
	}
	driver := &modelCapabilityDriver{
		modelCaps: sdk.Capabilities{
			sdk.CapabilityCurtailFull: false,
		},
	}
	service := &Service{
		pluginManager: &capabilityPluginManager{
			baseCaps: baseCaps,
			driver:   driver,
		},
	}

	caps := service.effectiveCapabilitiesForDevice(t.Context(), "antminer", "Bitmain", "")

	assert.Equal(t, 0, driver.callCount)
	assert.True(t, caps[sdk.CapabilityCurtailFull])
	assert.True(t, caps[sdk.CapabilityAsymmetricAuth])
}
