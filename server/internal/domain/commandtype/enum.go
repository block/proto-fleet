package commandtype

// own package due to cyclic imports between command and queue packages

import (
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type Type int

// don't forget to declare GetMinerCommandFunc for a new Type
const (
	// StartMining represents a command to begin mining operations
	StartMining Type = iota
	// StopMining represents a command to halt mining operations
	StopMining
	SetCoolingMode
	SetPowerTarget
	UpdateMiningPools
	DownloadLogs
	Reboot
	BlinkLED
	FirmwareUpdate
	// Unpair represents a command to unpair a device from the fleet
	Unpair
	// UpdateMinerPassword represents a command to update miner web UI password
	UpdateMinerPassword
)

func (t *Type) String() string {
	switch *t {
	case StartMining:
		return "StartMining"
	case StopMining:
		return "StopMining"
	case SetCoolingMode:
		return "SetCoolingMode"
	case SetPowerTarget:
		return "SetPowerTarget"
	case UpdateMiningPools:
		return "UpdateMiningPools"
	case DownloadLogs:
		return "DownloadLogs"
	case Reboot:
		return "Reboot"
	case BlinkLED:
		return "BlinkLED"
	case FirmwareUpdate:
		return "FirmwareUpdate"
	case Unpair:
		return "Unpair"
	case UpdateMinerPassword:
		return "UpdateMinerPassword"

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
	case "SetPowerTarget":
		return SetPowerTarget, nil
	case "UpdateMiningPools":
		return UpdateMiningPools, nil
	case "DownloadLogs":
		return DownloadLogs, nil
	case "Reboot":
		return Reboot, nil
	case "BlinkLED":
		return BlinkLED, nil
	case "FirmwareUpdate":
		return FirmwareUpdate, nil
	case "Unpair":
		return Unpair, nil
	case "UpdateMinerPassword":
		return UpdateMinerPassword, nil

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
