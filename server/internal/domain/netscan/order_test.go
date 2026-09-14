package netscan

import (
	"net/netip"
	"slices"
	"testing"
)

func TestInterleavedAddressesWrapsAndPreservesTargetBoundaries(t *testing.T) {
	targets := []Target{
		mustTarget(t, "10.0.0.0/30"),
		mustTarget(t, "192.168.1.0/29"),
		mustTarget(t, "fd00::1"),
		mustTarget(t, "10.1.0.0/31"),
		mustTarget(t, "10.2.0.0/32"),
		{}, // Unresolved/empty targets contain no addresses.
	}
	var got []string
	for addr := range interleavedAddresses(targets, func(count uint64) uint64 { return count - 1 }) {
		got = append(got, addr.String())
	}
	want := []string{
		"10.0.0.2", "192.168.1.6", "fd00::1", "10.1.0.1", "10.2.0.0",
		"10.0.0.1", "192.168.1.1", "10.1.0.0",
		"192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
}

func TestInterleavedAddressesIsLazyForBroadTargets(t *testing.T) {
	target := mustTarget(t, "0.0.0.0/0")
	for addr := range interleavedAddresses([]Target{target}, func(count uint64) uint64 { return count - 1 }) {
		if want := netip.MustParseAddr("255.255.255.254"); addr != want {
			t.Fatalf("first address = %s, want %s", addr, want)
		}
		break
	}
}
