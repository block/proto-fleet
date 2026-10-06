package gateway

import (
	"context"
	"errors"
	"sync"
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

type concurrentHandshakeAuthService struct {
	firstStarted  chan struct{}
	releaseFirst  chan struct{}
	secondStarted chan struct{}

	mu           sync.Mutex
	callCount    int
	currentToken string
}

func (s *concurrentHandshakeAuthService) BeginHandshake(context.Context, string, []byte) ([]byte, time.Time, error) {
	panic("not used")
}

func (s *concurrentHandshakeAuthService) CompleteHandshake(context.Context, []byte, []byte) (string, time.Time, int64, error) {
	s.mu.Lock()
	s.callCount++
	call := s.callCount
	s.mu.Unlock()

	if call == 1 {
		close(s.firstStarted)
		<-s.releaseFirst
	} else {
		close(s.secondStarted)
	}

	token := "old-token"
	if call == 2 {
		token = "new-token"
	}
	s.mu.Lock()
	s.currentToken = token
	s.mu.Unlock()
	return token, time.Now().Add(time.Hour), 42, nil
}

func (s *concurrentHandshakeAuthService) CurrentToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentToken
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
	newStream, err := registry.RegisterAuthenticated(
		fleetNodeID,
		auth.SessionFingerprint("replacement-token"),
		pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	)
	require.NoError(t, err)
	newStream.Unregister()
	_, err = registry.RegisterAuthenticated(
		fleetNodeID,
		auth.SessionFingerprint("old-token"),
		pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	)
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

func TestCompleteAuthHandshakeSerializesSessionPersistenceAndFence(t *testing.T) {
	authService := &concurrentHandshakeAuthService{
		firstStarted:  make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		secondStarted: make(chan struct{}),
	}
	registry := control.NewRegistry()
	handler := &Handler{auth: authService, registry: registry}

	firstDone := make(chan error, 1)
	go func() {
		_, err := handler.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))
		firstDone <- err
	}()
	<-authService.firstStarted

	secondRequestStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondRequestStarted)
		_, err := handler.CompleteAuthHandshake(t.Context(), connect.NewRequest(&pb.CompleteAuthHandshakeRequest{}))
		secondDone <- err
	}()
	<-secondRequestStarted
	require.Never(t, func() bool {
		select {
		case <-authService.secondStarted:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, time.Millisecond, "second persistence entered before the first fence update")

	close(authService.releaseFirst)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	require.Equal(t, "new-token", authService.CurrentToken())

	currentStream, err := registry.RegisterAuthenticated(
		42,
		auth.SessionFingerprint(authService.CurrentToken()),
		pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	)
	require.NoError(t, err)
	currentStream.Unregister()
	_, err = registry.RegisterAuthenticated(
		42,
		auth.SessionFingerprint("old-token"),
		pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1,
	)
	require.Error(t, err)
}
