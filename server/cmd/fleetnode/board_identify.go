package main

import (
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func sdkBoardIdentifyRequest(a *pb.SetBoardIdentifyAction) sdk.BoardIdentifyRequest {
	return sdk.BoardIdentifyRequest{
		Board: sdk.BoardIdentifyTarget{Tank: a.GetTank(), Module: a.GetModule(), Slot: a.GetSlot(), ModuleReportedID: a.GetModuleReportedId(), MACAddress: a.GetMacAddress()},
		Mode:  sdk.BoardIdentifyMode(a.GetMode()),
	}
}
