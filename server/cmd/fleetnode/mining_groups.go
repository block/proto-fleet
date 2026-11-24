package main

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/poolconfig"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type miningGroupsCommand struct{ request sdk.MiningGroupRequest }

func newMiningGroupsCommand(privateKey []byte, target *pb.MinerConnectionDescriptor, mc *pb.MinerCommand) (*miningGroupsCommand, error) {
	a := mc.GetMiningGroups()
	if a == nil {
		return nil, nil
	}
	r, err := sdk.DecodeMiningGroupRequest(a.Request)
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid mining group request")
	}
	for _, p := range r.Pools {
		if p.Password != "" {
			return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "pool credentials must be encrypted")
		}
	}
	if r.Action == "replace_pools" {
		config, err := poolconfig.Decrypt(privateKey, a.EncryptedPools, target.GetDeviceIdentifier())
		if err != nil {
			return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid encrypted pool configuration")
		}
		if len(config.Pools) != len(r.Pools) {
			return nil, fmt.Errorf("pool configuration mismatch")
		}
		for i, p := range config.Pools {
			if p.Url != r.Pools[i].URL || p.Username != r.Pools[i].Username || int64(p.Priority) != int64(i) {
				return nil, fmt.Errorf("pool configuration mismatch")
			}
			r.Pools[i].Password = p.GetPassword()
		}
	} else if a.EncryptedPools != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "unexpected pool configuration")
	}
	if err := r.Validate(); err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid mining group request")
	}
	return &miningGroupsCommand{request: r}, nil
}
func (c *miningGroupsCommand) run(ctx context.Context, dev sdk.Device) ([]byte, error) {
	controller, ok := dev.(sdk.DeviceMiningGroups)
	if !ok {
		return nil, sdk.NewErrUnsupportedCapability(sdk.CapabilityMiningGroups)
	}
	result, err := controller.MiningGroups(ctx, c.request)
	if err != nil {
		return nil, err
	}
	if err := result.Validate(c.request); err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode mining group result: %w", err)
	}
	return data, nil
}
