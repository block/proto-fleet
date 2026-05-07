package agentbootstrap

import (
	"context"
	"errors"
)

// Mutates SessionToken/SessionExpiresAt only on success. BeginAuth
// Unauthenticated wraps ErrBeginAuthRejected so callers can distinguish
// it from CompleteAuth failures (expired challenge, bad signature).
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
