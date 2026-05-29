package admin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

func ipListReq(ips, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpList{
		IpList: &pairingpb.IPListModeRequest{IpAddresses: ips, Ports: ports},
	}}
}

func nmapReq(target string, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_Nmap{
		Nmap: &pairingpb.NmapModeRequest{Target: target, Ports: ports},
	}}
}

func TestBuildReportScope(t *testing.T) {
	tests := []struct {
		name    string
		req     *pairingpb.DiscoverRequest
		ip      string
		port    string
		inScope bool
	}{
		{"iplist in scope", ipListReq([]string{"192.168.1.10", "192.168.1.11"}, []string{"80", "4028"}), "192.168.1.10", "4028", true},
		{"iplist ip out of scope", ipListReq([]string{"192.168.1.10"}, []string{"80"}), "192.168.1.99", "80", false},
		{"iplist port out of scope", ipListReq([]string{"192.168.1.10"}, []string{"80"}), "192.168.1.10", "8080", false},
		{"iplist ipv6 case is canonicalized", ipListReq([]string{"FD00::1"}, []string{"80"}), "fd00::1", "80", true},
		{"iplist empty ports allows any port", ipListReq([]string{"192.168.1.10"}, nil), "192.168.1.10", "31337", true},
		{"iplist hostname leaves ip unconstrained", ipListReq([]string{"miner.lan"}, []string{"80"}), "10.0.0.5", "80", true},
		{"iplist hostname still enforces ports", ipListReq([]string{"miner.lan"}, []string{"80"}), "10.0.0.5", "22", false},
		{"iplist mixed hostname unconstrains all ips", ipListReq([]string{"192.168.1.10", "miner.lan"}, []string{"80"}), "10.0.0.5", "80", true},
		{"iplist ipv4-mapped reported ip is unmapped", ipListReq([]string{"192.168.1.10"}, []string{"80"}), "::ffff:192.168.1.10", "80", true},
		{"iplist ipv4-mapped requested entry is unmapped", ipListReq([]string{"::ffff:192.168.1.10"}, []string{"80"}), "192.168.1.10", "80", true},
		{"nmap cidr in scope", nmapReq("192.168.1.0/24", []string{"80"}), "192.168.1.55", "80", true},
		{"nmap cidr ip out of scope", nmapReq("192.168.1.0/24", []string{"80"}), "192.168.2.55", "80", false},
		{"nmap cidr port out of scope", nmapReq("192.168.1.0/24", []string{"80"}), "192.168.1.55", "22", false},
		{"nmap range in scope", nmapReq("192.168.1.10-50", []string{"80"}), "192.168.1.30", "80", true},
		{"nmap range below start", nmapReq("192.168.1.10-50", []string{"80"}), "192.168.1.5", "80", false},
		{"nmap range above end", nmapReq("192.168.1.10-50", []string{"80"}), "192.168.1.51", "80", false},
		{"nmap literal in scope", nmapReq("192.168.1.10", []string{"80"}), "192.168.1.10", "80", true},
		{"nmap literal mismatch", nmapReq("192.168.1.10", []string{"80"}), "192.168.1.11", "80", false},
		{"nmap hostname leaves ip unconstrained", nmapReq("miner.lan", []string{"80"}), "10.1.2.3", "80", true},
		{"nmap hostname still enforces ports", nmapReq("miner.lan", []string{"80"}), "10.1.2.3", "22", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			inScope := buildReportScope(tc.req)(tc.ip, tc.port)

			// Assert
			assert.Equal(t, tc.inScope, inScope)
		})
	}
}

func TestNormalizeDiscoverRequest_RejectsMalformedIPListEntry(t *testing.T) {
	// Arrange
	req := ipListReq([]string{"192.168.1.10", "bad/entry"}, []string{"80"})

	// Act
	_, err := normalizeDiscoverRequest(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid IP address or hostname")
}

func TestNormalizeDiscoverRequest_AcceptsIPListHostname(t *testing.T) {
	// Arrange
	req := ipListReq([]string{"192.168.1.10", "miner.lan"}, []string{"80"})

	// Act
	_, err := normalizeDiscoverRequest(req)

	// Assert
	require.NoError(t, err)
}

func TestNormalizeDiscoverRequest_RejectsInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port string
	}{
		{"protocol suffix", "80/tcp"},
		{"zero", "0"},
		{"above max", "70000"},
		{"non-numeric", "http"},
		{"empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			req := ipListReq([]string{"192.168.1.10"}, []string{tc.port})

			// Act
			_, err := normalizeDiscoverRequest(req)

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid port")
		})
	}
}

func TestNormalizeDiscoverRequest_RejectsPublicIPListEntry(t *testing.T) {
	// Arrange
	req := ipListReq([]string{"192.168.1.10", "8.8.8.8"}, []string{"80"})

	// Act
	_, err := normalizeDiscoverRequest(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestNormalizeDiscoverRequest_RejectsPublicNmapTarget(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{"public literal", "8.8.8.8"},
		{"public cidr", "8.8.8.0/24"},
		{"public range", "8.8.8.1-20"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			req := nmapReq(tc.target, []string{"80"})

			// Act
			_, err := normalizeDiscoverRequest(req)

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), "private")
		})
	}
}

func TestNmapTargetIsPrivate(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		private bool
	}{
		{"private literal", "192.168.1.5", true},
		{"public literal", "8.8.8.8", false},
		{"private cidr", "10.0.0.0/24", true},
		{"public cidr", "8.8.8.0/24", false},
		{"private range", "192.168.1.10-50", true},
		{"public range", "8.8.8.10-50", false},
		{"private ipv6 ula", "fd00::1", true},
		{"public ipv6", "2001:db8::1", false},
		{"hostname passes through", "miner.lan", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := nmapTargetIsPrivate(tc.target)

			// Assert
			assert.Equal(t, tc.private, got)
		})
	}
}
