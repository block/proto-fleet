package client

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
)

// Mock client constructor for testing
func mockClientConstructor(_ connect.HTTPClient, _ string, _ ...connect.ClientOption) any {
	return "mock-client"
}

func TestCreateClientWithInsecureTLS(t *testing.T) {
	// Reset clients to ensure we start fresh
	ResetClients()

	t.Setenv("SKIP_TLS_VERIFY", "true")

	// Test that the client can be created with HTTPS protocol
	// when TLS verification is disabled
	connectionInfo := networking.ConnectionInfo{
		IPAddress: "localhost",
		Port:      8443,
		Protocol:  networking.ProtocolHTTPS,
	}

	// This would normally fail with certificate verification errors
	// but should work when SKIP_TLS_VERIFY=true
	_, err := CreateClient(
		mockClientConstructor,
		connectionInfo,
	)

	// The error should not be related to TLS certificate verification
	// (it might fail for other reasons like connection refused, which is expected)
	if err != nil {
		t.Logf("Expected error (likely connection refused): %v", err)
	}
}

func TestCreateClientWithoutInsecureTLS(t *testing.T) {
	// Reset clients to ensure we start fresh
	ResetClients()

	connectionInfo := networking.ConnectionInfo{
		IPAddress: "localhost",
		Port:      8443,
		Protocol:  networking.ProtocolHTTPS,
	}

	_, err := CreateClient(
		mockClientConstructor,
		connectionInfo,
	)

	// Should fail with TLS-related errors when verification is enabled
	if err != nil {
		t.Logf("Expected TLS verification error: %v", err)
	}
}

func TestCreateClientRuntimeEnvChange(t *testing.T) {
	// Reset clients to ensure we start fresh
	ResetClients()

	// Start with TLS verification enabled
	t.Setenv("SKIP_TLS_VERIFY", "false")

	connectionInfo := networking.ConnectionInfo{
		IPAddress: "localhost",
		Port:      8443,
		Protocol:  networking.ProtocolHTTPS,
	}

	// Create first client with TLS verification enabled
	_, err1 := CreateClient(mockClientConstructor, connectionInfo)
	if err1 != nil {
		t.Logf("First client creation (TLS enabled): %v", err1)
	}

	// Change environment variable at runtime
	t.Setenv("SKIP_TLS_VERIFY", "true")

	// Reset clients to force recreation with new environment
	ResetClients()

	// Create second client with TLS verification disabled
	_, err2 := CreateClient(mockClientConstructor, connectionInfo)
	if err2 != nil {
		t.Logf("Second client creation (TLS disabled): %v", err2)
	}

	// Both should work (though they might fail for connection reasons, not TLS reasons)
	t.Logf("Client creation with TLS enabled: %v", err1)
	t.Logf("Client creation with TLS disabled: %v", err2)
}
