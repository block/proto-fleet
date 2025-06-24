package models

import (
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
)

type Type int

const (
	// TypeProto represents a Proto miner type.
	TypeProto Type = iota
	// TypeAntminer represents an Antminer miner type by Bitmain.
	TypeAntminer
)

func (t Type) String() string {
	switch t {
	case TypeProto:
		return "proto_miner"
	case TypeAntminer:
		return "antminer"
	default:
		return "unknown"
	}
}

func TypeFromString(s string) (Type, error) {
	switch s {
	case "proto":
		return TypeProto, nil
	case "antminer":
		return TypeAntminer, nil
	default:
		return TypeProto, fleeterror.NewInvalidArgumentErrorf("invalid miner type: %s", s)
	}
}

type PairingInfo struct {
	DeviceID     string
	SerialNumber string
	MacAddress   string
	Model        string
	Manufacturer string
}
