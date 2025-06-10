package miner

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/infrastructure/networking"
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
		return "proto"
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

type Miner interface {
	// Basic identification
	GetType() Type
	GetIdentifier() string
	GetConnectionInfo() networking.ConnectionInfo

	// Mining operations
	StartMining(ctx context.Context) error
	StopMining(ctx context.Context) error

	// System operations
	GetPairingInfo(ctx context.Context) (*PairingInfo, error)
}

type PairingInfo struct {
	DeviceID     string
	SerialNumber string
	MacAddress   string
	Model        string
	Manufacturer string
}
