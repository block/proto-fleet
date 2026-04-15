package networking

import (
	"bytes"
	"math"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const externalIPForGatewayDetection = "8.8.8.8"

type NetworkInfo struct {
	Interface  string
	LocalIP    string
	Gateway    string
	Subnet     string
	LocalIPv6  string
	IPv6Subnet string
}

var emptyNetworkInfo = NetworkInfo{}

func GetLocalNetworkInfo() (NetworkInfo, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return emptyNetworkInfo, fleeterror.NewInternalErrorf("failed to get network interfaces: %v", err)
	}

	// IPv4 gateway discovery is best-effort; IPv6-only hosts won't have one.
	gatewayIP, gatewayErr := discoverGateway()

	type ifaceAddrs struct {
		name    string
		ipv4Net *net.IPNet
		ipv6Net *net.IPNet
	}

	// Collect addresses from all usable interfaces, then pick the best one.
	var candidates []ifaceAddrs

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 ||
			iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		var candidate ifaceAddrs
		candidate.name = iface.Name

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			if ipNet.IP.To4() != nil {
				if candidate.ipv4Net == nil {
					candidate.ipv4Net = ipNet
				}
			} else if ipNet.IP.To16() != nil && !ipNet.IP.IsLinkLocalUnicast() {
				// Accept global-scope IPv6 addresses, skip link-local (fe80::)
				if candidate.ipv6Net == nil {
					candidate.ipv6Net = ipNet
				}
			}
		}

		if candidate.ipv4Net != nil || candidate.ipv6Net != nil {
			candidates = append(candidates, candidate)
		}
	}

	// Prefer an interface that has IPv4 (needed for auto-discovery subnet
	// expansion). Fall back to IPv6-only if no IPv4 interface exists.
	var best *ifaceAddrs
	for i := range candidates {
		if candidates[i].ipv4Net != nil {
			best = &candidates[i]
			break
		}
	}
	if best == nil && len(candidates) > 0 {
		best = &candidates[0]
	}
	if best == nil {
		return emptyNetworkInfo, fleeterror.NewInternalError("no suitable network interface found")
	}

	info := NetworkInfo{
		Interface: best.name,
	}

	if best.ipv4Net != nil {
		info.LocalIP = best.ipv4Net.IP.String()
		info.Subnet = subnetCIDR(best.ipv4Net)
		if gatewayErr == nil {
			info.Gateway = gatewayIP.String()
		}
	}

	if best.ipv6Net != nil {
		info.LocalIPv6 = best.ipv6Net.IP.String()
		info.IPv6Subnet = subnetCIDR(best.ipv6Net)
	}

	return info, nil
}

func subnetCIDR(ipNet *net.IPNet) string {
	if ipNet == nil {
		return ""
	}

	return (&net.IPNet{
		IP:   ipNet.IP.Mask(ipNet.Mask),
		Mask: ipNet.Mask,
	}).String()
}

// NormalizeMAC normalizes a MAC address to uppercase colon-separated format
// (e.g., AA:BB:CC:DD:EE:FF) matching the format stored in the database.
// Uses net.ParseMAC for standard input formats and also accepts bare 12-hex MACs.
// Returns "" for empty or unparseable input.
func NormalizeMAC(mac string) string {
	mac = strings.TrimSpace(mac)
	if mac == "" {
		return ""
	}

	hw, err := net.ParseMAC(mac)
	if err == nil {
		return strings.ToUpper(hw.String())
	}

	if len(mac) != 12 {
		return ""
	}

	var b strings.Builder
	b.Grow(17)
	for i, c := range mac {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
			return ""
		}
		if i > 0 && i%2 == 0 {
			b.WriteByte(':')
		}
		if c >= 'a' && c <= 'f' {
			c -= 'a' - 'A'
		}
		b.WriteRune(c)
	}

	return b.String()
}

// discoverGateway asks the kernel for the gateway it would use to reach 8.8.8.8.
// It parses the “via” field out of `ip route get` output and returns it as net.IP.
func discoverGateway() (net.IP, error) {
	// Run the ip command
	cmd := exec.Command("ip", "route", "get", externalIPForGatewayDetection)
	out, err := cmd.Output()
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to execute ip route command: %v", err)
	}

	// Example output:
	// 8.8.8.8 via 192.168.1.1 dev eth0 src 192.168.1.42 uid 0
	fields := strings.Fields(string(bytes.TrimSpace(out)))
	for i := range len(fields) - 1 {
		if fields[i] == "via" {
			gatewayStr := fields[i+1]
			gatewayIPStr := net.ParseIP(gatewayStr)
			if gatewayIPStr == nil {
				return nil, fleeterror.NewInternalErrorf("parsed invalid gateway IP %q", gatewayStr)
			}
			return gatewayIPStr, nil
		}
	}

	return nil, fleeterror.NewInternalErrorf("no gateway found in route output: %q", out)
}

type IPAddress string

func (ip IPAddress) String() string {
	return string(ip)
}

type Port uint16

func (p Port) String() string {
	return strconv.Itoa(int(p))
}

type Protocol int

// Protocol constants for network communication with miners
const (
	// ProtocolHTTP is used for unencrypted web communication with miners
	ProtocolHTTP Protocol = iota
	// ProtocolHTTPS is used for secure encrypted web communication with miners
	ProtocolHTTPS
	// ProtocolTCP is used for direct socket connections with miners
	ProtocolTCP
	// ProtocolVirtual is used for virtual/simulated miners that don't require network communication
	ProtocolVirtual
)

func (p Protocol) String() string {
	switch p {
	case ProtocolHTTP:
		return "http"
	case ProtocolHTTPS:
		return "https"
	case ProtocolTCP:
		return "tcp"
	case ProtocolVirtual:
		return "virtual"
	default:
		return "unknown"
	}
}

func ProtocolFromString(s string) (Protocol, error) {
	switch s {
	case "http":
		return ProtocolHTTP, nil
	case "https":
		return ProtocolHTTPS, nil
	case "tcp":
		return ProtocolTCP, nil
	case "virtual":
		return ProtocolVirtual, nil
	default:
		return Protocol(-1), fleeterror.NewInvalidArgumentErrorf("unsupported protocol: %s", s)
	}
}

type ConnectionInfo struct {
	IPAddress IPAddress
	Port      Port
	Protocol  Protocol
}

func NewConnectionInfo(ipAddress string, port string, protocol Protocol) (*ConnectionInfo, error) {
	portInt32, err := sdk.ParsePort(port)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to parse port: %v", err)
	}

	// ParsePort already validates range (0-65535), so this conversion is safe
	if portInt32 < 0 || portInt32 > math.MaxUint16 {
		return nil, fleeterror.NewInternalErrorf("port out of uint16 range: %d", portInt32)
	}

	return &ConnectionInfo{
		IPAddress: IPAddress(ipAddress),
		Port:      Port(uint16(portInt32)),
		Protocol:  protocol,
	}, nil
}

func (c ConnectionInfo) getHost() string {
	if c.Port == 0 {
		return string(c.IPAddress)
	}
	return net.JoinHostPort(string(c.IPAddress), c.Port.String())
}

func (c ConnectionInfo) GetURL() *url.URL {
	return &url.URL{
		Scheme: c.Protocol.String(),
		Host:   c.getHost(),
	}
}

func (c ConnectionInfo) GetHostPort() *url.URL {
	return &url.URL{Host: c.getHost()}
}
