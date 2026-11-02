// Package discoveryranges parses private IPv4 discovery targets without enumerating addresses.
package discoveryranges

import (
	"encoding/binary"
	"fmt"
	"iter"
	"net/netip"
	"slices"
	"sort"
	"strings"
)

// Range is an inclusive IPv4 interval.
type Range struct {
	Start netip.Addr
	End   netip.Addr
}

// Scope is a normalized set of inclusive private IPv4 intervals.
// Its zero value is empty.
type Scope struct {
	ranges []Range
}

var privateNetworks = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

// Parse accepts IPv4 addresses, CIDRs, and inclusive full-address start-end ranges.
// Every entry must lie entirely within one RFC 1918 private network.
func Parse(values []string) (Scope, error) {
	ranges := make([]Range, 0, len(values))
	for _, value := range values {
		r, err := parseRange(strings.TrimSpace(value))
		if err != nil {
			return Scope{}, fmt.Errorf("discovery range %q: %w", value, err)
		}
		ranges = append(ranges, r)
	}
	slices.SortFunc(ranges, func(a, b Range) int {
		if comparison := a.Start.Compare(b.Start); comparison != 0 {
			return comparison
		}
		return a.End.Compare(b.End)
	})
	merged := ranges[:0]
	for _, r := range ranges {
		if len(merged) == 0 || r.Start.Compare(merged[len(merged)-1].End.Next()) > 0 {
			merged = append(merged, r)
			continue
		}
		if last := &merged[len(merged)-1]; r.End.Compare(last.End) > 0 {
			last.End = r.End
		}
	}
	return Scope{ranges: merged}, nil
}

func parseRange(value string) (Range, error) {
	var r Range
	var err error
	switch {
	case strings.Contains(value, "/"):
		var prefix netip.Prefix
		prefix, err = netip.ParsePrefix(value)
		if err == nil && prefix.Addr().Is4() {
			r.Start = prefix.Masked().Addr()
			start := r.Start.As4()
			end := binary.BigEndian.Uint32(start[:]) | (^uint32(0) >> prefix.Bits())
			var endBytes [4]byte
			binary.BigEndian.PutUint32(endBytes[:], end)
			r.End = netip.AddrFrom4(endBytes)
		}
	case strings.Contains(value, "-"):
		start, end, _ := strings.Cut(value, "-")
		r.Start, err = netip.ParseAddr(strings.TrimSpace(start))
		if err == nil {
			r.End, err = netip.ParseAddr(strings.TrimSpace(end))
		}
	default:
		r.Start, err = netip.ParseAddr(value)
		r.End = r.Start
	}
	if err != nil {
		return Range{}, fmt.Errorf("parse IPv4 range: %w", err)
	}
	return r, validateRange(r)
}

func validateRange(r Range) error {
	if !r.Start.Is4() || !r.End.Is4() {
		return fmt.Errorf("only IPv4 addresses are supported")
	}
	if r.Start.Compare(r.End) > 0 {
		return fmt.Errorf("start address must not exceed end address")
	}
	if !WithinPrivateNetwork(r.Start, r.End) {
		return fmt.Errorf("range must remain within one private IPv4 network")
	}
	return nil
}

// WithinPrivateNetwork reports whether both addresses lie in the same RFC 1918 network.
func WithinPrivateNetwork(start, end netip.Addr) bool {
	for _, network := range privateNetworks {
		if network.Contains(start) && network.Contains(end) {
			return true
		}
	}
	return false
}

// Strings returns sorted, merged intervals as IPs or inclusive start-end strings.
func (s Scope) Strings() []string {
	result := make([]string, 0, len(s.ranges))
	for _, r := range s.ranges {
		value := r.Start.String()
		if r.Start != r.End {
			value += "-" + r.End.String()
		}
		result = append(result, value)
	}
	return result
}

// Ranges returns a copy of the sorted, disjoint intervals.
func (s Scope) Ranges() []Range { return slices.Clone(s.ranges) }

func (s Scope) Empty() bool { return len(s.ranges) == 0 }

func (s Scope) Contains(address netip.Addr) bool {
	if !address.Is4() {
		return false
	}
	index := sort.Search(len(s.ranges), func(i int) bool {
		return s.ranges[i].End.Compare(address) >= 0
	})
	return index < len(s.ranges) && s.ranges[index].Start.Compare(address) <= 0
}

func (s Scope) ContainsString(address string) bool {
	parsed, err := netip.ParseAddr(address)
	return err == nil && s.Contains(parsed)
}

// Addresses lazily yields each configured IPv4 address once in ascending order.
func (s Scope) Addresses() iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		for _, r := range s.ranges {
			for address := r.Start; ; address = address.Next() {
				if !yield(address) {
					return
				}
				if address == r.End {
					break
				}
			}
		}
	}
}
