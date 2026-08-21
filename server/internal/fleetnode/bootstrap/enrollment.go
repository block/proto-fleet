package bootstrap

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/passwordupdate"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

// Wraps server AlreadyExists / FailedPrecondition / Unauthenticated. Common
// causes: name already registered, enrollment code typoed / used / expired.
var ErrRegisterRejected = errors.New("server rejected register")

// Name is required; the library does not default it (CLI defaults to
// os.Hostname(), a web form picks its own).
type RegisterParams struct {
	ServerURL              string
	Name                   string
	Code                   string
	AllowInsecureTransport bool
}

// State is partial: keys + fleet_node_id + fingerprint, no api_key or session.
// Caller persists, surfaces State.IdentityFingerprint for human verification,
// then calls CompleteEnrollment.
type RegisterResult struct {
	State *State
}

// Callers MUST persist the returned State before calling CompleteEnrollment
// so a Ctrl-C between them is recoverable.
func Register(ctx context.Context, p RegisterParams) (*RegisterResult, error) {
	if err := ValidateServerURL(p.ServerURL, p.AllowInsecureTransport); err != nil {
		return nil, err
	}
	if p.Name == "" {
		return nil, errors.New("Name is required")
	}
	if p.Code == "" {
		return nil, errors.New("Code is required")
	}

	idPub, idPriv, err := GenerateKeypair()
	if err != nil {
		return nil, err
	}
	encPub, encPriv, err := passwordupdate.GenerateKeypair()
	if err != nil {
		return nil, err
	}

	client, err := NewGatewayClient(p.ServerURL)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := withHandshakeTimeout(ctx)
	resp, err := client.Register(callCtx, connect.NewRequest(&pb.RegisterRequest{
		EnrollmentToken:  p.Code,
		Name:             p.Name,
		IdentityPubkey:   idPub,
		EncryptionPubkey: encPub,
	}))
	cancel()
	if err != nil {
		code := connect.CodeOf(err)
		if code == connect.CodeAlreadyExists || code == connect.CodeFailedPrecondition || code == connect.CodeUnauthenticated {
			return nil, fmt.Errorf("%w: %w", ErrRegisterRejected, err)
		}
		return nil, fmt.Errorf("register: %w", err)
	}

	localFP := IdentityFingerprint(idPub)
	if got := resp.Msg.GetIdentityFingerprint(); got != localFP {
		return nil, fmt.Errorf("server fingerprint %q does not match local %q", got, localFP)
	}

	state := &State{
		ServerURL:               p.ServerURL,
		AllowInsecureTransport:  p.AllowInsecureTransport,
		FleetNodeID:             resp.Msg.GetFleetNodeId(),
		IdentityFingerprint:     localFP,
		IdentityPrivateKeyHex:   hex.EncodeToString(idPriv),
		IdentityPublicKeyHex:    hex.EncodeToString(idPub),
		EncryptionPrivateKeyHex: hex.EncodeToString(encPriv),
		EncryptionPublicKeyHex:  hex.EncodeToString(encPub),
	}
	return &RegisterResult{State: state}, nil
}

// Re-validates state.ServerURL so a tampered file can't bypass the
// https-or-loopback policy. State is unchanged on failure; retry with a
// different apiKey.
func CompleteEnrollment(ctx context.Context, state *State, apiKey string) error {
	if state == nil {
		return errors.New("state is required")
	}
	if apiKey == "" {
		return errors.New("apiKey is required")
	}
	if state.ServerURL == "" {
		return errors.New("state has no server_url")
	}
	if err := ValidateServerURL(state.ServerURL, state.AllowInsecureTransport); err != nil {
		return err
	}

	attempt := *state
	attempt.APIKey = apiKey
	client, err := NewGatewayClient(state.ServerURL)
	if err != nil {
		return err
	}
	if err := RunHandshake(ctx, client, &attempt); err != nil {
		return err
	}
	state.APIKey = attempt.APIKey
	state.SessionToken = attempt.SessionToken
	state.SessionExpiresAt = attempt.SessionExpiresAt
	return nil
}

// Requires https unless the host is loopback (localhost, 127/8, ::1) or
// allowInsecure is set.
func ValidateServerURL(raw string, allowInsecure bool) error {
	return transportguard.ValidateServerURL(raw, allowInsecure)
}
