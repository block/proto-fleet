package infrastructure

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/netutil"
)

// Config carries deployment-managed infrastructure control settings.
type Config struct {
	// OTControlSubnets is the deployment-global positive allowlist for direct
	// infrastructure control. Commas make multi-subnet .env values practical;
	// newlines preserve the canonical site-commissioning shape.
	OTControlSubnets string `help:"Comma- or newline-separated deployment OT control-subnet allowlist. Empty disables infrastructure writes." default:"" env:"OT_CONTROL_SUBNETS"`
}

func (c Config) controlSubnets() ([]netip.Prefix, error) {
	raw := strings.TrimSpace(c.OTControlSubnets)
	if raw == "" {
		return nil, nil
	}

	entries := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	canonical, err := netutil.CanonicalizeInfrastructureControlSubnets(entries)
	if err != nil {
		return nil, fmt.Errorf("deployment OT control-subnet allowlist is invalid: %w", err)
	}

	return append([]netip.Prefix(nil), canonical.Prefixes...), nil
}
