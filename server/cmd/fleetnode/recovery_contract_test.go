package main

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestRecoverMinerEndpointsRequestValidation(t *testing.T) {
	validTarget := func(id string) *pb.MinerConnectionDescriptor {
		return &pb.MinerConnectionDescriptor{
			DeviceIdentifier: id,
			DriverName:       "antminer",
			IpAddress:        "192.168.1.10",
			Port:             "80",
		}
	}

	require.NoError(t, protovalidate.Validate(&pb.RecoverMinerEndpointsRequest{
		Targets:   []*pb.MinerConnectionDescriptor{validTarget("miner-1")},
		ScanPorts: []string{"80"},
	}))
	require.Error(t, protovalidate.Validate(&pb.RecoverMinerEndpointsRequest{}))

	tooMany := make([]*pb.MinerConnectionDescriptor, 513)
	for i := range tooMany {
		tooMany[i] = validTarget("miner")
	}
	require.Error(t, protovalidate.Validate(&pb.RecoverMinerEndpointsRequest{Targets: tooMany, ScanPorts: []string{"80"}}))

	require.Error(t, protovalidate.Validate(&pb.RecoverMinerEndpointsRequest{
		Targets:   []*pb.MinerConnectionDescriptor{validTarget("miner-1")},
		ScanPorts: []string{"81"},
	}))
	require.Error(t, protovalidate.Validate(&pb.RecoverMinerEndpointsRequest{
		Targets:   []*pb.MinerConnectionDescriptor{validTarget("miner-1")},
		ScanPorts: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"},
	}))
}

func TestRecoverMinerEndpointsResultValidation(t *testing.T) {
	require.NoError(t, protovalidate.Validate(&pb.RecoverMinerEndpointsResult{
		Results: []*pb.MinerEndpointRecoveryResult{{
			DeviceIdentifier: "miner-1",
			Outcome:          pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND,
			IpAddress:        "192.168.1.20",
			Port:             "80",
		}},
	}))
	require.Error(t, protovalidate.Validate(&pb.RecoverMinerEndpointsResult{
		Results: []*pb.MinerEndpointRecoveryResult{{
			DeviceIdentifier: "miner-1",
			Outcome:          pb.MinerEndpointRecoveryOutcome(99),
		}},
	}))
}

func TestRecoverMinerEndpointsResultAcceptsUnspecifiedOutcome(t *testing.T) {
	require.NoError(t, protovalidate.Validate(&pb.RecoverMinerEndpointsResult{
		Results: []*pb.MinerEndpointRecoveryResult{{
			DeviceIdentifier: "miner-1",
			Outcome:          pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_UNSPECIFIED,
		}},
	}))
}
