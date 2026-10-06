package netutil

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// MaxInfrastructureControlSubnets bounds commissioning input and the
	// quadratic overlap check.
	MaxInfrastructureControlSubnets = 256

	// BroadestInfrastructureControlPrefixV4 limits a commissioned IPv4
	// segment to 4096 addresses. IPv6 commissioning is host-only.
	BroadestInfrastructureControlPrefixV4 = 20
)

// CanonicalInfrastructureControlSubnets is the validated control-network
// allowlist shared by site commissioning and deployment configuration.
type CanonicalInfrastructureControlSubnets struct {
	Canonical string
	Subnets   []string
	Prefixes  []netip.Prefix
}

type indexedControlPrefix struct {
	line   int
	prefix netip.Prefix
}

// CanonicalizeInfrastructureControlSubnets validates a positive allowlist for
// OT control endpoints. Errors identify the field and line without echoing
// submitted topology.
func CanonicalizeInfrastructureControlSubnets(entries []string) (CanonicalInfrastructureControlSubnets, error) {
	if len(entries) > MaxInfrastructureControlSubnets {
		return CanonicalInfrastructureControlSubnets{}, fleeterror.NewInvalidArgumentErrorf(
			"infrastructure_control_subnets: too many entries (%d > %d)",
			len(entries),
			MaxInfrastructureControlSubnets,
		)
	}

	prefixes := make([]indexedControlPrefix, 0, len(entries))
	for i, raw := range entries {
		line := i + 1
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return CanonicalInfrastructureControlSubnets{}, controlSubnetLineError(
				line,
				"must be a valid CIDR prefix",
			)
		}

		addr := prefix.Addr()
		if addr.Is4In6() ||
			addr.IsUnspecified() ||
			addr.IsLoopback() ||
			addr.IsLinkLocalUnicast() ||
			addr.IsLinkLocalMulticast() ||
			!addr.IsPrivate() {
			return CanonicalInfrastructureControlSubnets{}, controlSubnetLineError(
				line,
				"must be an RFC1918 or IPv6 ULA prefix",
			)
		}

		switch {
		case addr.Is4() && prefix.Bits() < BroadestInfrastructureControlPrefixV4:
			return CanonicalInfrastructureControlSubnets{}, controlSubnetLineError(
				line,
				"IPv4 prefix cannot be broader than /20",
			)
		case addr.Is6() && prefix.Bits() != 128:
			return CanonicalInfrastructureControlSubnets{}, controlSubnetLineError(
				line,
				"IPv6 prefix must be /128",
			)
		}

		prefixes = append(prefixes, indexedControlPrefix{
			line:   line,
			prefix: prefix.Masked(),
		})
	}

	for i := range prefixes {
		for j := i + 1; j < len(prefixes); j++ {
			if prefixes[i].prefix.Overlaps(prefixes[j].prefix) {
				return CanonicalInfrastructureControlSubnets{}, fleeterror.NewInvalidArgumentErrorf(
					"infrastructure_control_subnets lines %d and %d: prefixes must not overlap",
					prefixes[i].line,
					prefixes[j].line,
				)
			}
		}
	}

	parsed := make([]netip.Prefix, len(prefixes))
	for i, prefix := range prefixes {
		parsed[i] = prefix.prefix
	}
	slices.SortFunc(parsed, func(a, b netip.Prefix) int {
		return strings.Compare(a.String(), b.String())
	})

	subnets := make([]string, len(parsed))
	for i, prefix := range parsed {
		subnets[i] = prefix.String()
	}

	return CanonicalInfrastructureControlSubnets{
		Canonical: strings.Join(subnets, "\n"),
		Subnets:   subnets,
		Prefixes:  parsed,
	}, nil
}

func controlSubnetLineError(line int, reason string) error {
	return fleeterror.NewInvalidArgumentErrorf(
		"infrastructure_control_subnets line %d: %s",
		line,
		reason,
	)
}
