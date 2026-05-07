package agentbootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
)

// ErrAPIKeyRejected wraps the connect.CodeUnauthenticated response from
// BeginAuthHandshake. Callers can errors.Is to distinguish api_key rejection
// from other handshake failures (challenge expiry, signature mismatch).
var ErrAPIKeyRejected = errors.New("server rejected api_key")

// RunHandshake runs the BeginAuth/CompleteAuth pair against the supplied
// client, signing the server's challenge with the state's identity private
// key. On success it populates state.SessionToken and state.SessionExpiresAt
// in place; on failure state is unchanged.
func RunHandshake(ctx context.Context, c agentgatewayv1connect.AgentGatewayServiceClient, s *State) error {
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

	begin, err := c.BeginAuthHandshake(ctx, connect.NewRequest(&pb.BeginAuthHandshakeRequest{
		ApiKey:         s.APIKey,
		IdentityPubkey: pub,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			return fmt.Errorf("%w: %w", ErrAPIKeyRejected, err)
		}
		return fmt.Errorf("begin handshake: %w", err)
	}
	challenge := begin.Msg.GetChallenge()
	signature := ed25519.Sign(ed25519.PrivateKey(priv), challenge)

	complete, err := c.CompleteAuthHandshake(ctx, connect.NewRequest(&pb.CompleteAuthHandshakeRequest{
		Challenge: challenge,
		Signature: signature,
	}))
	if err != nil {
		return fmt.Errorf("complete handshake: %w", err)
	}

	s.SessionToken = complete.Msg.GetSessionToken()
	if exp := complete.Msg.GetExpiresAt(); exp != nil {
		s.SessionExpiresAt = exp.AsTime()
	}
	return nil
}
