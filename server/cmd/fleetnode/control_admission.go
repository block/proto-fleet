package main

import (
	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

type controlCommandAdmissionClass = fleetnodecontrol.CommandAdmissionClass

const (
	controlCommandAdmissionGeneral        = fleetnodecontrol.CommandAdmissionGeneral
	controlCommandAdmissionDeferrableRead = fleetnodecontrol.CommandAdmissionDeferrableRead
	controlCommandAdmissionExclusive      = fleetnodecontrol.CommandAdmissionExclusive
)
