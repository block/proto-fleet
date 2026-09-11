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
		{}, // Unresolved/empty targets contain no addresses.
	}
	var got []string
	for addr := range interleavedAddresses(targets, func(count uint64) uint64 { return count - 1 }) {
		got = append(got, addr.String())
	}
	want := []string{
		"10.0.0.2", "192.168.1.6", "fd00::1",
		"10.0.0.1", "192.168.1.1",
		"192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
}

func TestInterleavedAddressesRetryCanReachDifferentHosts(t *testing.T) {
	targets := []Target{mustTarget(t, "10.0.0.0/16"), mustTarget(t, "192.168.0.0/16")}
	firstBatch := func(offset uint64) []netip.Addr {
		var addresses []netip.Addr
		for addr := range interleavedAddresses(targets, func(uint64) uint64 { return offset }) {
			addresses = append(addresses, addr)
			if len(addresses) == 8 {
				break
			}
		}
		return addresses
	}
	first, retry := firstBatch(0), firstBatch(1000)
	for i, addr := range retry {
		if slices.Contains(first, addr) {
			t.Fatalf("retry repeated %s despite different starting offset", addr)
		}
		if !targets[i%len(targets)].Contains(addr) {
			t.Fatalf("address %s was not interleaved from the expected subnet", addr)
		}
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

func TestInterleavedAddressesIncludesEveryHost(t *testing.T) {
	for _, raw := range []string{"10.0.0.0/24", "10.0.0.0/31", "10.0.0.0/32", "fd00::1"} {
		t.Run(raw, func(t *testing.T) {
			target := mustTarget(t, raw)
			got := slices.Collect(InterleavedAddresses([]Target{target}))
			slices.SortFunc(got, netip.Addr.Compare)
			if want := slices.Collect(target.Addresses()); !slices.Equal(got, want) {
				t.Fatalf("random ordering changed target coverage: %v", got)
			}
		})
	}
}
