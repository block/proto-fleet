package models

import sdk "github.com/block/proto-fleet/server/sdk/v1"

// MinerStatus is shared with plugin observations of boards and modules.
type MinerStatus = sdk.MinerStatus

const (
	MinerStatusUnknown         = sdk.MinerStatusUnknown
	MinerStatusActive          = sdk.MinerStatusActive
	MinerStatusOffline         = sdk.MinerStatusOffline
	MinerStatusInactive        = sdk.MinerStatusInactive
	MinerStatusMaintenance     = sdk.MinerStatusMaintenance
	MinerStatusError           = sdk.MinerStatusError
	MinerStatusNeedsMiningPool = sdk.MinerStatusNeedsMiningPool
	MinerStatusUpdating        = sdk.MinerStatusUpdating
	MinerStatusRebootRequired  = sdk.MinerStatusRebootRequired
)
