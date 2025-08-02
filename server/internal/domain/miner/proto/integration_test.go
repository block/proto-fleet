package proto_test

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/proto"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtoMiner_GetTelemetry_Integration(t *testing.T) {
	// This is an integration test that would require a running miner
	// For now, we'll test that the method doesn't panic and returns proper error handling

	testContext := testutil.InitializeDBServiceInfrastructure(t)

	testConfig, err := testutil.GetTestConfig()
	require.NoError(t, err, "expected no error when getting test config")
	minerAuthPrivateKey := testConfig.GetMinerAuthPrivateKey(t)

	minerInfo, err := proto.NewProtoMinerInfo("123", "localhost", 8080, networking.ProtocolHTTPS, minerAuthPrivateKey, "test_serial_number")
	require.NoError(t, err, "expected no error when creating miner info")

	miner, err := proto.NewProtoMiner(minerInfo, testContext.ServiceProvider.FilesService, testContext.ServiceProvider.TokenService, testContext.ServiceProvider.EncryptService)
	require.NoError(t, err, "expected no error when creating miner")
	require.NotNil(t, miner, "expected miner to be created")

	ctx := t.Context()
	after := time.Now().Add(-1 * time.Hour)

	// This will fail because there's no actual miner running, but it should not panic
	telemetryData, err := miner.GetTelemetry(ctx, after)

	require.Error(t, err, "expected error when getting telemetry from non-existent miner")
	assert.Nil(t, telemetryData, "expected telemetry data to be nil when miner is not running")
}

func TestProtoMiner_NewConstructors(t *testing.T) {
	// Test the new constructor
	testContext := testutil.InitializeDBServiceInfrastructure(t)

	testConfig, err := testutil.GetTestConfig()
	require.NoError(t, err, "expected no error when getting test config")
	minerAuthPrivateKey := testConfig.GetMinerAuthPrivateKey(t)

	minerInfo, err := proto.NewProtoMinerInfo("123", "localhost", 8080, networking.ProtocolHTTPS, minerAuthPrivateKey, "test_serial_number")
	require.NoError(t, err, "expected no error when creating miner info")
	miner1, err := proto.NewProtoMiner(minerInfo, testContext.ServiceProvider.FilesService, testContext.ServiceProvider.TokenService, testContext.ServiceProvider.EncryptService)
	assert.NotNil(t, miner1, "expected miner to be created")
	assert.NoError(t, err, "expected no error when creating miner")
}
