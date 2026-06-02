package fleetmanagement

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConstructWebViewURL pins the snapshot.Url semantics: only http/https are
// emitted as clickable links (other/untrusted schemes yield ""), host bracketed
// for IPv6, and port intentionally omitted so browsers fall back to the default
// for the scheme. This is the contract for both paired and unpaired device rows
// in ListMinerStateSnapshots.
func TestConstructWebViewURL(t *testing.T) {
	tests := []struct {
		name      string
		scheme    string
		ipAddress string
		expected  string
	}{
		{
			name:      "HTTP IPv4 address yields scheme and host with no port",
			scheme:    "http",
			ipAddress: "192.168.1.100",
			expected:  "http://192.168.1.100",
		},
		{
			name:      "HTTPS scheme is preserved",
			scheme:    "https",
			ipAddress: "192.168.1.100",
			expected:  "https://192.168.1.100",
		},
		{
			name:      "Bare IPv6 address is bracket-wrapped",
			scheme:    "http",
			ipAddress: "fe80::1",
			expected:  "http://[fe80::1]",
		},
		{
			name:      "Already-bracketed IPv6 address is not double-bracketed",
			scheme:    "http",
			ipAddress: "[::1]",
			expected:  "http://[::1]",
		},
		{
			name:      "Empty ipAddress yields empty URL",
			scheme:    "http",
			ipAddress: "",
			expected:  "",
		},
		{
			name:      "Empty scheme yields empty URL",
			scheme:    "",
			ipAddress: "192.168.1.100",
			expected:  "",
		},
		{
			name:      "Non-web scheme yields empty URL",
			scheme:    "stratum+tcp",
			ipAddress: "192.168.1.100",
			expected:  "",
		},
		{
			name:      "javascript scheme is never made clickable",
			scheme:    "javascript:alert(1)//",
			ipAddress: "192.168.1.100",
			expected:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			actual := constructWebViewURL(tc.scheme, tc.ipAddress)

			// Assert
			assert.Equal(t, tc.expected, actual)
		})
	}
}
