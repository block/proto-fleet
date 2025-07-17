package models

import (
	"fmt"
)

// DeviceIdentifier represents a unique identifier for a mining device
type DeviceIdentifier string

func (d DeviceIdentifier) String() string {
	return string(d)
}

// Type represents the type of mining device
type Type int

// Possible types of mining devices
const (
	TypeUnknown Type = iota
	TypeAntminer
	TypeProto
	TypeWhatsminer
	TypeAvalon
)

func (t Type) String() string {
	switch t {
	case TypeUnknown:
		return "unknown"
	case TypeAntminer:
		return "antminer"
	case TypeProto:
		return "proto"
	case TypeWhatsminer:
		return "whatsminer"
	case TypeAvalon:
		return "avalon"
	default:
		return "unknown"
	}
}

// TypeFromString converts a string to Type enum
func TypeFromString(s string) (Type, error) {
	switch s {
	case "antminer":
		return TypeAntminer, nil
	case "proto":
		return TypeProto, nil
	case "proto_miner": // Legacy format support
		return TypeProto, nil
	case "whatsminer":
		return TypeWhatsminer, nil
	case "avalon":
		return TypeAvalon, nil
	case "unknown", "":
		return TypeUnknown, nil
	default:
		return TypeUnknown, fmt.Errorf("unknown miner type: %s", s)
	}
}

type PairingInfo struct {
	DeviceID     DeviceIdentifier
	SerialNumber string
	MacAddress   string
	Model        string
	Manufacturer string
}
