// Package nmaptarget validates operator-supplied nmap discovery targets against
// the strict grammar the fleet node agent enforces, so the server can reject
// unsupported targets before dispatch instead of after a late BAD_REQUEST ack.
package nmaptarget

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Targets reach nmap as argv (no shell); a leading dash would parse as a
// flag (-iL, -oN reach file r/w on the agent). Range regex distinguishes
// "A.B.C.D-N" from dash-bearing hostnames like "miner-01.lan".
var (
	hostnameRE  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)
	ipv4RangeRE = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}-\d{1,3}$`)
	allowedRE   = regexp.MustCompile(`^[A-Za-z0-9.:/-]+$`)
	// multi-octet ranges (e.g. 10.0-255.0-255.1-254) match hostnameRE but would
	// bypass the /22 cap. Dot-required so all-numeric single labels fall through.
	multiOctetRangeRE = regexp.MustCompile(`^\d+(-\d+)?(\.\d+(-\d+)?)+$`)
)

// MinIPv4PrefixBits caps IPv4 CIDR breadth at 1024 hosts so /0 can't aim a
// multi-hour scan at the public internet. Operators split larger scopes.
const MinIPv4PrefixBits = 22

// IsIPv4Range reports whether s is an "A.B.C.D-N" nmap range.
func IsIPv4Range(s string) bool { return ipv4RangeRE.MatchString(s) }

// IsHostname reports whether s matches the DNS hostname grammar (letters,
// digits, and hyphens in dot-separated labels). Callers use it to validate a
// non-IP IpList entry before dispatch so a malformed token (e.g. "bad/entry")
// can't masquerade as a hostname and widen the scan scope to ports only.
func IsHostname(s string) bool { return hostnameRE.MatchString(s) }

// Validate checks s against the nmap target grammar: bare IPv4/IPv6, CIDR
// (IPv4 no broader than /MinIPv4PrefixBits), "A.B.C.D-N" range, or hostname.
// Leading dashes and shell metacharacters are rejected. IPv6 CIDR passes the
// grammar but is unsupported for scanning — callers that dispatch must also
// reject it (see resolveNmapTarget / the admin handler).
func Validate(s string) error {
	if s == "" {
		return errors.New("nmap target is required")
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("nmap target %q must not start with '-'", s)
	}
	if !allowedRE.MatchString(s) {
		return fmt.Errorf("nmap target %q contains disallowed characters", s)
	}
	if prefix, perr := netip.ParsePrefix(s); perr == nil {
		if prefix.Addr().Is4() && prefix.Bits() < MinIPv4PrefixBits {
			return fmt.Errorf("nmap target %q has CIDR prefix /%d shorter than the supported minimum /%d", s, prefix.Bits(), MinIPv4PrefixBits)
		}
		return nil
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return nil
	}
	if ipv4RangeRE.MatchString(s) {
		head, tail, _ := strings.Cut(s, "-")
		ip := net.ParseIP(head)
		n, perr := strconv.Atoi(tail)
		if ip == nil || ip.To4() == nil || perr != nil || n < 0 || n > 255 {
			return fmt.Errorf("nmap target %q has invalid IPv4 range", s)
		}
		// nmap reads "A.B.C.D-N" as last-octet D..N, so N must not be below the
		// head's last octet. A descending range like 192.168.1.50-10 otherwise
		// passes the grammar and later disables IP scope enforcement (the scope
		// matcher falls through to accept-all).
		if lastOctet := int(ip.To4()[3]); n < lastOctet {
			return fmt.Errorf("nmap target %q has a descending IPv4 range", s)
		}
		return nil
	}
	if multiOctetRangeRE.MatchString(s) {
		return fmt.Errorf("nmap target %q: multi-octet IPv4 ranges are not supported; use IpRange mode or split into separate commands", s)
	}
	if hostnameRE.MatchString(s) {
		return nil
	}
	return fmt.Errorf("nmap target %q is not a valid IP, CIDR, range, or hostname", s)
}
