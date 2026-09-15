package main

import (
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

type controlCommandAdmissionClass = fleetnodecontrol.CommandAdmissionClass

const (
	controlCommandAdmissionGeneral        = fleetnodecontrol.CommandAdmissionGeneral
	controlCommandAdmissionDeferrableRead = fleetnodecontrol.CommandAdmissionDeferrableRead
	controlCommandAdmissionExclusive      = fleetnodecontrol.CommandAdmissionExclusive
)

func classifyControlCommand(command *pb.AgentCommand) controlCommandAdmissionClass {
	class, _ := fleetnodecontrol.AdmissionForCommand(command)
	return class
}
