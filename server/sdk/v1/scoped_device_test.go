package sdk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

func scopedRequest() *pb.NewDeviceRequest {
	return &pb.NewDeviceRequest{DeviceId: "device-123", ReuseScopeKey: "incarnation-org-epoch", Info: &pb.DeviceInfo{Host: "127.0.0.1", Port: 80, UrlScheme: "http", SerialNumber: "serial", MacAddress: "00:11:22:33:44:55"}, Secret: &pb.SecretBundle{Version: "v1", Kind: &pb.SecretBundle_UserPass{UserPass: &pb.UsernamePassword{Username: "admin", Password: "test-only-password"}}}}
}
func scopedServer(create func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error)) *DriverGRPCServer {
	return &DriverGRPCServer{Impl: fakeDriver{newDeviceFunc: create}, devices: map[string]Device{}}
}

func TestScopedDeviceResolutionRetainsOneBackendAcrossConcurrentResolutions(t *testing.T) {
	var creations atomic.Int64
	s := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
		creations.Add(1)
		return NewDeviceResult{Device: fakeDevice{}}, nil
	})
	type outcome struct {
		response *pb.NewDeviceResponse
		err      error
	}
	outcomes := make(chan outcome, 24)
	var group sync.WaitGroup
	for range 24 {
		group.Go(func() {
			response, err := s.NewDevice(t.Context(), scopedRequest())
			outcomes <- outcome{response, err}
		})
	}
	group.Wait()
	close(outcomes)
	for result := range outcomes {
		require.NoError(t, result.err)
		require.Equal(t, "device-123", result.response.DeviceId)
		require.Equal(t, "incarnation-org-epoch", result.response.ReuseScopeKey)
	}
	require.Equal(t, int64(1), creations.Load())
}

func TestScopedDeviceResolutionBindsCompleteActualRequest(t *testing.T) {
	cases := map[string]func(*pb.NewDeviceRequest){
		"scope":          func(r *pb.NewDeviceRequest) { r.ReuseScopeKey = "new-epoch" },
		"host":           func(r *pb.NewDeviceRequest) { r.Info.Host = "127.0.0.2" },
		"port":           func(r *pb.NewDeviceRequest) { r.Info.Port = 443 },
		"scheme":         func(r *pb.NewDeviceRequest) { r.Info.UrlScheme = "https" },
		"serial":         func(r *pb.NewDeviceRequest) { r.Info.SerialNumber = "new-serial" },
		"mac":            func(r *pb.NewDeviceRequest) { r.Info.MacAddress = "00:11:22:33:44:66" },
		"model":          func(r *pb.NewDeviceRequest) { r.Info.Model = "different-model" },
		"manufacturer":   func(r *pb.NewDeviceRequest) { r.Info.Manufacturer = "different-firmware" },
		"class":          func(r *pb.NewDeviceRequest) { r.Info.DeviceClass = 1 },
		"secret-version": func(r *pb.NewDeviceRequest) { r.Secret.Version = "v2" },
		"secret-ttl":     func(r *pb.NewDeviceRequest) { r.Secret.Ttl = durationpb.New(time.Minute) },
		"username":       func(r *pb.NewDeviceRequest) { r.Secret.GetUserPass().Username = "different-user" },
		"password":       func(r *pb.NewDeviceRequest) { r.Secret.GetUserPass().Password = "different-test-password" },
		"secret-kind": func(r *pb.NewDeviceRequest) {
			r.Secret.Kind = &pb.SecretBundle_BearerToken{BearerToken: &pb.BearerToken{Token: "test-only-token"}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			creates := 0
			s := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
				creates++
				return NewDeviceResult{Device: fakeDevice{}}, nil
			})
			request := scopedRequest()
			_, err := s.NewDevice(t.Context(), request)
			require.NoError(t, err)
			changed := cloneScopedRequest(t, request)
			change(changed)
			_, err = s.NewDevice(t.Context(), changed)
			require.NoError(t, err)
			_, err = s.NewDevice(t.Context(), cloneScopedRequest(t, changed))
			require.NoError(t, err)
			require.Equal(t, 2, creates, "a changed actual request must replace even when trusted scope is unchanged")
		})
	}
}

func TestDefaultNewDeviceStillReplacesAndInvalidatesScopedEvidence(t *testing.T) {
	creates := 0
	s := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
		creates++
		return NewDeviceResult{Device: fakeDevice{}}, nil
	})
	request := scopedRequest()
	_, err := s.NewDevice(t.Context(), request)
	require.NoError(t, err)
	plain := cloneScopedRequest(t, request)
	plain.ReuseScopeKey = ""
	for range 2 {
		response, err := s.NewDevice(t.Context(), plain)
		require.NoError(t, err)
		require.Empty(t, response.ReuseScopeKey)
	}
	_, err = s.NewDevice(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, 4, creates)
}

func TestScopedResolutionPreservesCreatingAndClosingReservations(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var creates atomic.Int64
	closeFails := true
	s := scopedServer(func(context.Context, string, DeviceInfo, SecretBundle) (NewDeviceResult, error) {
		creates.Add(1)
		if creates.Load() == 1 {
			close(started)
			<-release
		}
		return NewDeviceResult{Device: fakeDevice{closeFunc: func(context.Context) error {
			if closeFails {
				return errors.New("test cleanup failure")
			}
			return nil
		}}}, nil
	})
	first := make(chan error, 1)
	go func() { _, err := s.NewDevice(t.Context(), scopedRequest()); first <- err }()
	<-started
	plain := scopedRequest()
	plain.ReuseScopeKey = ""
	_, err := s.NewDevice(t.Context(), plain)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	_, err = s.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.Equal(t, codes.Unavailable, status.Code(err))
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	waiter := make(chan error, 1)
	go func() { _, err := s.NewDevice(canceled, scopedRequest()); waiter <- err }()
	close(release)
	require.NoError(t, <-first)
	require.Equal(t, codes.Canceled, status.Code(<-waiter))
	require.Equal(t, int64(1), creates.Load())
	_, err = s.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.Error(t, err)
	_, err = s.NewDevice(t.Context(), scopedRequest())
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	closeFails = false
	_, err = s.CloseDevice(t.Context(), &pb.DeviceRef{DeviceId: "device-123"})
	require.NoError(t, err)
	_, err = s.NewDevice(t.Context(), scopedRequest())
	require.NoError(t, err)
	require.Equal(t, int64(2), creates.Load())
}

type scopedClientPeer struct {
	pb.DriverClient
	supports bool
	response *pb.NewDeviceResponse
	creates  int
}

func (p *scopedClientPeer) DescribeDriver(context.Context, *emptypb.Empty, ...grpc.CallOption) (*pb.DescribeDriverResponse, error) {
	return &pb.DescribeDriverResponse{Caps: &pb.Capabilities{Flags: map[string]bool{CapabilityScopedDeviceReuse: p.supports}}}, nil
}
func (p *scopedClientPeer) NewDevice(context.Context, *pb.NewDeviceRequest, ...grpc.CallOption) (*pb.NewDeviceResponse, error) {
	p.creates++
	return p.response, nil
}
func TestScopedClientRejectsUnsupportedRuntimeAndMismatchedConfirmation(t *testing.T) {
	for _, test := range []struct {
		name      string
		supports  bool
		id, scope string
		creates   int
	}{
		{name: "unsupported", creates: 0}, {name: "mismatched-ID", supports: true, id: "other", scope: "scope", creates: 1}, {name: "mismatched-scope", supports: true, id: "device-123", scope: "other", creates: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			peer := &scopedClientPeer{supports: test.supports, response: &pb.NewDeviceResponse{DeviceId: test.id, ReuseScopeKey: test.scope}}
			client := &DriverGRPCClient{client: peer}
			_, err := client.GetOrCreateDevice(t.Context(), "device-123", DeviceInfo{}, SecretBundle{}, "scope")
			require.Error(t, err)
			require.Equal(t, test.creates, peer.creates)
		})
	}
}

func cloneScopedRequest(t *testing.T, request *pb.NewDeviceRequest) *pb.NewDeviceRequest {
	t.Helper()
	cloned, ok := proto.Clone(request).(*pb.NewDeviceRequest)
	require.True(t, ok, "cloned request must preserve its concrete message type")
	return cloned
}
