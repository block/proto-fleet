package dto

import (
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
)

type CoolingModePayload struct {
	Mode commonpb.CoolingMode `json:"mode"`
}

type PowerTargetPayload struct {
	PerformanceMode pb.PerformanceMode `json:"performance_mode"`
}

type MiningPool struct {
	Priority        uint32 `json:"priority"`
	URL             string `json:"url"`
	Username        string `json:"username"`
	Password        string `json:"password,omitempty"`
	AppendMinerName bool   `json:"append_miner_name,omitempty"`
}

type UpdateMiningPoolsPayload struct {
	DefaultPool                             MiningPool  `json:"default_pool"`
	Backup1Pool                             *MiningPool `json:"backup1_pool,omitempty"`
	Backup2Pool                             *MiningPool `json:"backup2_pool,omitempty"`
	ReapplyCurrentPoolsWithStoredWorkerName bool        `json:"reapply_current_pools_with_stored_worker_name,omitempty"`
	DesiredWorkerName                       string      `json:"desired_worker_name,omitempty"`
	// ReleaseSV2Translation is an internal queue instruction honored only after
	// the miner has accepted this pool update.
	ReleaseSV2Translation bool `json:"release_sv2_translation,omitempty"`
}

type UpdateMinerPasswordPayload struct {
	NewPassword             string                `json:"new_password,omitempty"`
	CurrentPassword         string                `json:"current_password,omitempty"`
	EncryptedPasswordUpdate *NodeEncryptedPayload `json:"encrypted_password_update,omitempty"`
}

type NodeEncryptedPayload struct {
	Algorithm       string `json:"algorithm"`
	EphemeralPubkey []byte `json:"ephemeral_pubkey"`
	Nonce           []byte `json:"nonce"`
	Ciphertext      []byte `json:"ciphertext"`
}

// CurtailPayload carries the curtailment level for a Curtail dispatch.
type CurtailPayload struct {
	Level int32 `json:"level"`
}
