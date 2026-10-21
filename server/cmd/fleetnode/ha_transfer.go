package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"buf.build/go/protovalidate"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/keytransfer"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	"google.golang.org/protobuf/proto"
)

func (r *RunCmd) handleHAKeyTransfer(sender acker, command *pb.ControlCommand, transfer *pb.HAKeyTransfer, authority *haAuthority, logger *slog.Logger) bool {
	if command.GetCommandId() == "" || len(command.GetCommandId()) > 128 {
		return false
	}
	if err := protovalidate.Validate(command); err != nil {
		r.sendAck(sender, command.GetCommandId(), pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid key transfer command", logger)
		return false
	}
	result, err := r.executeHAKeyTransfer(command, transfer, authority, time.Now())
	if err != nil {
		r.sendAck(sender, command.GetCommandId(), pb.AckCode_ACK_CODE_HA_NOT_AUTHORIZED, err.Error(), logger)
		return false
	}
	payload, err := proto.Marshal(result)
	if err != nil {
		r.sendAck(sender, command.GetCommandId(), pb.AckCode_ACK_CODE_INTERNAL, "encode key transfer result", logger)
		return false
	}
	r.sendAckWithPayload(sender, command.GetCommandId(), pb.AckCode_ACK_CODE_OK, "", payload, logger)
	return transfer.GetEncryptedKey() != nil
}

func (r *RunCmd) executeHAKeyTransfer(command *pb.ControlCommand, transfer *pb.HAKeyTransfer, authority *haAuthority, now time.Time) (*pb.HAKeyTransferResult, error) {
	if err := protovalidate.Validate(transfer); err != nil {
		return nil, fmt.Errorf("invalid key transfer metadata")
	}
	scope, ok := authority.transferScope(now)
	if !ok || command.GetIntendedInstanceId() != authority.instanceID || command.GetControlSessionId() != authority.sessionID || transfer.GetOrgId() != scope.GetOrgId() || (len(authority.assignment.GetCredentialKeyFingerprint()) > 0 && !bytes.Equal(transfer.GetCredentialKeyFingerprint(), authority.assignment.GetCredentialKeyFingerprint())) {
		return nil, fmt.Errorf("key transfer is outside this control session")
	}
	if r.credentials == nil || r.haState == nil || r.haStatePath == "" {
		return nil, fmt.Errorf("credential state is unavailable")
	}
	owner := scope.GetOwnerNodeId()
	if owner == 0 {
		owner = authority.nodeID
	}
	if transfer.GetOwnerNodeId() != owner || (command.GetHaOwnerNodeId() != 0 && command.GetHaOwnerNodeId() != owner) {
		return nil, fmt.Errorf("key transfer owner does not match current assignment")
	}
	metadata := keytransfer.Metadata{OperationID: transfer.GetOperationId(), OrgID: transfer.GetOrgId(), OwnerNodeID: owner, SourceNodeID: transfer.GetSourceNodeId(), RecipientNodeID: transfer.GetRecipientNodeId(), RecipientPublicKey: transfer.GetRecipientPublicKey(), Fingerprint: transfer.GetCredentialKeyFingerprint(), ExpiresAt: transfer.GetExpiresAt().AsTime().UnixNano()}
	if err := metadata.Validate(now); err != nil {
		return nil, err
	}
	if transfer.GetEncryptedKey() == nil {
		if transfer.GetSourceNodeId() != authority.nodeID || scope.GetPreparing() {
			return nil, fmt.Errorf("credential export requires the ready source")
		}
		encrypted, err := keytransfer.Encrypt(metadata, r.credentials.keySnapshot(), now)
		if err != nil {
			return nil, err
		}
		return &pb.HAKeyTransferResult{EncryptedKey: encrypted, CredentialKeyFingerprint: append([]byte(nil), metadata.Fingerprint...)}, nil
	}
	if !scope.GetPreparing() || transfer.GetRecipientNodeId() != authority.nodeID {
		return nil, fmt.Errorf("credential import requires the preparing recipient")
	}
	if r.runtimeStarted {
		return nil, fmt.Errorf("credential import requires startup preparation")
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	publicKey, err := hex.DecodeString(r.haState.EncryptionPublicKeyHex)
	if err != nil || !bytes.Equal(publicKey, metadata.RecipientPublicKey) {
		return nil, fmt.Errorf("key transfer recipient public key does not match enrollment")
	}
	privateKey, err := hex.DecodeString(r.haState.EncryptionPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient encryption key")
	}
	key, err := keytransfer.Decrypt(privateKey, transfer.GetEncryptedKey(), metadata, now)
	if err != nil {
		return nil, err
	}
	if r.haState.HACredentialOwnerNodeID != 0 && (r.haState.HACredentialOwnerNodeID != owner || r.haState.CredentialKeyHex != hex.EncodeToString(key)) {
		return nil, fmt.Errorf("credential key was already installed for another owner or key")
	}
	next := *r.haState
	next.CredentialKeyHex = hex.EncodeToString(key)
	next.HACredentialOwnerNodeID = owner
	if err := bootstrap.SaveState(r.haStatePath, &next); err != nil {
		return nil, fmt.Errorf("persist transferred credential key: %w", err)
	}
	r.haState.CredentialKeyHex = next.CredentialKeyHex
	r.haState.HACredentialOwnerNodeID = next.HACredentialOwnerNodeID
	return &pb.HAKeyTransferResult{CredentialKeyFingerprint: append([]byte(nil), metadata.Fingerprint...)}, nil
}
