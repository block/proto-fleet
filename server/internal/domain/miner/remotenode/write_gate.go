package remotenode

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

// withPhysicalWriteAdmission classifies terminal write acknowledgements while the
// admission is active. Read callers retain their own payload and partial-result handling.
func (m *Miner) withPhysicalWriteAdmission(ctx context.Context, command *gatewaypb.MinerCommand, send func(context.Context) (*gatewaypb.ControlAck, error)) error {
	kind, physical := physicalWriteKind(command)
	invoked := false
	invoke := func(ctx context.Context) error {
		invoked = true
		ack, err := send(ctx)
		if err != nil {
			err = mapSendCommandError(err)
			if physical && !devicewrite.IsNotDispatched(err) {
				return devicewrite.UncertainDelivery(err)
			}
			return err
		}
		if physical {
			evidence := ack.GetWriteOutcome()
			outcome := sdk.WithWriteOutcome(ackToError(ack), evidence.GetOutcome(), sdk.WriteStage(evidence.GetStage()), sdk.WriteReason(evidence.GetReason()))
			// Failed acknowledgements may describe a plugin RPC whose backend
			// still runs; only explicit pre-dispatch refusals prove no work began.
			if outcome != nil && sdk.WriteEvidence(outcome) == nil && !devicewrite.IsNotDispatched(outcome) {
				return devicewrite.UncertainDelivery(outcome)
			}
			return outcome
		}
		return nil
	}
	if !physical || m.writeGate == nil {
		return invoke(ctx)
	}
	err := m.writeGate(ctx, kind, invoke)
	if err != nil && !invoked {
		return devicewrite.NotDispatched(err)
	}
	return err
}

// Only explicitly classified reads and indicator control bypass physical-write admission.
// Future actions remain fenced until their behavior has been classified.
// control.MinerCommandReadOnly holds that classification, which the node's
// command cancel follows too.
func physicalWriteKind(command *gatewaypb.MinerCommand) (commandtype.Type, bool) {
	write := !control.MinerCommandReadOnly(command)
	switch command.GetAction().(type) {
	case *gatewaypb.MinerCommand_ContainerMining:
		return commandtype.ContainerMining, write
	case *gatewaypb.MinerCommand_ContainerPowerFeed:
		return commandtype.ControlPowerFeed, write
	case *gatewaypb.MinerCommand_Reboot:
		return commandtype.Reboot, true
	case *gatewaypb.MinerCommand_StartMining:
		return commandtype.StartMining, true
	case *gatewaypb.MinerCommand_StopMining:
		return commandtype.StopMining, true
	case *gatewaypb.MinerCommand_Curtail:
		return commandtype.Curtail, true
	case *gatewaypb.MinerCommand_Uncurtail:
		return commandtype.Uncurtail, true
	case *gatewaypb.MinerCommand_SetCoolingMode:
		return commandtype.SetCoolingMode, true
	case *gatewaypb.MinerCommand_SetPowerTarget:
		return commandtype.SetPowerTarget, true
	case *gatewaypb.MinerCommand_UpdateMiningPools:
		return commandtype.UpdateMiningPools, true
	case *gatewaypb.MinerCommand_UpdateMinerPassword:
		return commandtype.UpdateMinerPassword, true
	case *gatewaypb.MinerCommand_FirmwareUpdate:
		return commandtype.FirmwareUpdate, true
	case *gatewaypb.MinerCommand_ApplyCurtailmentConfig:
		return commandtype.ApplyCurtailmentConfig, true
	case *gatewaypb.MinerCommand_SetTankMining:
		return commandtype.SetTankMining, true
	case *gatewaypb.MinerCommand_SetModuleMining:
		return commandtype.SetModuleMining, true
	case *gatewaypb.MinerCommand_SetContainerControl:
		return commandtype.SetContainerControl, true
	case *gatewaypb.MinerCommand_RunContainerAlarmAction:
		return commandtype.ContainerAlarmAction, true
	case *gatewaypb.MinerCommand_FrigateUpdate:
		// The same kind as a direct controller. A support or status call only reads.
		return commandtype.FrigateServerUpdate, write
	default:
		return commandtype.Type(-1), write
	}
}
