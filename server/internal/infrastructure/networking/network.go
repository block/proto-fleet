package networking

import (
	"fmt"
	"net"

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
		return emptyNetworkInfo, fmt.Errorf("failed to get network interfaces: %w", err)
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
				return emptyNetworkInfo, fmt.Errorf("failed to discover gateway: %w", err)
			}

			return NetworkInfo{
				Interface: iface.Name,
				LocalIP:   ipNet.IP.String(),
				Gateway:   gatewayIP.String(),
				Subnet:    ipNet.String(),
			}, nil
		}
	}

	return emptyNetworkInfo, fmt.Errorf("no suitable network interface found")
}
