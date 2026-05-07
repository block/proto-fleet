package agentbootstrap

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1"
)

// ErrRegisterRejected wraps a server-side AlreadyExists or FailedPrecondition
// from Register. Callers can errors.Is to surface UX-specific recovery hints
// (revoke prior agent, choose a unique name, request a fresh enrollment code).
var ErrRegisterRejected = errors.New("server rejected register")

// RegisterParams is the input to Register. Name is required; callers that
// want to default it (CLI to os.Hostname(), web form to a chosen value) do
// that themselves.
type RegisterParams struct {
	ServerURL              string
	Name                   string
	Code                   string
	AllowInsecureTransport bool
}

// RegisterResult is the output of a successful Register: a partial State
// (no api_key, no session_token) for the caller to persist, and the
// fingerprint to surface for human verification against the operator UI.
type RegisterResult struct {
	State               *State
	IdentityFingerprint string
}

// Register validates the URL, generates ed25519 keypairs, calls
// AgentGatewayService.Register, verifies the server's returned fingerprint
// matches the local one, and returns the partial state. Callers must
// persist the returned State before continuing to CompleteEnrollment.
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
	mPub, mPriv, err := GenerateKeypair()
	if err != nil {
		return nil, err
	}

	client := NewGatewayClient(p.ServerURL)
	resp, err := client.Register(ctx, connect.NewRequest(&pb.RegisterRequest{
		EnrollmentToken:    p.Code,
		Name:               p.Name,
		IdentityPubkey:     idPub,
		MinerSigningPubkey: mPub,
	}))
	if err != nil {
		code := connect.CodeOf(err)
		if code == connect.CodeAlreadyExists || code == connect.CodeFailedPrecondition {
			return nil, fmt.Errorf("%w: %w", ErrRegisterRejected, err)
		}
		return nil, fmt.Errorf("register: %w", err)
	}

	localFP := IdentityFingerprint(idPub)
	if got := resp.Msg.GetIdentityFingerprint(); got != localFP {
		return nil, fmt.Errorf("server fingerprint %q does not match local %q", got, localFP)
	}

	state := &State{
		ServerURL:                 p.ServerURL,
		AllowInsecureTransport:    p.AllowInsecureTransport,
		AgentID:                   resp.Msg.GetAgentId(),
		IdentityFingerprint:       localFP,
		IdentityPrivateKeyHex:     hex.EncodeToString(idPriv),
		IdentityPublicKeyHex:      hex.EncodeToString(idPub),
		MinerSigningPrivateKeyHex: hex.EncodeToString(mPriv),
		MinerSigningPublicKeyHex:  hex.EncodeToString(mPub),
	}
	return &RegisterResult{State: state, IdentityFingerprint: localFP}, nil
}

// CompleteEnrollment runs the handshake against the api_key the operator
// pasted (after they verified the fingerprint in the operator UI),
// populating state.APIKey, state.SessionToken, and state.SessionExpiresAt
// in place on success. State is left untouched on failure: a tampered or
// stale state file's ServerURL is re-validated against the same
// https-or-loopback policy Register applies, and the supplied apiKey is
// only written back when the handshake actually completes.
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
	if err := RunHandshake(ctx, NewGatewayClient(state.ServerURL), &attempt); err != nil {
		return err
	}
	state.APIKey = attempt.APIKey
	state.SessionToken = attempt.SessionToken
	state.SessionExpiresAt = attempt.SessionExpiresAt
	return nil
}

// ValidateServerURL parses raw and rejects:
//   - schemes other than http or https
//   - empty hosts
//   - http schemes for non-loopback hosts unless allowInsecure is set
//
// loopback covers localhost, the full 127/8 block, and IPv6 ::1.
func ValidateServerURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse server-url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server-url scheme must be http or https; got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("server-url has no host")
	}
	if u.Scheme == "https" {
		return nil
	}
	if isLoopbackHost(u.Hostname()) {
		return nil
	}
	if allowInsecure {
		return nil
	}
	return fmt.Errorf("server-url must use https for non-loopback hosts; set AllowInsecureTransport to override (testing only)")
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}
