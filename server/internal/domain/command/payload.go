package command

import pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"

type CoolingModePayload struct {
	Mode pb.CoolingMode `json:"mode"`
}
