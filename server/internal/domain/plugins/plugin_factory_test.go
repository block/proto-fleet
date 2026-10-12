package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/internal/infrastructure/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkMocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
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

type capturingContainerDriver struct {
	sdk.Driver
	info sdk.DeviceInfo
}

func (d *capturingContainerDriver) NewDevice(_ context.Context, _ string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
	d.info = info
	return sdk.NewDeviceResult{}, errors.New("stop after capturing reconstruction")
}

type containerDriverGetter struct{ driver sdk.Driver }

func (g containerDriverGetter) GetDriverByDriverName(string) (sdk.Driver, error) {
	return g.driver, nil
}
func TestContainerClassSurvivesPluginReconstruction(t *testing.T) {
	driver := &capturingContainerDriver{}
	_, err := NewPluginMinerWithCredentials(t.Context(), PluginMinerConfig{
		SDKReuseScopeKey: "container-binding", DeviceIdentifier: "ct1", DeviceClass: sdk.DeviceClassContainer, DriverName: "proto", DeviceIPAddress: "192.0.2.1", DevicePort: "8080", DeviceScheme: "http", FilesService: &files.Service{}, DriverGetter: containerDriverGetter{&sdkMocks.ScopedDriverFixture{Driver: driver}},
	})
	require.ErrorContains(t, err, "stop after capturing reconstruction")
	require.Equal(t, sdk.DeviceClassContainer, driver.info.DeviceClass)
}

func TestScopedPluginReconstructionRejectsUnsupportedResolverBeforeNewDevice(t *testing.T) {
	driver := &capturingContainerDriver{}
	_, err := NewPluginMinerWithCredentials(t.Context(), PluginMinerConfig{
		SDKReuseScopeKey: "trusted-current-incarnation", DeviceIdentifier: "device-unsupported", DriverName: "custom", FilesService: &files.Service{}, DeviceIPAddress: "192.0.2.1", DevicePort: "8080", DeviceScheme: "http", DriverGetter: containerDriverGetter{driver},
	})
	require.ErrorContains(t, err, "scoped device resolution")
	require.Empty(t, driver.info.Host, "unsupported resolution must not call destructive NewDevice as a fallback")
}

func TestPluginReconstructionRequiresScopeBeforeDeviceCreation(t *testing.T) {
	driver := &capturingContainerDriver{}
	_, err := NewPluginMinerWithCredentials(t.Context(), PluginMinerConfig{
		DeviceIdentifier: "device-unscoped", DriverName: "custom", FilesService: &files.Service{}, DeviceIPAddress: "192.0.2.1", DevicePort: "8080", DeviceScheme: "http", DriverGetter: containerDriverGetter{driver},
	})
	require.ErrorContains(t, err, "miner resolution requires a binding scope")
	require.Empty(t, driver.info.Host, "a missing scope must not create or replace a backend")
}
