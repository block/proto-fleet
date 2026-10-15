package main

import (
	"net"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
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

func TestHeartbeatMetadataValidation(t *testing.T) {
	addresses := make([]net.Addr, 65)
	ips := make([]string, len(addresses))
	for i := range addresses {
		ip := net.IPv4(10, 0, 0, byte(i+1))
		addresses[i] = &net.IPNet{IP: ip, Mask: net.CIDRMask(24, 32)}
		ips[i] = ip.String()
	}
	req := &pb.UploadHeartbeatRequest{
		SentAt:      timestamppb.Now(),
		IpAddresses: usableIPAddresses(addresses),
		Version:     strings.Repeat("v", 128),
	}
	require.Len(t, req.IpAddresses, 64)
	require.NoError(t, protovalidate.Validate(req))
	require.NoError(t, protovalidate.Validate(&pb.UploadHeartbeatRequest{SentAt: req.SentAt}))

	for _, tc := range []struct {
		name    string
		ips     []string
		version string
		field   string
	}{
		{name: "too many addresses", ips: ips, version: "dev", field: "ip_addresses"},
		{name: "invalid address", ips: []string{"not-an-ip"}, version: "dev", field: "ip_addresses"},
		{name: "version too long", ips: ips[:1], version: strings.Repeat("v", 129), field: "version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := protovalidate.Validate(&pb.UploadHeartbeatRequest{
				SentAt: req.SentAt, IpAddresses: tc.ips, Version: tc.version,
			})
			require.ErrorContains(t, err, tc.field)
		})
	}
}
