package models

// DriverNameProto is the driver name for Proto miners, used for plugin routing.
const DriverNameProto = "proto"

// DeviceIdentifier represents a unique identifier for a mining device
type DeviceIdentifier string

func (d DeviceIdentifier) String() string {
	return string(d)
}

type PairingInfo struct {
	DeviceID     DeviceIdentifier
	SerialNumber string
	MacAddress   string
	Model        string
	Manufacturer string
}
