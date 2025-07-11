package commandtype

// own package due to cyclic imports between command and queue packages

import (
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
)

type Type int

// don't forget to declare GetMinerCommandFunc for a new Type
const (
	// StartMining represents a command to begin mining operations
	StartMining Type = iota
	// StopMining represents a command to halt mining operations
	StopMining
	SetCoolingMode
)

func (t *Type) String() string {
	switch *t {
	case StartMining:
		return "StartMining"
	case StopMining:
		return "StopMining"
	case SetCoolingMode:
		return "SetCoolingMode"
	default:
		return "Undefined"
	}
}

func FromString(s string) (Type, error) {
	switch s {
	case "StartMining":
		return StartMining, nil
	case "StopMining":
		return StopMining, nil
	case "SetCoolingMode":
		return SetCoolingMode, nil

	default:
		return Type(-1), fleeterror.NewInternalErrorf("invalid command type: %s", s)
	}
}

func (t *Type) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t *Type) UnmarshalText(text []byte) error {
	val, err := FromString(string(text))
	if err != nil {
		return err
	}
	*t = val
	return nil
}
