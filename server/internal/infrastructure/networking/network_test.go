package networking

import (
	"net"
	"testing"
)

func TestNormalizeMAC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "colon separated",
			input:    "AA:BB:CC:DD:EE:FF",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "dash separated lowercase",
			input:    "aa-bb-cc-dd-ee-ff",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "dot separated",
			input:    "aabb.ccdd.eeff",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "bare uppercase",
			input:    "AABBCCDDEEFF",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "bare lowercase",
			input:    "aabbccddeeff",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "trim whitespace",
			input:    "  AABBCCDDEEFF  ",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "invalid length",
			input:    "AABBCCDDEEF",
			expected: "",
		},
		{
			name:     "invalid characters",
			input:    "AABBCCDDEEFG",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeMAC(tt.input); got != tt.expected {
				t.Fatalf("NormalizeMAC(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSubnetCIDR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ipNet    *net.IPNet
		expected string
	}{
		{
			name: "ipv4 host address is normalized to network cidr",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("192.168.2.17").To4(),
				Mask: net.CIDRMask(24, 32),
			},
			expected: "192.168.2.0/24",
		},
		{
			name: "existing network address stays unchanged",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("10.0.0.0").To4(),
				Mask: net.CIDRMask(16, 32),
			},
			expected: "10.0.0.0/16",
		},
		{
			name:     "nil input returns empty string",
			ipNet:    nil,
			expected: "",
		},
		{
			name: "ipv6 host address is normalized to network cidr",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("fd00::1"),
				Mask: net.CIDRMask(64, 128),
			},
			expected: "fd00::/64",
		},
		{
			name: "ipv6 /128 single host",
			ipNet: &net.IPNet{
				IP:   net.ParseIP("2001:db8::1"),
				Mask: net.CIDRMask(128, 128),
			},
			expected: "2001:db8::1/128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := subnetCIDR(tt.ipNet); got != tt.expected {
				t.Fatalf("subnetCIDR(%v) = %q, want %q", tt.ipNet, got, tt.expected)
			}
		})
	}
}
