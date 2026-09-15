package discovery

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
)

func ipListReq(ips, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpList{
		IpList: &pairingpb.IPListModeRequest{IpAddresses: ips, Ports: ports},
	}}
}

func networkScanReq(target string, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{
		NetworkScan: &pairingpb.NetworkScanModeRequest{Target: target, Ports: ports},
	}}
}

func ipRangeReq(start, end string, ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_IpRange{
		IpRange: &pairingpb.IPRangeModeRequest{StartIp: start, EndIp: end, Ports: ports},
	}}
}

func autoNetworkScanReq(ports []string) *pairingpb.DiscoverRequest {
	return &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{
		NetworkScan: &pairingpb.NetworkScanModeRequest{Target: netscan.LocalSubnetTarget, Ports: ports},
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
		{"ip range in scope", ipRangeReq("192.168.1.10", "192.168.1.20", []string{"80"}), "192.168.1.15", "80", true},
		{"ip range address out of scope", ipRangeReq("192.168.1.10", "192.168.1.20", []string{"80"}), "192.168.1.21", "80", false},
		{"ip range port out of scope", ipRangeReq("192.168.1.10", "192.168.1.20", []string{"80"}), "192.168.1.15", "22", false},
		{"network scan cidr in scope", networkScanReq("192.168.1.0/24", []string{"80"}), "192.168.1.55", "80", true},
		{"network scan cidr ip out of scope", networkScanReq("192.168.1.0/24", []string{"80"}), "192.168.2.55", "80", false},
		{"network scan cidr port out of scope", networkScanReq("192.168.1.0/24", []string{"80"}), "192.168.1.55", "22", false},
		{"network scan range in scope", networkScanReq("192.168.1.10-50", []string{"80"}), "192.168.1.30", "80", true},
		{"network scan range below start", networkScanReq("192.168.1.10-50", []string{"80"}), "192.168.1.5", "80", false},
		{"network scan range above end", networkScanReq("192.168.1.10-50", []string{"80"}), "192.168.1.51", "80", false},
		{"network scan literal in scope", networkScanReq("192.168.1.10", []string{"80"}), "192.168.1.10", "80", true},
		{"network scan literal mismatch", networkScanReq("192.168.1.10", []string{"80"}), "192.168.1.11", "80", false},
		{"network scan hostname leaves ip unconstrained", networkScanReq("miner.lan", []string{"80"}), "10.1.2.3", "80", true},
		{"network scan hostname still enforces ports", networkScanReq("miner.lan", []string{"80"}), "10.1.2.3", "22", false},
		{"auto accepts private ip on in-scope port", autoNetworkScanReq([]string{"80"}), "192.168.5.9", "80", true},
		{"auto rejects public ip", autoNetworkScanReq([]string{"80"}), "8.8.8.8", "80", false},
		{"auto rejects out-of-scope port", autoNetworkScanReq([]string{"80"}), "192.168.5.9", "22", false},
		{"auto empty ports allows any private ip and port", autoNetworkScanReq(nil), "10.2.3.4", "31337", true},
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
	err := ValidateRequest(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid IP address or hostname")
}

func TestNormalizeDiscoverRequest_AcceptsIPListHostname(t *testing.T) {
	// Arrange
	req := ipListReq([]string{"192.168.1.10", "miner.lan"}, []string{"80"})

	// Act
	err := ValidateRequest(req)

	// Assert
	require.NoError(t, err)
}

func TestValidateDiscoverRequest_TargetAndPortLimits(t *testing.T) {
	addresses := make([]string, 4096)
	for i := range addresses {
		addresses[i] = "10.0.0.1"
	}
	ports := []string{"80", "80", "80", "80", "80", "80", "80", "80", "80", "80"}
	for _, tc := range []struct {
		name string
		req  *pairingpb.DiscoverRequest
		ok   bool
	}{
		{"4096 raw targets", ipListReq(addresses, nil), true},
		{"4097 raw targets", ipListReq(slices.Concat(addresses, []string{"10.0.0.1"}), nil), false},
		{"/20 network", networkScanReq("10.0.0.0/20", nil), true},
		{"/19 network", networkScanReq("10.0.0.0/19", nil), false},
		{"ten raw ports", ipListReq([]string{"10.0.0.1"}, ports), true},
		{"eleven raw ports", ipListReq([]string{"10.0.0.1"}, append(ports, "80")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRequest(tc.req)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.True(t, fleeterror.IsInvalidArgumentError(err))
			}
		})
	}
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
			err := ValidateRequest(req)

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
	err := ValidateRequest(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestNormalizeDiscoverRequest_RejectsPublicNetworkScanTarget(t *testing.T) {
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
			req := networkScanReq(tc.target, []string{"80"})

			// Act
			err := ValidateRequest(req)

			// Assert
			require.Error(t, err)
			assert.Contains(t, err.Error(), "private")
		})
	}
}

func TestNormalizeDiscoverRequest_LocalSubnetTarget_Accepts(t *testing.T) {
	// Arrange: the local-subnet sentinel target with valid ports.
	req := autoNetworkScanReq([]string{"80", "4028"})

	// Act
	err := ValidateRequest(req)

	// Assert
	require.NoError(t, err)
}

func TestNormalizeDiscoverRequest_LocalSubnetTarget_RejectsInvalidPort(t *testing.T) {
	// Arrange
	req := autoNetworkScanReq([]string{"80/tcp"})

	// Act
	err := ValidateRequest(req)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid port")
}

func TestValidateRequest_IPRangeTargetLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		end     string
		wantErr bool
	}{
		{name: "4096 targets", end: "10.0.16.1"},
		{name: "4097 targets", end: "10.0.16.2", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			req := ipRangeReq("10.0.0.2", tc.end, []string{"4028"})

			// Act
			err := ValidateRequest(req)

			// Assert
			if tc.wantErr {
				require.ErrorContains(t, err, "ip range exceeds 4096 addresses")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, "10.0.0.2", req.GetIpRange().GetStartIp())
			assert.Equal(t, tc.end, req.GetIpRange().GetEndIp())
		})
	}
}

func TestValidateRequest_AutomaticLocalSubnet(t *testing.T) {
	// Arrange: Fleet Server's subnet is not the target sent to Fleet Nodes.
	req := networkScanReq("192.168.0.0/19", []string{"4028"})
	req.GetNetworkScan().UseFleetNodeLocalSubnet = true

	// Act
	err := ValidateRequest(req)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "192.168.0.0/19", req.GetNetworkScan().GetTarget())
}
