package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsableIPAddresses(t *testing.T) {
	addresses := []net.Addr{}
	for _, cidr := range []string{"127.0.0.1/8", "::1/128", "169.254.1.1/16", "fe80::1/64", "224.0.0.1/4", "192.168.1.7/24", "fd00::7/64", "192.168.1.7/24", "10.0.0.2/24"} {
		ip, network, err := net.ParseCIDR(cidr)
		require.NoError(t, err)
		network.IP = ip
		addresses = append(addresses, network)
	}
	require.Equal(t, []string{"10.0.0.2", "192.168.1.7", "fd00::7"}, usableIPAddresses(addresses))
}

func TestActiveIPAddressesExcludeDownInterfaces(t *testing.T) {
	interfaces := []net.Interface{
		{Index: 1, Name: "docker0"},
		{Index: 2, Name: "eth0", Flags: net.FlagUp},
		{Index: 3, Name: "tailscale0", Flags: net.FlagUp},
		{Index: 4, Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
	}
	addresses := map[string]string{"docker0": "172.17.0.1/16", "eth0": "192.168.1.50/24", "tailscale0": "100.64.0.7/32", "lo": "127.0.0.1/8"}
	got := activeIPAddresses(interfaces, func(iface net.Interface) ([]net.Addr, error) {
		ip, network, err := net.ParseCIDR(addresses[iface.Name])
		require.NoError(t, err)
		network.IP = ip
		return []net.Addr{network}, nil
	})
	require.Equal(t, []string{"100.64.0.7", "192.168.1.50"}, got)
}
