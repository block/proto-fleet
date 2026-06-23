package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile_EnvironmentOverridesGeneratedMiners(t *testing.T) {
	t.Setenv(envMinerCount, "3")
	t.Setenv(envMinerSerialPrefix, "LOAD")
	t.Setenv(envMinerIPStart, "10.255.4.10")
	t.Setenv(envBaselineVariancePercent, "12")

	path := writeConfig(t, Config{
		Generate: &GenerateConfig{Count: 1, SerialPrefix: "VM", IPStart: "10.255.0.2"},
	})

	cfg, err := LoadFromFile(path)

	require.NoError(t, err)
	require.Len(t, cfg.Miners, 3)
	assert.Equal(t, "LOAD0001", cfg.Miners[0].SerialNumber)
	assert.Equal(t, "10.255.4.10", cfg.Miners[0].IPAddress)
	assert.Equal(t, "10.255.4.12", cfg.Miners[2].IPAddress)
}

func TestLoadFromFile_DefaultLatencyProfile(t *testing.T) {
	path := writeConfig(t, Config{
		Miners: []VirtualMinerConfig{{
			SerialNumber: "VM001",
			IPAddress:    "10.255.0.2",
		}},
	})

	cfg, err := LoadFromFile(path)

	require.NoError(t, err)
	require.Len(t, cfg.Miners, 1)
	miner := cfg.Miners[0]
	assert.Equal(t, 5, miner.Behavior.NetworkLatency.MinMS)
	assert.Equal(t, 50, miner.Behavior.NetworkLatency.MaxMS)
	assert.Equal(t, 200, miner.Behavior.InternalLatency.MinMS)
	assert.Equal(t, 500, miner.Behavior.InternalLatency.MaxMS)
	assert.Equal(t, 5000, miner.Behavior.InternalLatency.OutlierMinMS)
	assert.Equal(t, 8000, miner.Behavior.InternalLatency.OutlierMaxMS)
}

func TestLoadFromFile_ExplicitZeroBaselineVariancePreserved(t *testing.T) {
	path := writeConfig(t, Config{
		Generate: &GenerateConfig{
			Count:                   1,
			IPStart:                 "10.255.0.2",
			BaselineVariancePercent: float64Ptr(0),
			Profiles: []MinerProfile{{
				Weight:              1,
				Model:               "Exact",
				Manufacturer:        "Virtual",
				Hashboards:          3,
				ASICsPerBoard:       76,
				FanCount:            4,
				BaselineHashrateTHS: 125,
				BaselinePowerW:      3100,
				BaselineTempC:       67,
				FanRPMMin:           3000,
				FanRPMMax:           5000,
			}},
		},
	})

	cfg, err := LoadFromFile(path)

	require.NoError(t, err)
	require.Len(t, cfg.Miners, 1)
	assert.Equal(t, 125.0, cfg.Miners[0].BaselineHashrateTHS)
	assert.Equal(t, 3100.0, cfg.Miners[0].BaselinePowerW)
	assert.Equal(t, 67.0, cfg.Miners[0].BaselineTempC)
}

func TestLoadFromFile_RejectsGeneratedMinerCountAboveOperationalLimit(t *testing.T) {
	t.Setenv(envMinerCount, "50001")

	path := writeConfig(t, Config{
		Generate: &GenerateConfig{Count: 1, IPStart: "10.255.0.2"},
	})

	_, err := LoadFromFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "count must be <= 50000")
}

func TestLoadFromFile_RejectsGeneratedMinerCountBeyondVirtualIPRange(t *testing.T) {
	path := writeConfig(t, Config{
		Generate: &GenerateConfig{Count: 2, IPStart: "10.255.255.254"},
	})

	_, err := LoadFromFile(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "count 2 exceeds available virtual IP addresses")
	assert.Contains(t, err.Error(), "1 available")
}

func TestLatencyConfig_SampleZeroValueHasNoLatency(t *testing.T) {
	var latency LatencyConfig

	assert.Equal(t, time.Duration(0), latency.Sample(nil))
}

func float64Ptr(value float64) *float64 {
	return &value
}

func writeConfig(t *testing.T, cfg Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}
