package main

import (
	"net"
	"slices"
)

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
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
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
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || !ip.IsGlobalUnicast() {
			continue
		}
		ips = append(ips, ip.String())
	}
	slices.Sort(ips)
	ips = slices.Compact(ips)
	if len(ips) > 64 {
		ips = ips[:64]
	}
	return ips
}
