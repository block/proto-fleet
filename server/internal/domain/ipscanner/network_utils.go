package ipscanner

import (
	"fmt"
	"net"
)

// parseCIDR parses a CIDR notation string and returns the network information
func parseCIDR(cidr string) (*net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR notation %s: %w", cidr, err)
	}
	return ipNet, nil
}

// generateIPsFromCIDR generates all valid host IPs from a CIDR notation
// It excludes network address, gateway, and broadcast address
func generateIPsFromCIDR(cidr string) ([]string, error) {
	ipNet, err := parseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	// Host routes (/32 for IPv4, /128 for IPv6) are single-address probes —
	// return the address directly without network/gateway skip logic.
	ones, bits := ipNet.Mask.Size()
	if ones == bits {
		return []string{ipNet.IP.String()}, nil
	}

	// Refuse to enumerate CIDRs with more than 16 host bits to prevent
	// accidental OOM (e.g. a /64 IPv6 prefix would be 2^64 addresses).
	if hostBits := bits - ones; hostBits > 16 {
		return nil, fmt.Errorf("CIDR %s has %d host bits; refusing to enumerate (max 16)", cidr, hostBits)
	}

	var ips []string

	// Get first and last IP in range
	firstIP := ipNet.IP.Mask(ipNet.Mask)

	// Calculate the last IP in the range
	lastIP := make(net.IP, len(firstIP))
	copy(lastIP, firstIP)

	// Set all host bits to 1
	for i := range lastIP {
		lastIP[i] = firstIP[i] | ^ipNet.Mask[i]
	}

	// Skip the network address (.0) and gateway (.1)
	// In Docker bridge networks and most standard networks, the gateway
	// is typically the first usable IP address, which can cause discovery
	// issues where all devices appear to respond at the gateway IP
	gatewayIP := incrementIP(firstIP)
	startIP := incrementIP(gatewayIP)

	// Iterate through all IPs in range, starting from .2
	for ip := startIP; !ip.Equal(lastIP); ip = incrementIP(ip) {
		if ipNet.Contains(ip) {
			ips = append(ips, ip.String())
		}
	}

	return ips, nil
}

// incrementIP increments an IP address by one
func incrementIP(ip net.IP) net.IP {
	// Make a copy
	nextIP := make(net.IP, len(ip))
	copy(nextIP, ip)

	// Increment from the right (least significant byte)
	for i := len(nextIP) - 1; i >= 0; i-- {
		nextIP[i]++
		if nextIP[i] != 0 {
			break
		}
	}

	return nextIP
}

// ipToSubnet converts an IP address to its subnet CIDR notation with the given mask
// For example: ipToSubnet("192.168.1.100", 24) -> "192.168.1.0/24"
func ipToSubnet(ip string, maskBits int) (string, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "", fmt.Errorf("invalid IP address: %s", ip)
	}

	// Use /64 for IPv6 if mask not specified (maskBits == 0)
	if parsedIP.To4() == nil && maskBits == 0 {
		maskBits = 64
	}

	return getSubnetFromIPAndMask(ip, maskBits)
}

// getSubnetFromIPAndMask returns subnet CIDR from IP and mask
func getSubnetFromIPAndMask(ip string, maskBits int) (string, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "", fmt.Errorf("invalid IP address: %s", ip)
	}

	var mask net.IPMask
	if parsedIP.To4() != nil {
		mask = net.CIDRMask(maskBits, 32)
	} else {
		mask = net.CIDRMask(maskBits, 128)
	}

	network := parsedIP.Mask(mask)
	return fmt.Sprintf("%s/%d", network.String(), maskBits), nil
}
