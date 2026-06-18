package admin

import (
	"testing"

	"github.com/stretchr/testify/assert"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

func TestPairResultStatus_DefaultPasswordActive(t *testing.T) {
	active := true
	result := &gatewaypb.FleetNodePairResult{
		Outcome:               gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED,
		DefaultPasswordActive: &active,
	}

	assert.Equal(t, fleetmanagementv1.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD, pairResultStatus(result))
}
