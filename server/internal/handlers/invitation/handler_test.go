package invitation_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/invitation/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	invitationhandler "github.com/block/proto-fleet/server/internal/handlers/invitation"
	"github.com/stretchr/testify/require"
)

func TestInvitedRegistrationRequiresTrustedClientAddress(t *testing.T) {
	handler := invitationhandler.NewHandler(nil, nil)
	req := connect.NewRequest(&pb.RegisterInvitedUserRequest{Token: "untrusted", Username: "new-user", Password: "password123"})
	req.Header().Set("X-Forwarded-For", "192.0.2.1")
	_, err := handler.RegisterInvitedUser(t.Context(), req)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "headers cannot substitute for trusted middleware context")
}

func TestInvitationPreviewRequiresTrustedClientAddress(t *testing.T) {
	handler := invitationhandler.NewHandler(nil, nil)
	req := connect.NewRequest(&pb.PreviewInvitationRequest{Token: "K7MP-4X9R"})
	req.Header().Set("X-Forwarded-For", "192.0.2.1")
	_, err := handler.PreviewInvitation(t.Context(), req)
	require.True(t, fleeterror.IsFailedPreconditionError(err), "headers cannot substitute for trusted middleware context")
}
