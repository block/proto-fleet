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
	if !r.Start.Is4() || !r.End.Is4() {
		return Range{}, fmt.Errorf("only IPv4 addresses are supported")
	}
	if r.Start.Compare(r.End) > 0 {
		return Range{}, fmt.Errorf("start address must not exceed end address")
	}
	for _, network := range privateNetworks {
		if network.Contains(r.Start) && network.Contains(r.End) {
			return r, nil
		}
	}
	return Range{}, fmt.Errorf("range must remain within one private IPv4 network")
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

// Intersect returns the portions of this scope within the inclusive IPv4 interval.
// Invalid, IPv6, and reversed bounds produce no intersection.
func (s Scope) Intersect(start, end netip.Addr) []Range {
	if !start.Is4() || !end.Is4() || start.Compare(end) > 0 {
		return nil
	}
	var result []Range
	for _, r := range s.ranges {
		if r.Start.Compare(end) > 0 {
			break
		}
		if r.End.Compare(start) < 0 {
			continue
		}
		if r.Start.Compare(start) < 0 {
			r.Start = start
		}
		if r.End.Compare(end) > 0 {
			r.End = end
		}
		result = append(result, r)
	}
	return result
}

// IntersectScope returns the addresses permitted by both scopes.
func (s Scope) IntersectScope(other Scope) Scope {
	var result []Range
	for left, right := 0, 0; left < len(s.ranges) && right < len(other.ranges); {
		a, b := s.ranges[left], other.ranges[right]
		start, end := a.Start, a.End
		if b.Start.Compare(start) > 0 {
			start = b.Start
		}
		if b.End.Compare(end) < 0 {
			end = b.End
		}
		if start.Compare(end) <= 0 {
			result = append(result, Range{Start: start, End: end})
		}
		if a.End.Compare(b.End) <= 0 {
			left++
		} else {
			right++
		}
	}
	return Scope{ranges: result}
}

// Chunks lazily yields unique IPv4 address strings in ascending order.
// Each chunk contains at most maxAddresses entries. maxAddresses must be positive.
func (s Scope) Chunks(maxAddresses int) iter.Seq[[]string] {
	if maxAddresses <= 0 {
		panic("discoveryranges: chunk size must be positive")
	}
	return func(yield func([]string) bool) {
		var chunk []string
		for _, r := range s.ranges {
			for address := r.Start; ; address = address.Next() {
				chunk = append(chunk, address.String())
				if len(chunk) == maxAddresses {
					if !yield(chunk) {
						return
					}
					chunk = nil
				}
				if address == r.End {
					break
				}
			}
		}
		if len(chunk) > 0 {
			yield(chunk)
		}
	}
}
