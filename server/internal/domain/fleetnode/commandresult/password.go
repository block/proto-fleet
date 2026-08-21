package commandresult

import (
	"errors"
	"fmt"

	"buf.build/go/protovalidate"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

// ValidateUpdateMinerPassword validates the typed ACK payload returned after a
// fleet-node password update.
func ValidateUpdateMinerPassword(result *gatewaypb.UpdateMinerPasswordResult) error {
	if err := protovalidate.Validate(result); err != nil {
		return fmt.Errorf("invalid update miner password result: %w", err)
	}
	creds := result.GetEncryptedCredentials()
	if creds == nil || len(creds.GetUsername()) == 0 || len(creds.GetPassword()) == 0 {
		return errors.New("invalid update miner password result: encrypted credentials are required")
	}
	return nil
}
