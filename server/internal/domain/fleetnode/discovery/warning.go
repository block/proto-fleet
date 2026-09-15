package discovery

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// SourceWarning keeps internal diagnostics out of successful discovery frames.
// Callers log the original error and choose a source label their caller may see.
func SourceWarning(source string, err error) *pairingpb.DiscoverResponse {
	code, message := connect.CodeUnknown, "discovery failed"
	var fleetErr fleeterror.FleetError
	var connectErr *connect.Error
	if errors.As(err, &fleetErr) {
		code, message = fleetErr.GRPCCode, fleetErr.DebugMessage
	} else if errors.As(err, &connectErr) {
		code, message = connectErr.Code(), connectErr.Message()
	}
	if code == connect.CodeInternal || code == connect.CodeUnknown {
		message = "discovery failed"
	}
	return &pairingpb.DiscoverResponse{Warning: fmt.Sprintf("%s: %s", source, message)}
}
