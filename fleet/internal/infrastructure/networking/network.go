package networking

import (
	"fmt"
	"github.com/j-keck/arping"
	"github.com/jackpal/gateway"
	"net"
	"strings"
	"time"
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

// GetMacAddress returns MAC lowercased or empty string
func GetMacAddress(ip string) string {
	// Timeout of 1 second for the ARP request
	arping.SetTimeout(time.Second)

	hwAddr, _, err := arping.Ping(net.ParseIP(ip))
	if err != nil {
		return ""
	}
	return strings.ToLower(hwAddr.String())
}
