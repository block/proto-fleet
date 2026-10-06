package models

const (
	// DriverNameProto is the driver name for Proto miners, used for plugin routing.
	DriverNameProto = "proto"

	// ProtoDefaultUsername is the nominal username stored for Proto credentials.
	// Proto authenticates by password only, so this is cosmetic but keeps stored
	// username/password credentials structurally complete.
	ProtoDefaultUsername = "admin"
)

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
