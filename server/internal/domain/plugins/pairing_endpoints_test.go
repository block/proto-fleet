package plugins

import (
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	sdkmocks "github.com/block/proto-fleet/server/sdk/v1/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
)

func TestPairingRequiresDiscoveryBeforeAdoptingEndpointGroup(t *testing.T) {
	for _, savedKey := range []string{"", "different-group"} {
		name := "legacy inventory"
		if savedKey != "" {
			name = "conflicting group"
		}
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			driver := sdkmocks.NewMockDriver(ctrl)
			manager := NewManager(&Config{})
			manager.pluginsByDriverName["endpoint-test"] = &LoadedPlugin{Identifier: sdk.DriverIdentifier{DriverName: "endpoint-test"}, Driver: driver, Caps: sdk.Capabilities{sdk.CapabilityPairing: true}}
			pairer := createTestPairer(ctrl, manager)
			device := &discovery.DiscoveredDevice{Device: pb.Device{DeviceIdentifier: "selected", DriverName: "endpoint-test", DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, IpAddress: "10.0.0.11", Port: "8080", UrlScheme: "http"}, OrgID: 1}
			if savedKey != "" {
				device.Endpoints = []*commonpb.DeviceEndpoint{{Id: "a", DeviceKey: savedKey, Host: device.IpAddress, Port: 8080, UrlScheme: "http", RpcPort: 1234}}
			}
			before := proto.CloneOf(&device.Device)
			driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.DeviceInfo{
				Endpoints:    []sdk.DeviceEndpoint{{ID: "a", DeviceKey: "new-group", Host: device.IpAddress, Port: 8080, URLScheme: "http", RPCPort: 1234}},
				SerialNumber: "changed-identity",
			}, nil)

			err := pairer.PairDevice(t.Context(), device, nil)

			require.True(t, fleeterror.IsFailedPreconditionError(err), "group adoption must stop before any store calls, got %v", err)
			require.True(t, proto.Equal(before, &device.Device), "a refused pairing must leave its selected inventory snapshot intact")
		})
	}
}

func TestPairingPreservesDiscoveredRoutesWhenResponseOmitsOrChangesThem(t *testing.T) {
	for _, omitEndpoints := range []bool{true, false} {
		name := "omitted routes"
		if !omitEndpoints {
			name = "changed routes with same group key"
		}
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			driver := sdkmocks.NewMockDriver(ctrl)
			pairer := &Pairer{}
			device := &discovery.DiscoveredDevice{Device: pb.Device{DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER,
				Endpoints: []*commonpb.DeviceEndpoint{{Id: "a", DeviceKey: "saved-group", Host: "10.0.0.11", Port: 8443, UrlScheme: "https", RpcPort: 1234}},
			}}
			before, err := deviceendpoints.Encode(device.Endpoints)
			require.NoError(t, err)
			response := sdk.DeviceInfo{FirmwareVersion: "new-firmware"}
			if !omitEndpoints {
				response.Endpoints = []sdk.DeviceEndpoint{{ID: "rogue", DeviceKey: "saved-group", Host: "10.0.0.99", Port: 80, URLScheme: "http", RPCPort: 4321}}
			}
			driver.EXPECT().PairDevice(gomock.Any(), gomock.Any(), gomock.Any()).Return(response, nil)

			require.NoError(t, pairer.callPluginPairDevice(t.Context(), &LoadedPlugin{Driver: driver}, device, nil))

			after, err := deviceendpoints.Encode(device.Endpoints)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "pairing must never replace routes outside discovery reconciliation")
			require.Equal(t, "new-firmware", device.FirmwareVersion)
		})
	}
}
