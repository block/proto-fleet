package main

import (
	"fmt"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostIPNet builds the *net.IPNet shape an interface reports for a host address
// (host IP + the subnet mask), e.g. "192.168.1.50/24".
func hostIPNet(cidr string) *net.IPNet {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return &net.IPNet{IP: ip, Mask: n.Mask}
}

func stubAddrs(byName map[string][]net.Addr) func(*net.Interface) ([]net.Addr, error) {
	return func(i *net.Interface) ([]net.Addr, error) { return byName[i.Name], nil }
}

func TestSelectLocalPrivateSubnets_Typical24(t *testing.T) {
	// Arrange
	ifaces := []net.Interface{{Name: "eth0", Flags: net.FlagUp | net.FlagRunning}}
	addrs := stubAddrs(map[string][]net.Addr{"eth0": {hostIPNet("192.168.1.50/24")}})

	// Act
	got, err := selectLocalPrivateSubnets(ifaces, addrs)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.0/24"}, got)
}

func TestSelectLocalPrivateSubnets_OversizedMaskNarrowedTo22(t *testing.T) {
	// Arrange: a /8-masked NIC must narrow to /22 around its own host address so
	// the auto scan stays within the manual-path host ceiling.
	ifaces := []net.Interface{{Name: "eth0", Flags: net.FlagUp | net.FlagRunning}}
	addrs := stubAddrs(map[string][]net.Addr{"eth0": {hostIPNet("10.1.2.3/8")}})

	// Act
	got, err := selectLocalPrivateSubnets(ifaces, addrs)

	// Assert
	require.NoError(t, err)
	require.Len(t, got, 1)
	prefix, perr := netip.ParsePrefix(got[0])
	require.NoError(t, perr)
	assert.Equal(t, 22, prefix.Bits(), "oversized mask must narrow to /22")
	assert.True(t, prefix.Contains(netip.MustParseAddr("10.1.2.3")), "narrowed subnet must contain the host: %s", got[0])
}

func TestSelectLocalPrivateSubnets_FiltersLoopbackDownAndVirtual(t *testing.T) {
	// Arrange: loopback, a not-running NIC, and a docker bridge all excluded.
	ifaces := []net.Interface{
		{Name: "lo", Flags: net.FlagUp | net.FlagRunning | net.FlagLoopback},
		{Name: "eth1", Flags: net.FlagUp}, // up but not running
		{Name: "docker0", Flags: net.FlagUp | net.FlagRunning},
	}
	addrs := stubAddrs(map[string][]net.Addr{
		"lo":      {hostIPNet("127.0.0.1/8")},
		"eth1":    {hostIPNet("192.168.5.5/24")},
		"docker0": {hostIPNet("172.17.0.1/16")},
	})

	// Act
	_, err := selectLocalPrivateSubnets(ifaces, addrs)

	// Assert
	require.ErrorIs(t, err, errNoLocalPrivateSubnet)
}

func TestSelectLocalPrivateSubnets_SkipsPublicAddress(t *testing.T) {
	// Arrange
	ifaces := []net.Interface{{Name: "eth0", Flags: net.FlagUp | net.FlagRunning}}
	addrs := stubAddrs(map[string][]net.Addr{"eth0": {hostIPNet("8.8.8.8/24")}})

	// Act
	_, err := selectLocalPrivateSubnets(ifaces, addrs)

	// Assert
	require.ErrorIs(t, err, errNoLocalPrivateSubnet)
}

func TestSelectLocalPrivateSubnets_DedupesSameSubnetAcrossNICs(t *testing.T) {
	// Arrange
	ifaces := []net.Interface{
		{Name: "eth0", Flags: net.FlagUp | net.FlagRunning},
		{Name: "eth1", Flags: net.FlagUp | net.FlagRunning},
	}
	addrs := stubAddrs(map[string][]net.Addr{
		"eth0": {hostIPNet("192.168.1.10/24")},
		"eth1": {hostIPNet("192.168.1.20/24")},
	})

	// Act
	got, err := selectLocalPrivateSubnets(ifaces, addrs)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.0/24"}, got)
}

func TestSelectLocalPrivateSubnets_CapsResultCount(t *testing.T) {
	// Arrange: more distinct private subnets than the cap allows.
	ifaces := make([]net.Interface, 0, maxAutoSubnets+2)
	byName := make(map[string][]net.Addr)
	for i := range maxAutoSubnets + 2 {
		name := fmt.Sprintf("eth%d", i)
		ifaces = append(ifaces, net.Interface{Name: name, Flags: net.FlagUp | net.FlagRunning})
		byName[name] = []net.Addr{hostIPNet(fmt.Sprintf("192.168.%d.5/24", i))}
	}

	// Act
	got, err := selectLocalPrivateSubnets(ifaces, stubAddrs(byName))

	// Assert
	require.NoError(t, err)
	assert.Len(t, got, maxAutoSubnets)
}

func TestSelectLocalPrivateSubnets_IgnoresIPv6ULA(t *testing.T) {
	// Arrange
	ifaces := []net.Interface{{Name: "eth0", Flags: net.FlagUp | net.FlagRunning}}
	addrs := stubAddrs(map[string][]net.Addr{"eth0": {hostIPNet("fd00::1/64")}})

	// Act
	_, err := selectLocalPrivateSubnets(ifaces, addrs)

	// Assert
	require.ErrorIs(t, err, errNoLocalPrivateSubnet)
}

func TestSelectLocalPrivateSubnets_NoInterfaces(t *testing.T) {
	// Act
	_, err := selectLocalPrivateSubnets(nil, stubAddrs(nil))

	// Assert
	require.ErrorIs(t, err, errNoLocalPrivateSubnet)
}
