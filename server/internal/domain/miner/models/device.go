package models

import (
	"fmt"
	"strings"
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
	TypeVirtual
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
	case TypeVirtual:
		return "virtual"
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
	case "asic":
		return TypeAntminer, nil
	case "whatsminer":
		return TypeWhatsminer, nil
	case "avalon":
		return TypeAvalon, nil
	case "virtual":
		return TypeVirtual, nil
	case "unknown", "":
		return TypeUnknown, nil
	default:
		return TypeUnknown, fmt.Errorf("unknown miner type: %s", s)
	}
}

// TypeFromDeviceInfo converts type string and model to Type enum, using model to disambiguate "asic" type.
// TODO: Replace Type enum with model-based plugin routing system
func TypeFromDeviceInfo(typeStr, model string) (Type, error) {
	// When type is ambiguous "asic", use model to determine the actual miner type
	if strings.ToLower(typeStr) == "asic" {
		modelLower := strings.ToLower(model)
		if strings.HasPrefix(modelLower, "rig") {
			return TypeProto, nil
		}
		if strings.HasPrefix(modelLower, "antminer") {
			return TypeAntminer, nil
		}
		if strings.HasPrefix(modelLower, "whatsminer") {
			return TypeWhatsminer, nil
		}
		if strings.HasPrefix(modelLower, "avalon") {
			return TypeAvalon, nil
		}
		return TypeUnknown, fmt.Errorf("unknown ASIC model: %s", model)
	}
	// For non-asic types, use the existing TypeFromString logic
	return TypeFromString(typeStr)
}

// ParseDeviceTypeOrUnknown parses device type using TypeFromDeviceInfo and returns TypeUnknown on error.
// This is useful when type parsing failure should not halt execution.
func ParseDeviceTypeOrUnknown(typeStr, model string) Type {
	deviceType, err := TypeFromDeviceInfo(typeStr, model)
	if err != nil {
		return TypeUnknown
	}
	return deviceType
}

type PairingInfo struct {
	DeviceID     DeviceIdentifier
	SerialNumber string
	MacAddress   string
	Model        string
	Manufacturer string
}
