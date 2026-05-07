package agentbootstrap

import (
	"context"
	"errors"
)

// Refresh re-validates the stored URL and runs the handshake, populating
// state.SessionToken and state.SessionExpiresAt in place. Caller persists
// state after success.
//
// Refresh does not mutate state on failure; the caller may safely retry or
// surface the error without persisting. Auth failures (Unauthenticated)
// from BeginAuth are wrapped with ErrAPIKeyRejected so callers can
// distinguish revocation from other handshake failures.
func Refresh(ctx context.Context, state *State) error {
	if state == nil {
		return errors.New("state is required")
	}
	if state.ServerURL == "" {
		return errors.New("state has no server_url")
	}
	if state.APIKey == "" {
		return errors.New("state has no api_key")
	}
	if err := ValidateServerURL(state.ServerURL, state.AllowInsecureTransport); err != nil {
		return err
	}
	return RunHandshake(ctx, NewGatewayClient(state.ServerURL), state)
}
