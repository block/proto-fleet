package pairing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestPairOnNodeRejectsOversizedBatchBeforeDispatch(t *testing.T) {
	targets := make([]*pairingpb.FleetNodePairTarget, MaxPairBatch+1)
	for i := range targets {
		targets[i] = &pairingpb.FleetNodePairTarget{DeviceIdentifier: "mac:device"}
	}

	err := (&Service{}).PairOnNode(t.Context(), 1, targets, nil, 1, nil, nil)

	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
