package sites

import (
	"github.com/block/proto-fleet/server/internal/domain/netutil"
)

// MaxInfrastructureControlSubnets bounds commissioning input and the
// quadratic overlap check. The proto repeats the numeric value so oversized
// requests are rejected before reaching the domain.
const MaxInfrastructureControlSubnets = netutil.MaxInfrastructureControlSubnets

// CanonicalInfrastructureControlSubnets is the validated site commissioning
// shape. Canonical is persisted as newline-separated text; Subnets is returned
// by the dedicated RPC.
type CanonicalInfrastructureControlSubnets = netutil.CanonicalInfrastructureControlSubnets

// CanonicalizeInfrastructureControlSubnets validates the per-site positive
// allowlist for OT control endpoints. Error messages identify the request
// field and line, but deliberately never include submitted topology.
func CanonicalizeInfrastructureControlSubnets(entries []string) (CanonicalInfrastructureControlSubnets, error) {
	return netutil.CanonicalizeInfrastructureControlSubnets(entries)
}
