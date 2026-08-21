package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
)

type stubAuthService struct {
	token       string
	expiresAt   time.Time
	fleetNodeID int64
	err         error
}

func (stubAuthService) BeginHandshake(context.Context, string, []byte) ([]byte, time.Time, error) {
	panic("not used")
}

func (s stubAuthService) CompleteHandshake(context.Context, []byte, []byte) (string, time.Time, int64, error) {
	return s.token, s.expiresAt, s.fleetNodeID, s.err
}

func TestCompleteAuthHandshakeDisconnectsReplacedSessionStream(t *testing.T) {
	const fleetNodeID = 42
	registry := control.NewRegistry()
	oldStream := registry.Register(fleetNodeID)
	expiresAt := time.Now().Add(time.Hour)
	handler := &Handler{auth: stubAuthService{
		token:       "replacement-token",
		expiresAt:   expiresAt,
		fleetNodeID: fleetNodeID,
	}, registry: registry}

	response, err := handler.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))

	require.NoError(t, err)
	require.Equal(t, "replacement-token", response.Msg.GetSessionToken())
	require.True(t, expiresAt.Equal(response.Msg.GetExpiresAt().AsTime()))
	select {
	case <-oldStream.Done:
	default:
		t.Fatal("replaced session's ControlStream remains connected")
	}
	require.Empty(t, registry.ConnectedFleetNodeIDs())
	newStream, err := registry.RegisterAuthenticated(fleetNodeID, auth.SessionFingerprint("replacement-token"))
	require.NoError(t, err)
	newStream.Unregister()
	_, err = registry.RegisterAuthenticated(fleetNodeID, auth.SessionFingerprint("old-token"))
	require.Error(t, err)
}

func TestCompleteAuthHandshakeFailureKeepsCurrentStream(t *testing.T) {
	const fleetNodeID = 42
	registry := control.NewRegistry()
	oldStream := registry.Register(fleetNodeID)
	handler := &Handler{auth: stubAuthService{
		fleetNodeID: fleetNodeID,
		err:         errors.New("handshake failed"),
	}, registry: registry}

	_, err := handler.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))

	require.EqualError(t, err, "handshake failed")
	select {
	case <-oldStream.Done:
		t.Fatal("failed session replacement disconnected the current ControlStream")
	default:
	}
}
