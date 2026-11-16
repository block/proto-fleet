package commandtype

// own package due to cyclic imports between command and queue packages

import (
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type Type int

// don't forget to add a dispatch arm in command/execution_service.go
// executeCommandOnDevice for any new Type.
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
	// Curtail transitions the device to the curtailment level in the payload.
	Curtail
	// Uncurtail restores the device to its pre-curtailment mining state.
	Uncurtail
	// ApplyCurtailmentConfig replaces the rig-local fallback configuration.
	ApplyCurtailmentConfig
	SetContainerControl
	SetTankMining
	SetModuleMining
	ContainerMining
	// SetBoardIdentify sets one verified CT1 board's identification LED.
	SetBoardIdentify
	// FrigateServerUpdate admits one Frigate server update call (staging,
	// start, retry, cancel) against a controller's current binding. It is
	// dispatched directly, never through the command queue.
	FrigateServerUpdate
	// ContainerAlarmAction sends one CT1 plant alarm mute or reset command. It
	// is a physical plant write and is never retried.
	ContainerAlarmAction
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
	case Curtail:
		return "Curtail"
	case Uncurtail:
		return "Uncurtail"
	case ContainerMining:
		return "ContainerMining"
	case SetModuleMining:
		return "SetModuleMining"
	case SetBoardIdentify:
		return "SetBoardIdentify"
	case SetTankMining:
		return "SetTankMining"
	case SetContainerControl:
		return "SetContainerControl"
	case ApplyCurtailmentConfig:
		return "ApplyCurtailmentConfig"
	case FrigateServerUpdate:
		return "FrigateServerUpdate"
	case ContainerAlarmAction:
		return "ContainerAlarmAction"

	default:
		return "Undefined"
	}
}

// IsCurtailmentControl identifies host fleet operations independent of pool ownership.
func (t *Type) IsCurtailmentControl() bool {
	return *t == Curtail || *t == Uncurtail || *t == ApplyCurtailmentConfig
}

// MayAffectPools treats unknown commands as conflicting. Only commands that are
// independent of pool configuration and reconciliation may overlap pool work.
func (t *Type) MayAffectPools() bool {
	switch *t {
	case BlinkLED, SetBoardIdentify, DownloadLogs, SetCoolingMode, SetPowerTarget:
		return false
	case FrigateServerUpdate:
		// Frigate preserves pools across controller updates. Pool work must
		// not prevent the retry, cancel or abort calls that restore controllers.
		return false
	case StartMining, StopMining, UpdateMiningPools, Reboot, FirmwareUpdate, Unpair,
		UpdateMinerPassword, Curtail, Uncurtail, ApplyCurtailmentConfig,
		SetContainerControl, ContainerAlarmAction, SetTankMining, SetModuleMining, ContainerMining:
		return true
	default:
		return true
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
	case "Curtail":
		return Curtail, nil
	case "Uncurtail":
		return Uncurtail, nil
	case "ContainerMining":
		return ContainerMining, nil
	case "SetModuleMining":
		return SetModuleMining, nil
	case "SetBoardIdentify":
		return SetBoardIdentify, nil
	case "SetTankMining":
		return SetTankMining, nil
	case "SetContainerControl":
		return SetContainerControl, nil
	case "ApplyCurtailmentConfig":
		return ApplyCurtailmentConfig, nil
	case "FrigateServerUpdate":
		return FrigateServerUpdate, nil
	case "ContainerAlarmAction":
		return ContainerAlarmAction, nil

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
