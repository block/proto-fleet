package main

import "net"

// These mirror the UploadHeartbeatRequest validation rules.
const (
	maxReportedIPAddresses = 64
	maxReportedVersionLen  = 128
)

func reportedVersion() string {
	runes := []rune(version)
	if len(runes) > maxReportedVersionLen {
		runes = runes[:maxReportedVersionLen]
	}
	return string(runes)
}

func localIPAddresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	return activeIPAddresses(interfaces, func(iface net.Interface) ([]net.Addr, error) {
		return iface.Addrs()
	})
}

func activeIPAddresses(interfaces []net.Interface, getAddresses func(net.Interface) ([]net.Addr, error)) []string {
	var addresses []net.Addr
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagRunning == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		current, err := getAddresses(iface)
		if err != nil {
			continue
		}
		addresses = append(addresses, current...)
	}
	return usableIPAddresses(addresses)
}

func usableIPAddresses(addresses []net.Addr) []string {
	ips := make([]string, 0, len(addresses))
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || !ip.IsGlobalUnicast() {
			continue
		}
		addr := ip.String()
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		ips = append(ips, addr)
	}
	if len(ips) > maxReportedIPAddresses {
		ips = ips[:maxReportedIPAddresses]
	}
	return ips
}
