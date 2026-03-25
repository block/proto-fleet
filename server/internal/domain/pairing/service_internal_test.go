package pairing

import (
	"context"
	"testing"

	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/fleeterror"
	discoverymodels "github.com/proto-at-block/proto-fleet/server/internal/domain/minerdiscovery/models"
	stores "github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestHandleAuthenticationRequiredPairing_PreservesExistingWorkerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTransactor := mocks.NewMockTransactor(ctrl)
	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)

	service := &Service{
		deviceStore: mockDeviceStore,
		transactor:  mockTransactor,
	}

	discoveredDevice := &discoverymodels.DiscoveredDevice{
		Device: pb.Device{
			DeviceIdentifier: "device-123",
			IpAddress:        "192.168.1.100",
			Port:             "80",
			UrlScheme:        "http",
			DriverName:       "antminer",
			MacAddress:       "AA:BB:CC:DD:EE:FF",
		},
		OrgID: 1,
	}

	mockTransactor.EXPECT().RunInTx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error {
			return fn(t.Context())
		},
	)

	mockDeviceStore.EXPECT().
		GetPairedDeviceByMACAddress(gomock.Any(), "AA:BB:CC:DD:EE:FF", int64(1)).
		Return(nil, fleeterror.NewNotFoundError("no paired device"))
	mockDeviceStore.EXPECT().
		GetDeviceByDeviceIdentifier(gomock.Any(), "device-123", int64(1)).
		Return(&pb.Device{DeviceIdentifier: "device-123"}, nil)
	mockDeviceStore.EXPECT().
		UpdateDeviceInfo(gomock.Any(), gomock.Any(), int64(1)).
		Return(nil)
	mockDeviceStore.EXPECT().
		GetDevicePropertiesForRename(gomock.Any(), int64(1), []string{"device-123"}, false).
		Return([]stores.DeviceRenameProperties{
			{
				DeviceIdentifier: "device-123",
				WorkerName:       "rig-01",
			},
		}, nil)
	mockDeviceStore.EXPECT().
		UpsertDevicePairing(gomock.Any(), gomock.Any(), int64(1), StatusAuthenticationNeeded).
		Return(nil)

	err := service.handleAuthenticationRequiredPairing(t.Context(), discoveredDevice)
	require.NoError(t, err)
}
