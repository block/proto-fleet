package minerdiscovery

import (
	"sync"
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

// DiscoveredDeviceStore defines the interface for storing and retrieving discovered devices
type DiscoveredDeviceStore interface {
	// Save stores a discovered device and returns the updated device
	Save(doi DeviceOrgIdentifier, in *DiscoveredDevice) (*DiscoveredDevice, error)
	// GetDevice retrieves a device by its organization and device identifier
	GetDevice(doi DeviceOrgIdentifier) (*DiscoveredDevice, error)
}

type Pair[A comparable, B comparable] struct {
	A A
	B B
}

type InMemoryDiscoveredDeviceStore struct {
	mu       sync.RWMutex
	devices  map[DeviceOrgIdentifier]*DiscoveredDevice
	bySerial map[string]*DiscoveredDevice
	// non proto miners won't have a serial number or mac address at this point,
	// so if we already have a device with the same ip and type, we update it
	byIPAndType map[Pair[string, string]]*DiscoveredDevice
}

// Ensure InMemoryDevicesStore implements DeviceStore interface
var _ DiscoveredDeviceStore = (*InMemoryDiscoveredDeviceStore)(nil)

func NewInMemoryDiscoveredDeviceStore() *InMemoryDiscoveredDeviceStore {
	return &InMemoryDiscoveredDeviceStore{
		devices:     make(map[DeviceOrgIdentifier]*DiscoveredDevice),
		bySerial:    make(map[string]*DiscoveredDevice),
		byIPAndType: make(map[Pair[string, string]]*DiscoveredDevice),
	}
}

func (s *InMemoryDiscoveredDeviceStore) Save(doi DeviceOrgIdentifier, in *DiscoveredDevice) (*DiscoveredDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	serialNum := in.SerialNumber
	ipTypePair := Pair[string, string]{A: in.IpAddress, B: in.Type}

	// Get existing device by identifier
	device, exists := s.devices[doi]
	var originalDOI DeviceOrgIdentifier

	// Try to find device by "secondary indices" if not found by identifier
	if !exists {
		if serialNum != "" {
			device, exists = s.bySerial[serialNum]
		}

		if !exists {
			device, exists = s.byIPAndType[ipTypePair]
		}

		if exists {
			originalDOI = DeviceOrgIdentifier{
				DeviceIdentifier: device.DeviceIdentifier,
				OrgID:            device.OrgID,
			}
		}
	}

	// Update serial number mapping if changed
	if exists && device.SerialNumber != serialNum {
		if device.SerialNumber != "" {
			delete(s.bySerial, device.SerialNumber)
		}
	}

	// Update ip and type mapping if changed
	if exists && (device.IpAddress != in.IpAddress || device.Type != in.Type) {
		oldPair := Pair[string, string]{A: device.IpAddress, B: device.Type}
		delete(s.byIPAndType, oldPair)
	}

	if !exists {
		// Create device with supplied identifier
		device = &DiscoveredDevice{
			Device: pb.Device{
				DeviceIdentifier: doi.DeviceIdentifier,
			},
			OrgID:           doi.OrgID,
			FirstDiscovered: now,
		}
	}

	// Update common fields for both new and existing devices
	device.MacAddress = in.MacAddress
	device.SerialNumber = serialNum
	device.Model = in.Model
	device.Manufacturer = in.Manufacturer
	device.Type = in.Type
	device.Port = in.Port
	device.UrlScheme = in.UrlScheme
	device.IpAddress = in.IpAddress
	device.OrgID = doi.OrgID
	device.LastSeen = now

	// Store device in maps
	s.devices[doi] = device

	// If found by secondary index, also keep the original mapping
	if exists && originalDOI.DeviceIdentifier != "" && originalDOI.DeviceIdentifier != doi.DeviceIdentifier {
		s.devices[originalDOI] = device
	}

	s.byIPAndType[ipTypePair] = device
	if serialNum != "" {
		s.bySerial[serialNum] = device
	}

	return device, nil
}

func (s *InMemoryDiscoveredDeviceStore) GetDevice(doi DeviceOrgIdentifier) (*DiscoveredDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if dev, ok := s.devices[doi]; ok {
		return dev, nil
	}

	return nil, MinerNotFoundFleetError
}
