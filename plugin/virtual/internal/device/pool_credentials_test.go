package device

import (
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiningPoolsKeepIndependentStoredCredentials(t *testing.T) {
	device := newTestDevice(t)
	pools := []sdk.MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://primary.example:3333", WorkerName: "account.primary", Password: "primary-password"},
		{Priority: 1, URL: "stratum+tcp://backup.example:3333", WorkerName: "account.backup", Password: "backup-password"},
		{Priority: 2, URL: "stratum+tcp://last.example:3333", WorkerName: "account.last", Password: ""},
	}
	expected := append([]sdk.MiningPoolConfig(nil), pools...)
	require.NoError(t, device.UpdateMiningPools(t.Context(), pools))
	pools[0].URL = "stratum+tcp://mutated.example:3333"
	pools[0].WorkerName = "mutated-worker"
	pools[0].Password = "mutated-password"

	actual, err := device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	require.Len(t, actual, len(expected))
	for i, pool := range expected {
		assert.Equal(t, pool.Priority, actual[i].Priority)
		assert.Equal(t, pool.URL, actual[i].URL)
		assert.Equal(t, pool.WorkerName, actual[i].Username)
		require.NotNil(t, actual[i].Password, "stored empty password must be distinguished from missing readback")
		assert.Equal(t, pool.Password, *actual[i].Password)
	}
	actual[0].URL = "stratum+tcp://output-mutated.example:3333"
	*actual[0].Password = "output-mutated-password"
	again, err := device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected[0].URL, again[0].URL)
	require.NotNil(t, again[0].Password)
	assert.Equal(t, expected[0].Password, *again[0].Password)
	assert.NotSame(t, actual[0].Password, again[0].Password)

	replacement := []sdk.MiningPoolConfig{{Priority: 0, URL: "stratum+tcp://replacement.example:3333", WorkerName: "replacement", Password: ""}}
	require.NoError(t, device.UpdateMiningPools(t.Context(), replacement))
	replaced, err := device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	require.Len(t, replaced, 1, "removed backups must not remain")
	require.NotNil(t, replaced[0].Password)
	assert.Empty(t, *replaced[0].Password)
}

func TestClearAllPoolsRemovesPrimaryBackupsAndCredentials(t *testing.T) {
	device := newTestDevice(t)
	_, capabilities, err := device.DescribeDevice(t.Context())
	require.NoError(t, err)
	require.True(t, capabilities[sdk.CapabilityPoolClear])
	require.NoError(t, device.UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://primary.example", WorkerName: "worker", Password: "primary"}, {Priority: 1, URL: "stratum+tcp://backup.example", WorkerName: "worker", Password: "backup"}}))
	require.NoError(t, device.UpdateMiningPools(t.Context(), nil))
	stored, err := device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	require.Empty(t, stored)
	reported, err := device.GetMiningPools(t.Context())
	require.NoError(t, err)
	require.Empty(t, reported)
}
