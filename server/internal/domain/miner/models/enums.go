package models

import "fmt"

type MinerStatus int

// MinerStatus constants define the overall status of a mining device
const (
	MinerStatusUnknown MinerStatus = iota
	MinerStatusActive
	MinerStatusOffline
	MinerStatusInactive
	MinerStatusMaintenance
	MinerStatusError
	MinerStatusNeedsMiningPool
)

func (m MinerStatus) String() string {
	switch m {
	case MinerStatusUnknown:
		return "unknown"
	case MinerStatusActive:
		return "active"
	case MinerStatusOffline:
		return "offline"
	case MinerStatusInactive:
		return "inactive"
	case MinerStatusMaintenance:
		return "maintenance"
	case MinerStatusError:
		return "error"
	case MinerStatusNeedsMiningPool:
		return "needs_mining_pool"
	default:
		return "unknown"
	}
}

func (m MinerStatus) Parse(s string) (MinerStatus, error) {
	switch s {
	case "unknown":
		return MinerStatusUnknown, nil
	case "active":
		return MinerStatusActive, nil
	case "offline":
		return MinerStatusOffline, nil
	case "inactive":
		return MinerStatusInactive, nil
	case "maintenance":
		return MinerStatusMaintenance, nil
	case "error":
		return MinerStatusError, nil
	case "needs_mining_pool":
		return MinerStatusNeedsMiningPool, nil
	default:
		return MinerStatusUnknown, fmt.Errorf("unknown miner status: %s", s)
	}
}
