package dto

import (
	commonpb "github.com/btc-mining/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
)

type CoolingModePayload struct {
	Mode commonpb.CoolingMode `json:"mode"`
}

type PowerTargetPayload struct {
	PerformanceMode pb.PerformanceMode `json:"performance_mode"`
}

type MiningPool struct {
	Priority uint32 `json:"priority"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

type UpdateMiningPoolsPayload struct {
	DefaultPool MiningPool  `json:"default_pool"`
	Backup1Pool *MiningPool `json:"backup1_pool,omitempty"`
	Backup2Pool *MiningPool `json:"backup2_pool,omitempty"`
}

type UpdateMinerPasswordPayload struct {
	NewPassword     string `json:"new_password"`
	CurrentPassword string `json:"current_password"`
}
