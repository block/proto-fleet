package plugins

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func TestClassifyNewDeviceError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want func(error) bool
	}{
		{
			name: "in-process SDK auth error → unauthenticated",
			err:  sdk.NewErrorAuthenticationFailed("device-1"),
			want: fleeterror.IsAuthenticationError,
		},
		{
			name: "out-of-process gRPC Unauthenticated → unauthenticated",
			err:  grpcstatus.Error(codes.Unauthenticated, "authentication failed"),
			want: fleeterror.IsAuthenticationError,
		},
		{
			name: "out-of-process gRPC PermissionDenied with default-password marker → forbidden",
			err:  grpcstatus.Error(codes.PermissionDenied, "default password must be changed"),
			want: fleeterror.IsForbiddenError,
		},
		{
			name: "out-of-process gRPC PermissionDenied without marker → forbidden",
			err:  grpcstatus.Error(codes.PermissionDenied, "access denied"),
			want: fleeterror.IsForbiddenError,
		},
		{
			name: "unrelated error → internal",
			err:  errors.New("connection refused"),
			want: func(err error) bool {
				return !fleeterror.IsAuthenticationError(err) && !fleeterror.IsForbiddenError(err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyNewDeviceError(tt.err, "device-1")
			assert.True(t, tt.want(got), "got: %v", got)
		})
	}
}

func TestNewPluginMinerWithCredentials_BasicAuthRequiresStoredCredentials(t *testing.T) {
	_, err := NewPluginMinerWithCredentials(t.Context(), PluginMinerConfig{
		DeviceIdentifier: "device-1",
		DevicePort:       "443",
		DeviceScheme:     "https",
		Caps:             sdk.Capabilities{sdk.CapabilityBasicAuth: true},
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsAuthenticationError(err), "got: %v", err)
}
