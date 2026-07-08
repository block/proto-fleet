// Package netutil holds small networking helpers shared across domain
// packages. The contents are deliberately narrow: a function lands here
// only when at least two domains need the same primitive.
package netutil

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ErrEmptyCIDR is returned by ParseCIDROrIP when the input string is
// empty. Callers wrap it with their own field/index context.
var ErrEmptyCIDR = errors.New("empty value")

// ParseCIDROrIP accepts either a CIDR ("10.0.0.0/24") or a bare IP
// ("10.0.0.5", treated as /32 for IPv4 and /128 for IPv6) and returns
// the prefix masked to its network address so equality and overlap
// checks are canonical. Callers add their own context (field name,
// index, line number) to the returned error.
func ParseCIDROrIP(raw string) (netip.Prefix, error) {
	if raw == "" {
		return netip.Prefix{}, ErrEmptyCIDR
	}
	if !strings.Contains(raw, "/") {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid IP address: %w", err)
		}
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR: %w", err)
	}
	if !prefix.IsValid() {
		return netip.Prefix{}, errors.New("invalid CIDR")
	}
	return prefix.Masked(), nil
}

// ParseIPRange parses an inclusive IP range from start/end address strings.
// Both must be valid IPs of the same family, and end must be >= start.
// Callers add their own context (field name, index) to the returned error.
func ParseIPRange(start, end string) (netip.Addr, netip.Addr, error) {
	if start == "" || end == "" {
		return netip.Addr{}, netip.Addr{}, ErrEmptyCIDR
	}
	s, err := netip.ParseAddr(start)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("invalid start IP: %w", err)
	}
	e, err := netip.ParseAddr(end)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("invalid end IP: %w", err)
	}
	if s.Is4() != e.Is4() {
		return netip.Addr{}, netip.Addr{}, errors.New("start and end IP must be the same family")
	}
	if e.Less(s) {
		return netip.Addr{}, netip.Addr{}, errors.New("end IP must be >= start IP")
	}
	return s, e, nil
}
