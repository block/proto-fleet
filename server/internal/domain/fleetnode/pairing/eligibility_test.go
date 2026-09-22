package pairing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

func TestAutomaticIPRecoveryEligible(t *testing.T) {
	tests := []struct {
		name   string
		result *gatewaypb.FleetNodePairResult
		want   bool
	}{
		{name: "paired with serial", result: &gatewaypb.FleetNodePairResult{Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, SerialNumber: " SN-1 "}, want: true},
		{name: "paired with MAC", result: &gatewaypb.FleetNodePairResult{Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED, MacAddress: "AA:BB:CC:DD:EE:FF"}, want: true},
		{name: "paired without stable identity", result: &gatewaypb.FleetNodePairResult{Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_PAIRED}, want: false},
		{name: "failed with identity", result: &gatewaypb.FleetNodePairResult{Outcome: gatewaypb.PairOutcome_PAIR_OUTCOME_AUTH_FAILED, SerialNumber: "SN-1"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, AutomaticIPRecoveryEligible(tc.result))
		})
	}
}
