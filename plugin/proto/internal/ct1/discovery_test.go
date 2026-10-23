package ct1

import (
	"context"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDiscoveryOptionalPartnerMetadataErrors(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Unimplemented,
		codes.PermissionDenied,
		codes.Unknown,
		codes.Unauthenticated,
		codes.Unavailable,
		codes.Internal,
		codes.DeadlineExceeded,
	} {
		t.Run(code.String(), func(t *testing.T) {
			s := startHAServer(t, "a", "b")
			s.supportError = status.Error(code, "partner metadata unavailable")
			info := sdk.DeviceInfo{Host: s.endpoint.Host, Port: s.endpoint.Port, URLScheme: s.endpoint.URLScheme, RPCPort: s.endpoint.RPCPort}
			c, err := NewClient(info, sdk.UsernamePassword{})
			require.NoError(t, err)
			defer c.Close()
			got, err := c.Discover(t.Context(), info)
			require.NoError(t, err)
			require.Equal(t, sdk.DeviceClassContainer, got.DeviceClass)
			require.Equal(t, "a", got.SerialNumber)
			require.Empty(t, got.Endpoints, "missing partner metadata cannot establish a pair")
			info.SerialNumber = "replacement"
			_, err = c.Discover(t.Context(), info)
			require.ErrorContains(t, err, "controller identity mismatch")
			info.SerialNumber = "a"
			info.Endpoints = []sdk.DeviceEndpoint{s.endpoint}
			_, err = c.Discover(t.Context(), info)
			require.ErrorContains(t, err, "read CT1 partner identity")
			require.Equal(t, code, status.Code(err), "saved endpoints must not fall back to standalone discovery")
		})
	}
}

func TestDiscoveryHonorsCancellationDuringOptionalPartnerRead(t *testing.T) {
	s := startHAServer(t, "a", "b")
	s.supportEntered = make(chan struct{})
	info := sdk.DeviceInfo{Host: s.endpoint.Host, Port: s.endpoint.Port, URLScheme: s.endpoint.URLScheme, RPCPort: s.endpoint.RPCPort}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := c.Discover(ctx, info)
		completed <- err
	}()
	select {
	case <-s.supportEntered:
	case err := <-completed:
		t.Fatalf("discovery returned before the optional RPC started: %v", err)
	}
	cancel()
	err = <-completed
	require.Equal(t, codes.Canceled, status.Code(err))
}
