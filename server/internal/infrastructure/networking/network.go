package networking

import (
	"net"
	"net/url"
	"strconv"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	"github.com/jackpal/gateway"
)

type NetworkInfo struct {
	Interface string
	LocalIP   string
	Gateway   string
	Subnet    string
}

var emptyNetworkInfo = NetworkInfo{}

func GetLocalNetworkInfo() (NetworkInfo, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return emptyNetworkInfo, fleeterror.NewInternalErrorf("failed to get network interfaces: %v", err)
	}

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

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Skip IPv6
			if ipNet.IP.To4() == nil {
				continue
			}

			// Get gateway IP
			gatewayIP, err := gateway.DiscoverGateway()
			if err != nil {
				return emptyNetworkInfo, fleeterror.NewInternalErrorf("failed to discover gateway: %v", err)
			}

			return NetworkInfo{
				Interface: iface.Name,
				LocalIP:   ipNet.IP.String(),
				Gateway:   gatewayIP.String(),
				Subnet:    ipNet.String(),
			}, nil
		}
	}

	return emptyNetworkInfo, fleeterror.NewInternalError("no suitable network interface found")
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
)

func (p Protocol) String() string {
	switch p {
	case ProtocolHTTP:
		return "http"
	case ProtocolHTTPS:
		return "https"
	case ProtocolTCP:
		return "tcp"
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
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("failed to convert port to int: %v", err)
	}
	if portInt < 0 || portInt > 65535 {
		return nil, fleeterror.NewInternalErrorf("port out of range: %d", portInt)
	}

	return &ConnectionInfo{
		IPAddress: IPAddress(ipAddress),
		Port:      Port(portInt),
		Protocol:  protocol,
	}, nil
}

func (c ConnectionInfo) GetURL() *url.URL {
	return &url.URL{Scheme: c.Protocol.String(), Host: net.JoinHostPort(string(c.IPAddress), c.Port.String())}
}

func (c ConnectionInfo) GetHostPort() *url.URL {
	return &url.URL{Host: net.JoinHostPort(string(c.IPAddress), c.Port.String())}
}
