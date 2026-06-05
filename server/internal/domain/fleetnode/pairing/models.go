package pairing

import "time"

// device_pairing.pairing_status values this package writes. Mirror the
// pairing_status_enum DB type (and internal/domain/pairing's constants).
const (
	StatusPaired               = "PAIRED"
	StatusAuthenticationNeeded = "AUTHENTICATION_NEEDED"
	StatusFailed               = "FAILED"
)

type DiscoveredDeviceReport struct {
	DeviceIdentifier string
	IPAddress        string
	Port             string
	URLScheme        string
	DriverName       string
	Model            string
	Manufacturer     string
	FirmwareVersion  string
}

type FleetNodeDevice struct {
	FleetNodeID      int64
	DeviceID         int64
	DeviceIdentifier string
	DeviceType       string
	AssignedAt       time.Time
	AssignedBy       *int64
}

// FleetNodeDiscoveredDevice is a device a fleet node discovered that is not yet
// paired to it. PairingStatus is empty when never attempted, or
// "AUTHENTICATION_NEEDED" after a pair attempt that needs credentials.
type FleetNodeDiscoveredDevice struct {
	ID               int64
	FleetNodeID      int64
	DeviceIdentifier string
	IPAddress        string
	Port             string
	URLScheme        string
	DriverName       string
	Model            string
	Manufacturer     string
	FirmwareVersion  string
	LastSeen         time.Time
	PairingStatus    string
}
