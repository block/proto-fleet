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
