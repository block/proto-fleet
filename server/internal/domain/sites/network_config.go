package sites

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// MaxBroadestPrefixBitsV4 is the upper bound on IPv4 subnet sizes
// admitted by network_config validation. Any CIDR with mask < /20 is
// rejected to prevent inadvertent scans across tens of thousands of
// hosts. Per the plan the exact /20 cap is calibrated against real
// Block-ops sites before a final lock; treat this constant as the
// single point of truth for now.
const MaxBroadestPrefixBitsV4 = 20

// MaxNetworkConfigEntries caps the number of parsed prefixes per site
// so the O(N^2) overlap check stays bounded. 256 is comfortably above
// the largest real Block-ops site.
const MaxNetworkConfigEntries = 256

// CanonicalizeNetworkConfigResult is the output of network_config
// validation: canonical form (newline-separated), warnings (cross-site
// overlap, etc.), or a structured InvalidArgument error if any line
// fails the within-site rules.
type CanonicalizeNetworkConfigResult struct {
	Canonical string
	Prefixes  []netip.Prefix
}

// CanonicalizeNetworkConfig validates and canonicalizes the raw
// newline-separated text from a CreateSite/UpdateSite request.
// Returns an InvalidArgument error if any line fails to parse, exceeds
// the broadest-prefix cap, or duplicates/overlaps another entry on the
// same site. Cross-site overlap warnings are NOT computed here —
// callers handle that against the rest of the org.
func CanonicalizeNetworkConfig(raw string) (CanonicalizeNetworkConfigResult, error) {
	if strings.TrimSpace(raw) == "" {
		return CanonicalizeNetworkConfigResult{}, nil
	}

	lines := strings.Split(raw, "\n")
	prefixes := make([]netip.Prefix, 0, len(lines))
	for i, line := range lines {
		entry := strings.TrimSpace(line)
		if entry == "" {
			continue
		}
		prefix, perr := parseNetworkEntry(entry)
		if perr != nil {
			return CanonicalizeNetworkConfigResult{}, fleeterror.NewInvalidArgumentErrorf(
				"network_config line %d (%q): %s", i+1, entry, perr.Error(),
			)
		}
		if prefix.Addr().Is6() {
			// IPv6 entries are host-only (/128). The broader "many hosts"
			// concern doesn't apply at IPv6 sizes the way it does for v4.
			if prefix.Bits() != 128 {
				return CanonicalizeNetworkConfigResult{}, fleeterror.NewInvalidArgumentErrorf(
					"network_config line %d (%q): IPv6 entries must be /128 host addresses",
					i+1, entry,
				)
			}
		} else if prefix.Bits() < MaxBroadestPrefixBitsV4 {
			return CanonicalizeNetworkConfigResult{}, fleeterror.NewInvalidArgumentErrorf(
				"network_config line %d (%q): subnet broader than /%d not allowed",
				i+1, entry, MaxBroadestPrefixBitsV4,
			)
		}
		prefixes = append(prefixes, prefix)
	}

	if len(prefixes) > MaxNetworkConfigEntries {
		return CanonicalizeNetworkConfigResult{}, fleeterror.NewInvalidArgumentErrorf(
			"network_config: too many entries (%d > %d)", len(prefixes), MaxNetworkConfigEntries,
		)
	}

	if dup, ok := findOverlap(prefixes); ok {
		return CanonicalizeNetworkConfigResult{}, fleeterror.NewInvalidArgumentErrorf(
			"network_config: %q overlaps %q within the same site",
			dup.A.String(), dup.B.String(),
		)
	}

	return CanonicalizeNetworkConfigResult{
		Canonical: canonicalText(prefixes),
		Prefixes:  prefixes,
	}, nil
}

// parseNetworkEntry accepts either a CIDR ("10.0.0.0/24") or a bare
// IP ("10.0.0.5", treated as /32 or /128).
func parseNetworkEntry(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR")
		}
		return p.Masked(), nil
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP address")
	}
	bits := 32
	if addr.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits), nil
}

// OverlapPair captures the two prefixes that triggered an overlap
// rejection so error messages can name both offenders.
type OverlapPair struct {
	A netip.Prefix
	B netip.Prefix
}

// findOverlap returns the first overlapping pair in prefixes, if any.
func findOverlap(prefixes []netip.Prefix) (OverlapPair, bool) {
	for i := range prefixes {
		for j := i + 1; j < len(prefixes); j++ {
			if prefixes[i].Overlaps(prefixes[j]) {
				return OverlapPair{A: prefixes[i], B: prefixes[j]}, true
			}
		}
	}
	return OverlapPair{}, false
}

// canonicalText renders prefixes back to newline-separated text in a
// stable, sorted order so round-trip writes don't shuffle the file.
func canonicalText(prefixes []netip.Prefix) string {
	if len(prefixes) == 0 {
		return ""
	}
	out := make([]string, len(prefixes))
	for i, p := range prefixes {
		out[i] = p.String()
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// CrossSiteOverlapWarnings returns one warning string per pair where
// `subject` overlaps a prefix from `other`. `otherLabel` typically
// names the other site. The result is non-blocking: callers persist
// `subject` and surface the warning slice via the response.
func CrossSiteOverlapWarnings(subject []netip.Prefix, other []netip.Prefix, otherLabel string) []string {
	if len(subject) == 0 || len(other) == 0 {
		return nil
	}
	var warnings []string
	for _, s := range subject {
		for _, o := range other {
			if s.Overlaps(o) {
				warnings = append(warnings, fmt.Sprintf(
					"%s overlaps %s on site %q", s.String(), o.String(), otherLabel,
				))
			}
		}
	}
	return warnings
}
