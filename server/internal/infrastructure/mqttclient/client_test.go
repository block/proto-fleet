package mqttclient

import (
	"strings"
	"testing"
)

func TestBrokerOptions_TCP(t *testing.T) {
	t.Parallel()

	url, tlsConfig, err := brokerOptions("10.155.0.3", "10.155.0.3:1883", transportTCP)

	if err != nil {
		t.Fatalf("brokerOptions returned error: %v", err)
	}
	if url != "tcp://10.155.0.3:1883" {
		t.Fatalf("url = %q, want tcp URL", url)
	}
	if tlsConfig != nil {
		t.Fatal("tcp transport must not configure TLS")
	}
}

func TestBrokerOptions_TLS(t *testing.T) {
	t.Parallel()

	url, tlsConfig, err := brokerOptions("broker.example.com", "broker.example.com:8883", transportTLS)

	if err != nil {
		t.Fatalf("brokerOptions returned error: %v", err)
	}
	if url != "ssl://broker.example.com:8883" {
		t.Fatalf("url = %q, want ssl URL", url)
	}
	if tlsConfig == nil {
		t.Fatal("tls transport must configure TLS")
	}
	if tlsConfig.ServerName != "broker.example.com" {
		t.Fatalf("ServerName = %q, want broker host", tlsConfig.ServerName)
	}
}

func TestCopyPayloadRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	if _, ok := copyPayload([]byte(strings.Repeat("x", maxPayloadBytes+1))); ok {
		t.Fatal("oversized payload was accepted")
	}
}

func TestCopyPayloadCopiesAcceptedPayload(t *testing.T) {
	t.Parallel()

	in := []byte(`{"target":100,"timestamp":1778538975}`)
	got, ok := copyPayload(in)
	if !ok {
		t.Fatal("valid payload rejected")
	}
	got[0] = 'X'
	if in[0] == 'X' {
		t.Fatal("payload was not copied")
	}
}
