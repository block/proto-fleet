package netscan

import (
	"iter"
	"math/rand/v2"
	"net/netip"
)

// InterleavedAddresses visits resolved targets in turns, starting at a random
// address in each and wrapping around. Every address in each target is visited
// once; overlapping targets may repeat addresses. Memory scales with targets, not hosts.
// A retry can cover a different part of a subnet without retaining scan state.
func InterleavedAddresses(targets []Target) iter.Seq[netip.Addr] {
	return interleavedAddresses(targets, rand.Uint64N)
}

func interleavedAddresses(targets []Target, offset func(uint64) uint64) iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		type cursor struct {
			target    Target
			next      netip.Addr
			remaining uint64
		}
		cursors := make([]cursor, 0, len(targets))
		for _, target := range targets {
			count := target.Count()
			if count == 0 {
				continue
			}
			start := target.first
			if count > 1 {
				start = ipv4Addr(ipv4Value(start) + uint32(offset(count))) //nolint:gosec // Offset is below the IPv4 interval size.
			}
			cursors = append(cursors, cursor{target, start, count})
		}
		for len(cursors) > 0 {
			active := cursors[:0]
			for _, c := range cursors {
				if !yield(c.next) {
					return
				}
				c.remaining--
				if c.remaining == 0 {
					continue
				}
				if c.next == c.target.last {
					c.next = c.target.first
				} else {
					c.next = c.next.Next()
				}
				active = append(active, c)
			}
			cursors = active
		}
	}
}
