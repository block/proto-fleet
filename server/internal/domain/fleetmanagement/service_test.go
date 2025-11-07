package fleetmanagement_test

import (
	"testing"
	"time"

	"connectrpc.com/authn"
	"github.com/golang-jwt/jwt/v5"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	pairingpb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
	discoverymodels "github.com/btc-mining/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces/mocks"
	tokenDomain "github.com/btc-mining/proto-fleet/server/internal/domain/token"
)

func TestService_ListUnpairedDevices_ShouldReturnDevices(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	mockDiscoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	service := fleetmanagement.NewService(mockDeviceStore, mockDiscoveredDeviceStore, nil, nil)

	orgID := int64(123)
	ctx := authn.SetInfo(t.Context(), &tokenDomain.ClientAuthClaims{
		UserID: int64(1),
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	mockDevices := []*discoverymodels.DiscoveredDevice{
		{
			Device: pairingpb.Device{
				DeviceIdentifier: "device-1",
				IpAddress:        "192.168.1.100",
				Port:             "4028",
				UrlScheme:        "http",
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				Type:             "ANTMINER",
			},
			IsActive: true,
			OrgID:    orgID,
		},
		{
			Device: pairingpb.Device{
				DeviceIdentifier: "device-2",
				IpAddress:        "192.168.1.101",
				Port:             "4028",
				UrlScheme:        "http",
				Model:            "S21",
				Manufacturer:     "Bitmain",
				Type:             "ANTMINER",
			},
			IsActive: true,
			OrgID:    orgID,
		},
	}

	mockDiscoveredDeviceStore.EXPECT().
		GetActiveUnpairedDevices(ctx, orgID, "", int32(50)).
		Return(mockDevices, "", nil)
	mockDiscoveredDeviceStore.EXPECT().
		CountActiveUnpairedDevices(ctx, orgID).
		Return(int64(2), nil)

	req := &pb.ListUnpairedDevicesRequest{
		PageSize: 50,
	}

	// Act
	resp, err := service.ListUnpairedDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Devices, 2)
	assert.Equal(t, int32(2), resp.TotalDevices)
	assert.Empty(t, resp.Cursor)

	// Verify device fields including device_identifier
	assert.Equal(t, "device-1", resp.Devices[0].DeviceIdentifier)
	assert.Equal(t, "192.168.1.100", resp.Devices[0].IpAddress)
	assert.Equal(t, "4028", resp.Devices[0].Port)
	assert.Equal(t, "http", resp.Devices[0].UrlScheme)
	assert.Equal(t, "S19 Pro", resp.Devices[0].Model)
	assert.Equal(t, "Bitmain", resp.Devices[0].Manufacturer)
	assert.Equal(t, "ANTMINER", resp.Devices[0].Type)
}

func TestService_ListUnpairedDevices_ShouldHandlePagination(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	mockDiscoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	service := fleetmanagement.NewService(mockDeviceStore, mockDiscoveredDeviceStore, nil, nil)

	orgID := int64(123)
	ctx := authn.SetInfo(t.Context(), &tokenDomain.ClientAuthClaims{
		UserID: int64(1),
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	// Store returns 2 devices and a cursor indicating more pages
	mockDevices := []*discoverymodels.DiscoveredDevice{
		{
			Device: pairingpb.Device{
				DeviceIdentifier: "device-1",
				IpAddress:        "192.168.1.100",
				Port:             "4028",
				UrlScheme:        "http",
				Model:            "S19 Pro",
				Manufacturer:     "Bitmain",
				Type:             "ANTMINER",
			},
			IsActive: true,
			OrgID:    orgID,
		},
		{
			Device: pairingpb.Device{
				DeviceIdentifier: "device-2",
				IpAddress:        "192.168.1.101",
				Port:             "4028",
				UrlScheme:        "http",
				Model:            "S21",
				Manufacturer:     "Bitmain",
				Type:             "ANTMINER",
			},
			IsActive: true,
			OrgID:    orgID,
		},
	}

	mockDiscoveredDeviceStore.EXPECT().
		GetActiveUnpairedDevices(ctx, orgID, "", int32(2)).
		Return(mockDevices, "next-cursor-token", nil)
	mockDiscoveredDeviceStore.EXPECT().
		CountActiveUnpairedDevices(ctx, orgID).
		Return(int64(10), nil)

	req := &pb.ListUnpairedDevicesRequest{
		PageSize: 2,
	}

	// Act
	resp, err := service.ListUnpairedDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Devices, 2, "Should only return 2 devices")
	assert.Equal(t, int32(10), resp.TotalDevices)
	assert.NotEmpty(t, resp.Cursor, "Should have a cursor for next page")
}

func TestService_ListUnpairedDevices_ShouldUseDefaultPageSize(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	mockDiscoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	service := fleetmanagement.NewService(mockDeviceStore, mockDiscoveredDeviceStore, nil, nil)

	orgID := int64(123)
	ctx := authn.SetInfo(t.Context(), &tokenDomain.ClientAuthClaims{
		UserID: int64(1),
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	mockDiscoveredDeviceStore.EXPECT().
		GetActiveUnpairedDevices(ctx, orgID, "", int32(50)).
		Return([]*discoverymodels.DiscoveredDevice{}, "", nil)
	mockDiscoveredDeviceStore.EXPECT().
		CountActiveUnpairedDevices(ctx, orgID).
		Return(int64(0), nil)

	req := &pb.ListUnpairedDevicesRequest{
		PageSize: 0, // Not specified
	}

	// Act
	resp, err := service.ListUnpairedDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestService_ListUnpairedDevices_ShouldCapMaxPageSize(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDeviceStore := mocks.NewMockDeviceStore(ctrl)
	mockDiscoveredDeviceStore := mocks.NewMockDiscoveredDeviceStore(ctrl)
	service := fleetmanagement.NewService(mockDeviceStore, mockDiscoveredDeviceStore, nil, nil)

	orgID := int64(123)
	ctx := authn.SetInfo(t.Context(), &tokenDomain.ClientAuthClaims{
		UserID: int64(1),
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	mockDiscoveredDeviceStore.EXPECT().
		GetActiveUnpairedDevices(ctx, orgID, "", int32(1000)).
		Return([]*discoverymodels.DiscoveredDevice{}, "", nil)
	mockDiscoveredDeviceStore.EXPECT().
		CountActiveUnpairedDevices(ctx, orgID).
		Return(int64(0), nil)

	req := &pb.ListUnpairedDevicesRequest{
		PageSize: 2000, // Exceeds max
	}

	// Act
	resp, err := service.ListUnpairedDevices(ctx, req)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, resp)
}
