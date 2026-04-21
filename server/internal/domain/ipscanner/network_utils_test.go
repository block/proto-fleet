package ipscanner

import (
	"net"
	"testing"
)

func TestParseCIDR(t *testing.T) {
	tests := []struct {
		name    string
		cidr    string
		wantErr bool
	}{
		{
			name:    "valid IPv4 /24",
			cidr:    "192.168.1.0/24",
			wantErr: false,
		},
		{
			name:    "valid IPv4 /16",
			cidr:    "10.0.0.0/16",
			wantErr: false,
		},
		{
			name:    "valid IPv4 /8",
			cidr:    "172.16.0.0/8",
			wantErr: false,
		},
		{
			name:    "invalid CIDR",
			cidr:    "192.168.1.0",
			wantErr: true,
		},
		{
			name:    "invalid IP",
			cidr:    "999.999.999.999/24",
			wantErr: true,
		},
		{
			name:    "empty string",
			cidr:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCIDR(tt.cidr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCIDR() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateIPsFromCIDR(t *testing.T) {
	tests := []struct {
		name             string
		cidr             string
		wantCount        int
		wantErr          bool
		shouldContain    []string // IPs that should be in the result
		shouldNotContain []string // IPs that should NOT be in the result
	}{
		{
			name:          "small subnet /30",
			cidr:          "192.168.1.0/30",
			wantCount:     1, // Only .2 (excluding .0 network, .1 gateway, and .3 broadcast)
			wantErr:       false,
			shouldContain: []string{"192.168.1.2"},
		},
		{
			name:          "typical /24 subnet",
			cidr:          "10.0.0.0/24",
			wantCount:     253, // 256 - 3 (network, gateway, and broadcast)
			wantErr:       false,
			shouldContain: []string{"10.0.0.2", "10.0.0.100", "10.0.0.254"},
		},
		{
			name:    "invalid CIDR",
			cidr:    "invalid",
			wantErr: true,
		},
		{
			name:             "/24 excludes network and gateway",
			cidr:             "192.168.1.0/24",
			wantCount:        253,
			shouldNotContain: []string{"192.168.1.0", "192.168.1.1"},
			shouldContain:    []string{"192.168.1.2", "192.168.1.3"},
		},
		{
			name:             "/16 excludes network and gateway",
			cidr:             "172.16.0.0/16",
			wantCount:        65533,
			shouldNotContain: []string{"172.16.0.0", "172.16.0.1"},
			shouldContain:    []string{"172.16.0.2", "172.16.0.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips, err := generateIPsFromCIDR(tt.cidr)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateIPsFromCIDR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(ips) != tt.wantCount {
				t.Errorf("generateIPsFromCIDR() got %d IPs, want %d", len(ips), tt.wantCount)
			}

			// Create a map for quick lookup
			ipMap := make(map[string]bool)
			for _, ip := range ips {
				ipMap[ip] = true
			}

			// Check IPs that should be in the result
			for _, checkIP := range tt.shouldContain {
				if !ipMap[checkIP] {
					t.Errorf("generateIPsFromCIDR() missing expected IP %s", checkIP)
				}
			}

			// Check IPs that should NOT be in the result
			for _, excludedIP := range tt.shouldNotContain {
				if ipMap[excludedIP] {
					t.Errorf("generateIPsFromCIDR() should not contain %s (network or gateway address)", excludedIP)
				}
			}
		})
	}
}

func TestIPToSubnet(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		maskBits int
		want     string
		wantErr  bool
	}{
		{
			name:     "standard IPv4 /24",
			ip:       "192.168.1.100",
			maskBits: 24,
			want:     "192.168.1.0/24",
			wantErr:  false,
		},
		{
			name:     "first IP in subnet /24",
			ip:       "10.0.0.1",
			maskBits: 24,
			want:     "10.0.0.0/24",
			wantErr:  false,
		},
		{
			name:     "last IP in subnet /24",
			ip:       "172.16.5.254",
			maskBits: 24,
			want:     "172.16.5.0/24",
			wantErr:  false,
		},
		{
			name:     "IPv4 /16 subnet",
			ip:       "10.20.30.40",
			maskBits: 16,
			want:     "10.20.0.0/16",
			wantErr:  false,
		},
		{
			name:     "IPv4 /8 subnet",
			ip:       "172.16.5.10",
			maskBits: 8,
			want:     "172.0.0.0/8",
			wantErr:  false,
		},
		{
			name:    "invalid IP",
			ip:      "invalid",
			wantErr: true,
		},
		{
			name:    "empty string",
			ip:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ipToSubnet(tt.ip, tt.maskBits)
			if (err != nil) != tt.wantErr {
				t.Errorf("ipToSubnet() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("ipToSubnet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetSubnetFromIPAndMask(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		maskBits int
		want     string
		wantErr  bool
	}{
		{
			name:     "/24 subnet",
			ip:       "192.168.1.100",
			maskBits: 24,
			want:     "192.168.1.0/24",
			wantErr:  false,
		},
		{
			name:     "/16 subnet",
			ip:       "10.20.30.40",
			maskBits: 16,
			want:     "10.20.0.0/16",
			wantErr:  false,
		},
		{
			name:     "/8 subnet",
			ip:       "172.16.5.10",
			maskBits: 8,
			want:     "172.0.0.0/8",
			wantErr:  false,
		},
		{
			name:     "/32 single host",
			ip:       "192.168.1.1",
			maskBits: 32,
			want:     "192.168.1.1/32",
			wantErr:  false,
		},
		{
			name:     "invalid IP",
			ip:       "invalid",
			maskBits: 24,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getSubnetFromIPAndMask(tt.ip, tt.maskBits)
			if (err != nil) != tt.wantErr {
				t.Errorf("getSubnetFromIPAndMask() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("getSubnetFromIPAndMask() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIncrementIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{
			name: "IPv4 simple increment",
			ip:   "192.168.1.1",
			want: "192.168.1.2",
		},
		{
			name: "IPv4 with carry-over",
			ip:   "192.168.1.255",
			want: "192.168.2.0",
		},
		{
			name: "IPv4 multiple carry-over",
			ip:   "192.168.255.255",
			want: "192.169.0.0",
		},
		{
			name: "IPv6 simple increment",
			ip:   "2001:db8::1",
			want: "2001:db8::2",
		},
		{
			name: "IPv6 with carry-over",
			ip:   "2001:db8::ffff",
			want: "2001:db8::1:0",
		},
		{
			name: "IPv6 multiple carry-over",
			ip:   "2001:db8::ffff:ffff",
			want: "2001:db8::1:0:0",
		},
		{
			name: "IPv6 all hex digits",
			ip:   "2001:db8:0:0:0:0:0:ff",
			want: "2001:db8::100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test IP: %s", tt.ip)
			}

			got := incrementIP(ip)
			if got.String() != tt.want {
				t.Errorf("incrementIP(%s) = %s, want %s", tt.ip, got.String(), tt.want)
			}
		})
	}
}

func TestIPToSubnet_IPv6(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		maskBits int
		want     string
	}{
		{
			name:     "IPv6 /64 from host address",
			ip:       "fd00::1",
			maskBits: 64,
			want:     "fd00::/64",
		},
		{
			name:     "IPv6 /64 default when maskBits is 0",
			ip:       "fd00::1",
			maskBits: 0,
			want:     "fd00::/64",
		},
		{
			name:     "IPv6 /128 single host",
			ip:       "2001:db8::1",
			maskBits: 128,
			want:     "2001:db8::1/128",
		},
		{
			name:     "IPv6 /48 subnet",
			ip:       "2001:db8:1::100",
			maskBits: 48,
			want:     "2001:db8:1::/48",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ipToSubnet(tt.ip, tt.maskBits)
			if err != nil {
				t.Fatalf("ipToSubnet(%s, %d) returned unexpected error: %v", tt.ip, tt.maskBits, err)
			}
			if got != tt.want {
				t.Errorf("ipToSubnet(%s, %d) = %s, want %s", tt.ip, tt.maskBits, got, tt.want)
			}
		})
	}
}

func TestGetSubnetFromIPAndMask_IPv6(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		maskBits int
		want     string
	}{
		{
			name:     "IPv6 /64",
			ip:       "fd00::abcd",
			maskBits: 64,
			want:     "fd00::/64",
		},
		{
			name:     "IPv6 /120",
			ip:       "fd00::1:ff",
			maskBits: 120,
			want:     "fd00::1:0/120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getSubnetFromIPAndMask(tt.ip, tt.maskBits)
			if err != nil {
				t.Fatalf("getSubnetFromIPAndMask(%s, %d) returned unexpected error: %v", tt.ip, tt.maskBits, err)
			}
			if got != tt.want {
				t.Errorf("getSubnetFromIPAndMask(%s, %d) = %s, want %s", tt.ip, tt.maskBits, got, tt.want)
			}
		})
	}
}
