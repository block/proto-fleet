package pairing

import (
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/stableidentity"
)

// AutomaticIPRecoveryEligible reports whether an authenticated pairing result
// contains stable identity evidence that can match the miner at a new endpoint.
func AutomaticIPRecoveryEligible(result *gatewaypb.FleetNodePairResult) bool {
	return result.GetOutcome() == gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED &&
		stableidentity.New(result.GetSerialNumber(), result.GetMacAddress()).Usable()
}
