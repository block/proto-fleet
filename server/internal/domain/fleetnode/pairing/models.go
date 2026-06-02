package pairing

import "time"

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
