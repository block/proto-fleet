package ct1

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxControllerTokenBytes matches Frigate's mdk_auth token file limit.
const maxControllerTokenBytes = 4096

// controllerToken returns the bearer token Fleet stored for this controller.
// Fleet stores it as a credential with an empty username. A credential with a
// username is a saved REST login and is never sent as RPC metadata.
func controllerToken(credentials sdk.UsernamePassword) (string, error) {
	if credentials.Username != "" || credentials.Password == "" {
		return "", nil
	}
	token := credentials.Password
	if len(token) > maxControllerTokenBytes || !validBearerToken(token) {
		// Never echo the value: it is a credential even when malformed.
		return "", errors.New("CT1 controller token must be one bearer token of at most 4096 characters")
	}
	return token, nil
}

// validBearerToken accepts RFC 6750 b64token syntax, as Frigate does. It is
// also safe as a gRPC metadata value.
func validBearerToken(token string) bool {
	padding, base := false, false
	for i := range len(token) {
		switch c := token[i]; {
		case c == '=':
			padding = true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~', c == '+', c == '/':
			if padding {
				return false
			}
			base = true
		default:
			return false
		}
	}
	return base
}

// bearerCredential attaches the controller token to every unary and streaming
// RPC on one controller connection.
type bearerCredential struct{ header string }

func newBearerCredential(token string) bearerCredential {
	return bearerCredential{header: "Bearer " + token}
}

func (b bearerCredential) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": b.header}, nil
}

// RequireTransportSecurity is false because Frigate serves MDK over plaintext
// by default; operators protect the link with TLS or a private network.
func (bearerCredential) RequireTransportSecurity() bool { return false }

func (bearerCredential) String() string   { return "bearerCredential(*****)" }
func (bearerCredential) GoString() string { return "bearerCredential(*****)" }

// authFailure reports a controller's rejection of Fleet's credential, or its
// demand for one, as an authentication failure. Fleet then shows the device as
// needing authentication instead of offline. The gRPC code stays reachable
// through Unwrap for callers that inspect it.
func authFailure(deviceID string, err error) error {
	if err == nil || status.Code(err) != codes.Unauthenticated {
		return err
	}
	var sdkErr sdk.SDKError
	if errors.As(err, &sdkErr) {
		return err
	}
	return sdk.NewErrorAuthenticationFailed(deviceID, fmt.Errorf("CT1 controller rejected Fleet's credential: %w", err))
}

// ErrIdentityRequiresToken reports a controller that refused Fleet's
// unauthenticated identity read. Discovery sends no token, so it cannot verify
// which controller answered.
var ErrIdentityRequiresToken = errors.New("CT1 controller requires a token to read its identity")

// UnidentifiedController lists a controller that requires a token without
// claiming an identity. Pairing with the token verifies the controller ID and
// HA membership before Fleet trusts it.
func UnidentifiedController(info sdk.DeviceInfo) sdk.DeviceInfo {
	info.DeviceClass = sdk.DeviceClassContainer
	info.SerialNumber = ""
	info.Model = "CT1"
	info.Manufacturer = "Proto"
	info.FirmwareVersion = ""
	info.Endpoints = nil
	return info
}

// ErrControllerIdentityMismatch reports that the controller answering at a saved
// address is not the one the caller expected.
var ErrControllerIdentityMismatch = errors.New("CT1 controller identity mismatch")

// identityMismatchGuidance turns a mismatch found while opening a device into a
// failed precondition, so pairing tells the operator how to recover instead of
// reporting an internal error. Other errors pass through unchanged.
func identityMismatchGuidance(err error) error {
	if !errors.Is(err, ErrControllerIdentityMismatch) {
		return err
	}
	return status.Errorf(codes.FailedPrecondition, "%v: a different controller now answers at this address; remove the existing device from Fleet, then discover and pair this controller", err) //nolint:wrapcheck // The gRPC status is the plugin boundary error.
}
