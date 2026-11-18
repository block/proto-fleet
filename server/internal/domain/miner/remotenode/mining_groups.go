package remotenode

import (
	"context"
	"encoding/json"
	"slices"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/poolconfig"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func miningGroupCommandVersion(request sdk.MiningGroupRequest) gatewaypb.CommandProtocolVersion {
	// A fresh nonempty group needs zero pool IDs in its next step. Check the
	// version at dispatch so an older node cannot leave an empty group behind.
	if len(request.PoolIDs) != 0 || (request.Action == "create" && !poolconfigurations.IsClearedPoolIntent(request.ConfigID)) {
		return gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V18
	}
	if request.Action == sdk.MiningGroupReadDefault || request.Action == "stop_legacy" || request.Action == "activate" ||
		(request.Action == "start" && request.GroupID == "default") {
		return gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V18
	}
	if request.Action == "deactivate" {
		return gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15
	}
	return gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V6
}

// CheckMiningGroupDeactivation checks the live node handshake without sending a
// command. Admission calls it before committing a durable mode-change workflow.
func (m *Miner) CheckMiningGroupDeactivation(context.Context) error {
	checker, ok := m.sender.(interface {
		SupportsCommandProtocolVersion(fleetNodeID int64, minimum gatewaypb.CommandProtocolVersion) bool
	})
	if !ok || !checker.SupportsCommandProtocolVersion(m.fleetNodeID, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V15) {
		return fleeterror.NewFailedPreconditionError("Upgrade the Fleet Node and reconnect it before disabling mining groups.")
	}
	return nil
}

func (m *Miner) MiningGroups(ctx context.Context, r sdk.MiningGroupRequest) (sdk.MiningGroupResult, error) {
	var result sdk.MiningGroupResult
	if err := r.Validate(); err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	action := &gatewaypb.MiningGroupsAction{}
	r.Pools = slices.Clone(r.Pools)
	if r.Action == "replace_pools" {
		if m.encryptionPublicKey == nil {
			return result, devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError("fleet node encryption key unavailable"))
		}
		key, err := m.encryptionPublicKey(ctx)
		if err != nil {
			return result, devicewrite.NotDispatched(err)
		}
		config := &gatewaypb.MiningPoolWriteConfiguration{}
		for i, p := range r.Pools {
			password := p.Password
			config.Pools = append(config.Pools, &gatewaypb.MiningPoolWriteConfig{Priority: []int32{0, 1, 2}[i], Url: p.URL, Username: p.Username, Password: &password})
			r.Pools[i].Password = ""
		}
		action.EncryptedPools, err = poolconfig.Encrypt(key, m.desc.GetDeviceIdentifier(), config)
		if err != nil {
			return result, devicewrite.NotDispatched(err)
		}
	}
	var err error
	action.Request, err = json.Marshal(r)
	if err != nil {
		return result, devicewrite.NotDispatched(err)
	}
	ack, err := m.send(ctx, &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_MiningGroups{MiningGroups: action}})
	if err != nil {
		return result, err
	}
	if err := ackToError(ack); err != nil {
		return result, err
	}
	return sdk.DecodeMiningGroupResult(ack.GetPayload(), r)
}
