package minerdiscovery

import (
	"sync"
	"time"
)

// DiscoveredDeviceStore defines the interface for storing and retrieving discovered devices
type DiscoveredDeviceStore interface {
	// Save stores a discovered device and returns the updated device
	Save(doi DeviceOrgIdentifier, in *DiscoveredDevice) (*DiscoveredDevice, error)
	// GetDevice retrieves a device by its organization and device identifier
	GetDevice(doi DeviceOrgIdentifier) (*DiscoveredDevice, error)
}

type InMemoryDiscoveredDeviceStore struct {
	mu       sync.RWMutex
	devices  map[DeviceOrgIdentifier]*DiscoveredDevice
	bySerial map[string]*DiscoveredDevice
}

// Ensure InMemoryDevicesStore implements DeviceStore interface
var _ DiscoveredDeviceStore = (*InMemoryDiscoveredDeviceStore)(nil)

func NewInMemoryDiscoveredDeviceStore() *InMemoryDiscoveredDeviceStore {
	return &InMemoryDiscoveredDeviceStore{
		devices:  make(map[DeviceOrgIdentifier]*DiscoveredDevice),
		bySerial: make(map[string]*DiscoveredDevice),
	}
}

func (s *InMemoryDiscoveredDeviceStore) Save(doi DeviceOrgIdentifier, in *DiscoveredDevice) (*DiscoveredDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	serialNum := in.SerialNumber

	if serialNum != "" {
		if existingDevice, exists := s.bySerial[serialNum]; exists && existingDevice.GetDeviceOrgIdentifier() != in.GetDeviceOrgIdentifier() {
			// remove stale entry
			delete(s.bySerial, serialNum)
		}
	}

	device, exists := s.devices[doi]
	if !exists {
		device = &DiscoveredDevice{FirstDiscovered: now}
		s.devices[doi] = device
	}

	// Update serial number mapping if changed
	if device.SerialNumber != serialNum {
		if device.SerialNumber != "" {
			delete(s.bySerial, device.SerialNumber)
		}
		if serialNum != "" {
			s.bySerial[serialNum] = device
		}
		device.SerialNumber = serialNum
	}

	device.MacAddress = in.MacAddress
	device.Model = in.Model
	device.Manufacturer = in.Manufacturer
	device.Type = in.Type
	device.Port = in.Port
	device.IpAddress = in.IpAddress
	device.OrgID = doi.OrgID
	device.DeviceIdentifier = doi.DeviceIdentifier
	device.LastSeen = now

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
