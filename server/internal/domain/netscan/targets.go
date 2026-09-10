package netscan

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

// LocalSubnetTarget asks a Fleet Node to select its own private IPv4 subnets.
const LocalSubnetTarget = "fleet-node-local-subnet"

var (
	hostnameRE        = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)
	ipv4RangeRE       = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}-\d{1,3}$`)
	multiOctetRangeRE = regexp.MustCompile(`^\d+(-\d+)?(\.\d+(-\d+)?)+$`)
	privateIPv4       = [...]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
)

// Resolver is the DNS lookup used by both server and Fleet Node discovery.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Target is an inclusive address interval or an unresolved hostname. Its zero
// value contains no addresses. CIDRs store only their usable host interval.
type Target struct {
	first    netip.Addr
	last     netip.Addr
	hostname string
}

// isHostname reports whether raw matches the supported DNS hostname grammar.
func isHostname(raw string) bool { return hostnameRE.MatchString(raw) }

// isIPv4Range reports whether raw has the supported A.B.C.D-N range shape.
func isIPv4Range(raw string) bool { return ipv4RangeRE.MatchString(raw) }

// ParseBoundedTarget parses a target with the Fleet Node CIDR breadth limit.
func ParseBoundedTarget(raw string) (Target, error) {
	target, err := ParseTarget(raw)
	if err != nil {
		return Target{}, err
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Bits() < discoverylimits.MinIPv4PrefixBits {
		return Target{}, fmt.Errorf("scan target %q has CIDR prefix /%d shorter than the supported minimum /%d", raw, prefix.Bits(), discoverylimits.MinIPv4PrefixBits)
	}
	return target, nil
}

// ParseTarget accepts an IP literal, IPv4 CIDR, last-octet IPv4 range, or
// hostname. It does not perform DNS or enumerate the address interval. Fleet
// Node requests use ParseBoundedTarget to apply their per-target CIDR breadth cap;
// server-local discovery retains its existing broader subnet policy.
func ParseTarget(raw string) (Target, error) {
	if raw == "" {
		return Target{}, errors.New("scan target is required")
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		if !prefix.Addr().Is4() {
			return Target{}, fmt.Errorf("scan target %q: IPv6 CIDRs are not supported", raw)
		}
		first := prefix.Masked().Addr()
		last := ipv4Addr(ipv4Value(first) | (^uint32(0) >> prefix.Bits()))
		if prefix.Bits() <= 30 {
			first = first.Next()
			last = last.Prev()
		}
		return Target{first: first, last: last}, nil
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		if !usableAddr(addr) {
			return Target{}, fmt.Errorf("scan target %q: scoped and link-local IPv6 addresses are not supported", raw)
		}
		addr = addr.Unmap()
		return Target{first: addr, last: addr}, nil
	}
	if isIPv4Range(raw) {
		head, tail, _ := strings.Cut(raw, "-")
		start, err := netip.ParseAddr(head)
		lastOctet, parseErr := strconv.ParseUint(tail, 10, 8)
		if err != nil || !start.Is4() || parseErr != nil {
			return Target{}, fmt.Errorf("scan target %q has invalid IPv4 range", raw)
		}
		end := ipv4Addr(ipv4Value(start)&0xffffff00 | uint32(lastOctet))
		if end.Less(start) {
			return Target{}, fmt.Errorf("scan target %q has a descending IPv4 range", raw)
		}
		return Target{first: start, last: end}, nil
	}
	if multiOctetRangeRE.MatchString(raw) {
		return Target{}, fmt.Errorf("scan target %q: multi-octet IPv4 ranges are not supported; use IP range mode", raw)
	}
	if isHostname(raw) {
		return Target{hostname: raw}, nil
	}
	return Target{}, fmt.Errorf("scan target %q is not a valid IP, CIDR, range, or hostname", raw)
}

// Range accepts inclusive IPv4 endpoints without a size cap. Callers apply the
// limit for their discovery mode before consuming Addresses.
func Range(start, end string) (Target, error) {
	first, err := netip.ParseAddr(start)
	if err != nil || first.Zone() != "" || !first.Unmap().Is4() {
		return Target{}, fmt.Errorf("invalid start_ip: %q", start)
	}
	last, err := netip.ParseAddr(end)
	if err != nil || last.Zone() != "" || !last.Unmap().Is4() {
		return Target{}, fmt.Errorf("invalid end_ip: %q", end)
	}
	first, last = first.Unmap(), last.Unmap()
	if last.Less(first) {
		return Target{}, errors.New("end_ip must be >= start_ip")
	}
	return Target{first: first, last: last}, nil
}

// Count returns the number of addresses yielded, or zero for an unresolved host.
func (t Target) Count() uint64 {
	if !t.first.IsValid() {
		return 0
	}
	if t.first.Is4() {
		return uint64(ipv4Value(t.last)) - uint64(ipv4Value(t.first)) + 1
	}
	return 1 // IPv6 targets are literals only.
}

// Addresses enumerates lazily and stops immediately when the consumer stops.
// Resolve must be called first for hostname targets.
func (t Target) Addresses() iter.Seq[netip.Addr] {
	return func(yield func(netip.Addr) bool) {
		if !t.first.IsValid() {
			return
		}
		for addr := t.first; ; addr = addr.Next() {
			if !yield(addr) || addr == t.last {
				return
			}
		}
	}
}

// Contains checks the same interval that Addresses yields. An unresolved host
// permits any usable address because the server cannot predict node-side DNS.
func (t Target) Contains(addr netip.Addr) bool {
	if !usableAddr(addr) {
		return false
	}
	addr = addr.Unmap()
	if t.hostname != "" {
		return true
	}
	return t.first.IsValid() && t.first.Is4() == addr.Is4() && !addr.Less(t.first) && !t.last.Less(addr)
}

// IsPrivate reports whether the entire address interval is private. Hostnames
// return true until Resolve applies the caller's address policy to DNS results.
func (t Target) IsPrivate() bool {
	if t.hostname != "" {
		return true
	}
	if !t.first.IsPrivate() || !t.last.IsPrivate() {
		return false
	}
	if !t.first.Is4() {
		return true // IPv6 targets are literals only.
	}
	for _, prefix := range privateIPv4 {
		if prefix.Contains(t.first) && prefix.Contains(t.last) {
			return true
		}
	}
	return false
}

// Resolve resolves a hostname once, filtering unusable or disallowed answers
// before preferring IPv4. Non-hostname targets only undergo the address policy.
func (t Target) Resolve(ctx context.Context, resolver Resolver, privateOnly bool) (Target, error) {
	if t.hostname == "" {
		if t.Count() == 0 {
			return Target{}, errors.New("scan target has no addresses")
		}
		if privateOnly && !t.IsPrivate() {
			return Target{}, errors.New("scan target must contain only private addresses")
		}
		return t, nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	answers, err := resolver.LookupIPAddr(ctx, t.hostname)
	if err != nil {
		return Target{}, fmt.Errorf("resolve %s: %w", t.hostname, err)
	}
	var ipv6 netip.Addr
	for _, answer := range answers {
		addr, ok := netip.AddrFromSlice(answer.IP)
		if !ok || answer.Zone != "" || !usableAddr(addr) {
			continue
		}
		addr = addr.Unmap()
		if privateOnly && !addr.IsPrivate() {
			continue
		}
		if addr.Is4() {
			return Target{first: addr, last: addr}, nil
		}
		if !ipv6.IsValid() {
			ipv6 = addr
		}
	}
	if ipv6.IsValid() {
		return Target{first: ipv6, last: ipv6}, nil
	}
	return Target{}, fmt.Errorf("hostname %s did not resolve to a usable address", t.hostname)
}

// ParseAddrTarget accepts only an IP literal or hostname for IP-list discovery.
// It shares the supported address grammar with ParseTarget without accepting
// CIDRs or ranges, resolving DNS, or applying a private-address policy.
func ParseAddrTarget(raw string) (Target, error) {
	if _, err := netip.ParseAddr(raw); err != nil && (!isHostname(raw) || isIPv4Range(raw) || multiOctetRangeRE.MatchString(raw)) {
		return Target{}, fmt.Errorf("invalid IP address or hostname: %q", raw)
	}
	return ParseTarget(raw)
}

// ResolveAddr normalizes an IP-list entry to one address with the same DNS
// selection and private-address policy as network scan targets.
func ResolveAddr(ctx context.Context, raw string, resolver Resolver, privateOnly bool) (netip.Addr, error) {
	target, err := ParseAddrTarget(raw)
	if err != nil {
		return netip.Addr{}, err
	}
	target, err = target.Resolve(ctx, resolver, privateOnly)
	return target.first, err
}

// Ports validates before deduplicating so repeated values cannot bypass the
// request limit. Empty input selects the caller's existing default port set.
func Ports(raw, defaults []string) ([]uint16, error) {
	if len(raw) == 0 {
		raw = defaults
	}
	if len(raw) == 0 {
		return nil, errors.New("at least one scan port is required")
	}
	if len(raw) > discoverylimits.MaxPortsPerIP {
		return nil, fmt.Errorf("scan supports at most %d ports per IP", discoverylimits.MaxPortsPerIP)
	}
	ports := make([]uint16, 0, len(raw))
	for _, value := range raw {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid port %q: expected 1..65535", value)
		}
		ports = append(ports, uint16(port))
	}
	slices.Sort(ports)
	return slices.Compact(ports), nil
}

func usableAddr(addr netip.Addr) bool {
	return addr.IsValid() && addr.Zone() == "" && (!addr.Is6() || addr.Is4In6() || !addr.IsLinkLocalUnicast())
}

func ipv4Value(addr netip.Addr) uint32 {
	b := addr.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func ipv4Addr(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
