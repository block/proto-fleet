package minerdiscovery

import (
	"testing"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryDiscoveredDeviceStore_Save(t *testing.T) {
	// Define common test variables
	var orgID int64 = 1001
	deviceID1 := "device1"
	deviceID2 := "device2"
	serial1 := "serial1"
	serial2 := "serial2"
	ipAddress1 := "192.168.1.1"
	ipAddress2 := "192.168.1.2"
	deviceType := "antminer"

	t.Run("Save new device", func(t *testing.T) {
		// Arrange
		store := NewInMemoryDiscoveredDeviceStore()
		doi := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID1,
		}
		device := &DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: deviceID1,
				SerialNumber:     serial1,
				MacAddress:       "00:11:22:33:44:55",
				IpAddress:        ipAddress1,
				Port:             "4028",
				Model:            "S19",
				Manufacturer:     "Bitmain",
			},
			Type: deviceType,
		}

		// Act
		savedDevice, err := store.Save(doi, device)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, savedDevice)
		assert.Equal(t, deviceID1, savedDevice.DeviceIdentifier)
		assert.Equal(t, serial1, savedDevice.SerialNumber)
		assert.Equal(t, ipAddress1, savedDevice.IpAddress)
		assert.Equal(t, deviceType, savedDevice.Type)
		assert.Equal(t, orgID, savedDevice.OrgID)
		assert.NotZero(t, savedDevice.LastSeen)
		assert.NotZero(t, savedDevice.FirstDiscovered)

		retrievedDevice, err := store.GetDevice(doi)
		require.NoError(t, err)
		assert.Equal(t, savedDevice, retrievedDevice)
	})

	t.Run("Update existing device by identifier", func(t *testing.T) {
		// Arrange
		store := NewInMemoryDiscoveredDeviceStore()
		doi := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID1,
		}
		originalDevice := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial1,
				MacAddress:   "00:11:22:33:44:55",
				IpAddress:    ipAddress1,
				Port:         "4028",
				Model:        "S19",
				Manufacturer: "Bitmain",
			},
			Type: deviceType,
		}
		_, err := store.Save(doi, originalDevice)
		require.NoError(t, err)

		firstDevice, err := store.GetDevice(doi)
		require.NoError(t, err)
		originalLastSeen := firstDevice.LastSeen
		originalFirstDiscovered := firstDevice.FirstDiscovered

		time.Sleep(time.Millisecond)

		updatedDevice := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial1,
				MacAddress:   "00:11:22:33:44:55",
				IpAddress:    ipAddress2,
				Port:         "4028",
				Model:        "S19 Pro",
				Manufacturer: "Bitmain",
			},
			Type: deviceType,
		}

		// Act
		savedDevice, err := store.Save(doi, updatedDevice)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, "S19 Pro", savedDevice.Model)
		assert.Equal(t, ipAddress2, savedDevice.IpAddress)
		assert.Equal(t, originalFirstDiscovered, savedDevice.FirstDiscovered)
		assert.True(t, savedDevice.LastSeen.After(originalLastSeen))
	})

	t.Run("Find device by serial number", func(t *testing.T) {
		// Arrange
		store := NewInMemoryDiscoveredDeviceStore()
		doi1 := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID1,
		}
		device1 := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial1,
				MacAddress:   "00:11:22:33:44:55",
				IpAddress:    ipAddress1,
				Port:         "4028",
				Model:        "S19",
				Manufacturer: "Bitmain",
			},
			Type: deviceType,
		}
		_, err := store.Save(doi1, device1)
		require.NoError(t, err)

		doi2 := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID2,
		}
		device2 := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial1,
				MacAddress:   "AA:BB:CC:DD:EE:FF",
				IpAddress:    "192.168.1.3",
				Port:         "4028",
				Model:        "S19j",
				Manufacturer: "Bitmain",
			},
			Type: deviceType,
		}

		// Act
		savedDevice, err := store.Save(doi2, device2)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, deviceID1, savedDevice.DeviceIdentifier)

		originalDevice, err := store.GetDevice(doi1)
		require.NoError(t, err)
		assert.Equal(t, deviceID1, originalDevice.DeviceIdentifier)

		newDevice, err := store.GetDevice(doi2)
		require.NoError(t, err)
		assert.Equal(t, deviceID1, newDevice.DeviceIdentifier)

		assert.Equal(t, serial1, originalDevice.SerialNumber)
		assert.Equal(t, serial1, newDevice.SerialNumber)
	})

	t.Run("Find device by IP and type", func(t *testing.T) {
		// Arrange
		store := NewInMemoryDiscoveredDeviceStore()
		doi1 := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID1,
		}
		device1 := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: "",
				IpAddress:    ipAddress1,
				Port:         "4028",
			},
			Type: deviceType,
		}
		_, err := store.Save(doi1, device1)
		require.NoError(t, err)

		doi2 := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID2,
		}
		device2 := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: "",
				IpAddress:    ipAddress1,
				Port:         "4028",
			},
			Type: deviceType,
		}

		// Act
		savedDevice2, err := store.Save(doi2, device2)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, deviceID1, savedDevice2.DeviceIdentifier)

		device1FromStore, err := store.GetDevice(doi1)
		require.NoError(t, err)
		device2FromStore, err := store.GetDevice(doi2)
		require.NoError(t, err)

		assert.Equal(t, deviceID1, device1FromStore.DeviceIdentifier)
		assert.Equal(t, deviceID1, device2FromStore.DeviceIdentifier)
	})

	t.Run("Update serial number", func(t *testing.T) {
		// Arrange
		store := NewInMemoryDiscoveredDeviceStore()
		doi := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: deviceID1,
		}
		device := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial1,
				IpAddress:    ipAddress1,
			},
			Type: deviceType,
		}
		_, err := store.Save(doi, device)
		require.NoError(t, err)

		updatedDevice := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial2,
				IpAddress:    ipAddress1,
			},
			Type: deviceType,
		}

		_, err = store.Save(doi, updatedDevice)
		require.NoError(t, err)

		doi2 := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: "non-existent-id",
		}
		deviceWithSerial2 := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: serial2,
				IpAddress:    ipAddress2,
			},
			Type: deviceType,
		}

		// Act
		foundDevice, err := store.Save(doi2, deviceWithSerial2)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, deviceID1, foundDevice.DeviceIdentifier)
		assert.Equal(t, serial2, foundDevice.SerialNumber)
		assert.Equal(t, ipAddress2, foundDevice.IpAddress)
	})
}

func TestInMemoryDiscoveredDeviceStore_GetDevice(t *testing.T) {
	// Arrange
	store := NewInMemoryDiscoveredDeviceStore()
	var orgID int64 = 1001

	t.Run("Get non-existent device", func(t *testing.T) {
		// Arrange
		doi := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: "non-existent",
		}

		// Act
		device, err := store.GetDevice(doi)

		// Assert
		require.Error(t, err)
		require.Nil(t, device)
		require.Equal(t, MinerNotFoundFleetError, err)
	})

	t.Run("Get existing device", func(t *testing.T) {
		// Arrange
		doi := DeviceOrgIdentifier{
			OrgID:            orgID,
			DeviceIdentifier: "device1",
		}
		deviceToSave := &DiscoveredDevice{
			Device: pb.Device{
				SerialNumber: "serial1",
				IpAddress:    "192.168.1.1",
				Port:         "4028",
			},
			Type: "antminer",
		}
		_, err := store.Save(doi, deviceToSave)
		require.NoError(t, err)

		// Act
		device, err := store.GetDevice(doi)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, device)
		assert.Equal(t, "device1", device.DeviceIdentifier)
		assert.Equal(t, "serial1", device.SerialNumber)
	})
}
