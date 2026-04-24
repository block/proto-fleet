package dto

import (
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	poolspb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
)

type CoolingModePayload struct {
	Mode commonpb.CoolingMode `json:"mode"`
}

type PowerTargetPayload struct {
	PerformanceMode pb.PerformanceMode `json:"performance_mode"`
}

type MiningPool struct {
	Priority        uint32               `json:"priority"`
	URL             string               `json:"url"`
	Username        string               `json:"username"`
	Password        string               `json:"password,omitempty"`
	AppendMinerName bool                 `json:"append_miner_name,omitempty"`
	// Protocol the URL speaks. Unspecified is treated as SV1. Emitted
	// unconditionally (no omitempty) so SV1 payloads round-trip through
	// the queue as an explicit POOL_PROTOCOL_UNSPECIFIED rather than
	// relying on json.Unmarshal to default the zero value.
	Protocol poolspb.PoolProtocol `json:"protocol"`
}

type UpdateMiningPoolsPayload struct {
	DefaultPool                             MiningPool  `json:"default_pool"`
	Backup1Pool                             *MiningPool `json:"backup1_pool,omitempty"`
	Backup2Pool                             *MiningPool `json:"backup2_pool,omitempty"`
	ReapplyCurrentPoolsWithStoredWorkerName bool        `json:"reapply_current_pools_with_stored_worker_name,omitempty"`
	DesiredWorkerName                       string      `json:"desired_worker_name,omitempty"`
}

type UpdateMinerPasswordPayload struct {
	NewPassword     string `json:"new_password"`
	CurrentPassword string `json:"current_password"`
}
