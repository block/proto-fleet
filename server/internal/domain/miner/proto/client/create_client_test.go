package client

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/generated/miner-api/miner_data_api/miner_data_apiconnect"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/secrets"
	"github.com/stretchr/testify/require"
)

func TestCreateClient(t *testing.T) {
	tests := []struct {
		name        string
		ctor        func(connect.HTTPClient, string, ...connect.ClientOption) miner_data_apiconnect.MinerDataApiClient
		httpClient  connect.HTTPClient
		ip          string
		port        string
		expectError bool
	}{
		{
			name:        "valid parameters",
			ctor:        miner_data_apiconnect.NewMinerDataApiClient,
			ip:          "localhost",
			port:        "8080",
			expectError: false,
		},
		{
			name:        "nil constructor",
			ctor:        nil,
			ip:          "localhost",
			port:        "8080",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connectionInfo, err := networking.NewConnectionInfo(tt.ip, tt.port, networking.ProtocolHTTPS)
			require.NoError(t, err)
			client, err := CreateClient(tt.ctor, *connectionInfo)

			if tt.expectError {
				require.Error(t, err, "expected an error but got none")
				return
			}

			require.NoError(t, err, "expected no error but got one")
			require.NotNil(t, client, "expected client to be created but got nil")
		})
	}
}

func TestContextWithAuth(t *testing.T) {
	t.Run("no panic", func(t *testing.T) {
		authCtx := ContextWithAuth(t.Context(), secrets.NewText("test-token"))
		require.NotNil(t, authCtx, "expected context with auth token to be created")
	})
}
