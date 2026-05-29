package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

var handshakeStepTimeout = 30 * time.Second

// ErrBeginAuthRejected wraps Unauthenticated from BeginAuthHandshake, which
// the server returns for revoked api_key, identity_pubkey mismatch, or any
// auth failure on that call. Kept distinct from CompleteAuthHandshake errors
// (expired challenge, bad signature) so callers can branch on root cause.
var ErrBeginAuthRejected = errors.New("BeginAuthHandshake rejected")

// RunHandshake mutates s.SessionToken / s.SessionExpiresAt only on success.
func RunHandshake(ctx context.Context, c fleetnodegatewayv1connect.FleetNodeGatewayServiceClient, s *State) error {
	if s == nil {
		return errors.New("state is required")
	}
	priv, err := hex.DecodeString(s.IdentityPrivateKeyHex)
	if err != nil {
		return fmt.Errorf("decode identity private key: %w", err)
	}
	pub, err := hex.DecodeString(s.IdentityPublicKeyHex)
	if err != nil {
		return fmt.Errorf("decode identity public key: %w", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		return errors.New("identity private key has wrong length")
	}
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("identity public key has wrong length")
	}
	if c == nil {
		return errors.New("client is required")
	}

	beginCtx, cancel := withHandshakeTimeout(ctx)
	begin, err := c.BeginAuthHandshake(beginCtx, connect.NewRequest(&pb.BeginAuthHandshakeRequest{
		ApiKey:         s.APIKey,
		IdentityPubkey: pub,
	}))
	cancel()
	if err != nil {
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			return fmt.Errorf("%w: %w", ErrBeginAuthRejected, err)
		}
		return fmt.Errorf("begin handshake: %w", err)
	}
	challenge := begin.Msg.GetChallenge()
	signature := ed25519.Sign(ed25519.PrivateKey(priv), challenge)

	completeCtx, cancel := withHandshakeTimeout(ctx)
	complete, err := c.CompleteAuthHandshake(completeCtx, connect.NewRequest(&pb.CompleteAuthHandshakeRequest{
		Challenge: challenge,
		Signature: signature,
	}))
	cancel()
	if err != nil {
		return fmt.Errorf("complete handshake: %w", err)
	}

	s.SessionToken = complete.Msg.GetSessionToken()
	if exp := complete.Msg.GetExpiresAt(); exp != nil {
		s.SessionExpiresAt = exp.AsTime()
	}
	return nil
}

func withHandshakeTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, handshakeStepTimeout)
}
