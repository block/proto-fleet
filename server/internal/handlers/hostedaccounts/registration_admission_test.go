package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	hostedhandler "github.com/block/proto-fleet/server/internal/handlers/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestInvitedRegistrationRequiresTrustedClientAddress(t *testing.T) {
	handler := hostedhandler.NewHandler(nil, nil, nil, nil)
	req := connect.NewRequest(&pb.RegisterInvitedUserRequest{Token: "untrusted", Username: "new-user", Password: "password123"})
	req.Header().Set("X-Forwarded-For", "192.0.2.1")
	_, err := handler.RegisterInvitedUser(t.Context(), req)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "headers cannot substitute for trusted middleware context")
}
