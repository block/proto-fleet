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

func TestLatencyConfig_SampleZeroValueHasNoLatency(t *testing.T) {
	var latency LatencyConfig

	assert.Equal(t, time.Duration(0), latency.Sample(nil))
}

func writeConfig(t *testing.T, cfg Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}
