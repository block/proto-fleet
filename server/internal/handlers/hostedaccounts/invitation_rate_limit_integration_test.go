package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestInvitationRPCSharesClientRateLimitAcrossAllLookupPaths(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	issued := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	for range 60 {
		_, err := f.client.PreviewInvitation(t.Context(), connect.NewRequest(&pb.PreviewInvitationRequest{Token: "AAAA-AAAA-AAAA-AAAA"}))
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	}
	_, err := f.client.PreviewInvitation(t.Context(), connect.NewRequest(&pb.PreviewInvitationRequest{Token: "AAAA-AAAA-AAAA-AAAA"}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err), "anonymous misses exhaust only their client's allowance")
	_, err = f.client.AcceptInvitation(t.Context(), rpcRequest(&pb.AcceptInvitationRequest{Token: "AAAA-AAAA-AAAA-AAAA"}, f.cookie, ""))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err), "signing in cannot bypass the client's preview allowance")
	_, err = f.client.RegisterInvitedUser(t.Context(), connect.NewRequest(&pb.RegisterInvitedUserRequest{Token: "AAAA-AAAA-AAAA-AAAA", Username: "throttled-user", Password: rpcPassword}))
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	// A caller-controlled forwarding header cannot replace the trusted peer.
	spoofed := connect.NewRequest(&pb.PreviewInvitationRequest{Token: issued.Token})
	spoofed.Header().Set("X-Forwarded-For", "198.51.100.10")
	_, err = f.client.PreviewInvitation(t.Context(), spoofed)
	require.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	otherPeer := "198.51.100.10:1234"
	f.peerAddress.Store(&otherPeer)
	_, err = f.client.PreviewInvitation(t.Context(), connect.NewRequest(&pb.PreviewInvitationRequest{Token: issued.Token}))
	require.NoError(t, err, "another peer can still preview a valid invitation")
	registered := f.register(t, issued.Token, "independent-recipient")
	require.NotEmpty(t, registered.Header().Get("Set-Cookie"), "the other peer can finish onboarding")
}
