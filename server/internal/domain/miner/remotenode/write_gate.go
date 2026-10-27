package remotenode

import (
	"context"

	sdk "github.com/block/proto-fleet/server/sdk/v1"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
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
			outcome := ackToError(ack)
			// Failed acknowledgements may describe a plugin RPC whose backend
			// still runs; only explicit pre-dispatch refusals prove no work began.
			if outcome != nil && !devicewrite.IsNotDispatched(outcome) {
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
func physicalWriteKind(command *gatewaypb.MinerCommand) (commandtype.Type, bool) {
	switch command.GetAction().(type) {
	case *gatewaypb.MinerCommand_MiningGroups:
		r, err := sdk.DecodeMiningGroupRequest(command.GetMiningGroups().Request)
		return commandtype.Type(-1), err != nil || !r.ReadOnly()
	case *gatewaypb.MinerCommand_GetCoolingMode, *gatewaypb.MinerCommand_GetErrors, *gatewaypb.MinerCommand_GetMiningPools, *gatewaypb.MinerCommand_GetFirmwareUpdateStatus, *gatewaypb.MinerCommand_DownloadLogs, *gatewaypb.MinerCommand_BlinkLed:
		return commandtype.Type(-1), false
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
	default:
		return commandtype.Type(-1), true
	}
}
