package main

import (
	"encoding/hex"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialkey"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

func (r *RunCmd) handleExportCredentialKey(stream acker, commandID string, req *pb.ExportCredentialKey, logger *slog.Logger) {
	if r.credentials == nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no credential key configured", logger)
		return
	}
	key := r.credentials.currentKey()
	sealed, err := credentialkey.Seal(req.GetRecipientPublicKey(), key)
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, err.Error(), logger)
		return
	}
	r.sendCredentialKeyResult(stream, commandID, &pb.CredentialKeyResult{SealedKey: sealed, Fingerprint: credentialkey.Fingerprint(key)}, logger)
}

func (r *RunCmd) handleInstallCredentialKey(stream acker, commandID string, req *pb.InstallCredentialKey, logger *slog.Logger) {
	if r.credentials == nil || len(r.passwordUpdatePrivateKey) == 0 {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "fleet node has no credential key configured", logger)
		return
	}
	key, err := credentialkey.Open(r.passwordUpdatePrivateKey, req.GetSealedKey())
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, err.Error(), logger)
		return
	}
	if err := r.installCredentialKey(key); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, err.Error(), logger)
		return
	}
	logger.Info("installed HA credential key from active fleet node")
	r.sendCredentialKeyResult(stream, commandID, &pb.CredentialKeyResult{Fingerprint: credentialkey.Fingerprint(key)}, logger)
}

func (r *RunCmd) installCredentialKey(key []byte) error {
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	r.stateMu.Lock()
	previous := r.state.CredentialKeyHex
	r.state.CredentialKeyHex = hex.EncodeToString(key)
	snapshot := *r.state
	r.stateMu.Unlock()
	if err := bootstrap.SaveState(r.statePath, &snapshot); err != nil {
		r.stateMu.Lock()
		r.state.CredentialKeyHex = previous
		r.stateMu.Unlock()
		return fmt.Errorf("persist credential key: %w", err)
	}
	r.credentials.replaceKey(key)
	return nil
}

func (r *RunCmd) sendCredentialKeyResult(stream acker, commandID string, result *pb.CredentialKeyResult, logger *slog.Logger) {
	payload, err := proto.Marshal(result)
	if err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_INTERNAL, fmt.Sprintf("marshal credential key result: %v", err), logger)
		return
	}
	r.sendAckWithPayload(stream, commandID, pb.AckCode_ACK_CODE_OK, "", payload, logger)
}
