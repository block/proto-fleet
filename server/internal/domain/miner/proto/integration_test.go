package proto

import (
	"testing"
	"time"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/files"

	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getFilesService(t *testing.T) *files.Service {
	filesService, err := files.NewService() // the current tests don't use the DB conn, we should be therefore OK with passing nil here
	if err != nil {
		t.Fatalf("failed to create test files service: %v", err)
	}
	return filesService
}

func TestProtoMiner_GetTelemetry_Integration(t *testing.T) {
	// This is an integration test that would require a running miner
	// For now, we'll test that the method doesn't panic and returns proper error handling

	minerInfo, err := NewProtoMinerInfo("123", "localhost", 8080, networking.ProtocolHTTPS)
	require.NoError(t, err, "expected no error when creating miner info")
	filesService := getFilesService(t)
	miner, err := NewProtoMiner(minerInfo, *secrets.NewText("test-token"), filesService)
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
	filesService := getFilesService(t)

	// Test the new constructor
	minerInfo, err := NewProtoMinerInfo("123", "localhost", 8080, networking.ProtocolHTTPS)
	require.NoError(t, err, "expected no error when creating miner info")
	miner1, err := NewProtoMiner(minerInfo, *secrets.NewText("test-token"), filesService)
	assert.NotNil(t, miner1, "expected miner to be created")
	assert.NoError(t, err, "expected no error when creating miner")
}
