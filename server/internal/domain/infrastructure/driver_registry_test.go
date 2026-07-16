package infrastructure_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/infrastructure"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver/modbustcp"
)

func TestNewDefaultDriverRegistryIsFailClosedForWrites(t *testing.T) {
	registry := infrastructure.NewDefaultDriverRegistry()
	controller, err := registry.Controller(modbustcp.DriverType)
	require.NoError(t, err)

	err = controller.SetState(t.Context(), registryTestDevice(t), driver.DesiredState{Power: driver.PowerOff})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployment allowlist is missing")
}

func TestNewConfiguredDriverRegistryAppliesDeploymentAllowlist(t *testing.T) {
	registry, err := infrastructure.NewConfiguredDriverRegistry(infrastructure.Config{
		OTControlSubnets: "10.99.0.0/24",
	})
	require.NoError(t, err)
	controller, err := registry.Controller(modbustcp.DriverType)
	require.NoError(t, err)

	err = controller.SetState(t.Context(), registryTestDevice(t), driver.DesiredState{Power: driver.PowerOff})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is not authorized")
	assert.NotContains(t, err.Error(), "10.20.30.40")
}

func TestNewConfiguredDriverRegistryAllowsEmptyFailClosedConfig(t *testing.T) {
	registry, err := infrastructure.NewConfiguredDriverRegistry(infrastructure.Config{})
	require.NoError(t, err)
	controller, err := registry.Controller(modbustcp.DriverType)
	require.NoError(t, err)

	err = controller.SetState(t.Context(), registryTestDevice(t), driver.DesiredState{Power: driver.PowerOff})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployment allowlist is missing")
}

func TestNewConfiguredDriverRegistryAcceptsCommaSeparatedEnvironmentValue(t *testing.T) {
	_, err := infrastructure.NewConfiguredDriverRegistry(infrastructure.Config{
		OTControlSubnets: "10.20.30.0/24,fd12:3456::8/128",
	})
	require.NoError(t, err)
}

func TestNewConfiguredDriverRegistryRejectsUnsafeDeploymentAllowlistsWithoutEcho(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		secrets   []string
	}{
		{
			name:      "public",
			allowlist: "203.0.113.44/32",
			secrets:   []string{"203.0.113.44"},
		},
		{
			name:      "IPv4 too broad",
			allowlist: "10.48.0.0/19",
			secrets:   []string{"10.48.0.0"},
		},
		{
			name:      "IPv6 not host only",
			allowlist: "fd12:3456::/64",
			secrets:   []string{"fd12:3456"},
		},
		{
			name:      "overlap",
			allowlist: "10.64.0.0/24\n10.64.0.42/32",
			secrets:   []string{"10.64.0.0", "10.64.0.42"},
		},
		{
			name:      "malformed",
			allowlist: "sensitive-control-subnet",
			secrets:   []string{"sensitive-control-subnet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := infrastructure.NewConfiguredDriverRegistry(infrastructure.Config{
				OTControlSubnets: tt.allowlist,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "deployment OT control-subnet allowlist")
			for _, secret := range tt.secrets {
				assert.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestNewConfiguredDriverRegistryCapsDeploymentAllowlist(t *testing.T) {
	entries := make([]string, 257)
	for i := range entries {
		entries[i] = netip.PrefixFrom(netip.AddrFrom4([4]byte{
			10,
			byte(i / 256),
			byte(i),
			byte(i),
		}), 32).String()
	}

	_, err := infrastructure.NewConfiguredDriverRegistry(infrastructure.Config{
		OTControlSubnets: strings.Join(entries, "\n"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployment OT control-subnet allowlist")
	assert.Contains(t, err.Error(), "too many entries")
}

func registryTestDevice(t *testing.T) driver.Device {
	t.Helper()
	return driver.Device{
		ID:         17,
		OrgID:      101,
		SiteID:     202,
		DriverType: modbustcp.DriverType,
		DriverConfig: []byte(`{
			"endpoint":"10.20.30.40",
			"port":502,
			"unit_id":1,
			"register_address":2001,
			"write_mode":"holding_register"
		}`),
		InfrastructureControlSubnets: []netip.Prefix{
			netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 20, 30, 40}), 32),
		},
	}
}
