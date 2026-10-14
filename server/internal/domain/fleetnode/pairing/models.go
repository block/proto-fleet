package pairing

import (
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"

	domainpairing "github.com/block/proto-fleet/server/internal/domain/pairing"
)

// device_pairing.pairing_status values this package writes, aliased from the
// canonical constants so the two pairing paths can't drift.
const (
	StatusPaired               = domainpairing.StatusPaired
	StatusUnpaired             = domainpairing.StatusUnpaired
	StatusAuthenticationNeeded = domainpairing.StatusAuthenticationNeeded
	StatusDefaultPassword      = domainpairing.StatusDefaultPassword
	StatusFailed               = domainpairing.StatusFailed
)

type DiscoveredDeviceReport struct {
	RPCPort          int32
	RPCTLS           bool
	DeviceIdentifier string
	IPAddress        string
	Port             string
	URLScheme        string
	DriverName       string
	DeviceClass      commonpb.DeviceClass
	Model            string
	Manufacturer     string
	FirmwareVersion  string
}

type FleetNodeDevice struct {
	Manufacturer     string
	Model            string
	FleetNodeID      int64
	DeviceID         int64
	DeviceIdentifier string
	DeviceType       string
	AssignedAt       time.Time
	AssignedBy       *int64
}

// FleetNodeDiscoveredDevice is a device available to a Fleet Node for pairing
// or credential retry. PairingStatus is empty when never attempted, or
// "AUTHENTICATION_NEEDED" after pairing or recovery needs new credentials.
type FleetNodeDiscoveredDevice struct {
	RPCPort          int32
	RPCTLS           bool
	ID               int64
	FleetNodeID      int64
	DeviceIdentifier string
	IPAddress        string
	Port             string
	URLScheme        string
	DriverName       string
	DeviceClass      commonpb.DeviceClass
	Model            string
	Manufacturer     string
	FirmwareVersion  string
	LastSeen         time.Time
	PairingStatus    string
}
