package interfaces

import (
	"context"
	"time"
)

const (
	CurtailmentScopeWholeOrg   = "whole_org"
	CurtailmentScopeDeviceSets = "device_sets"
	CurtailmentScopeDeviceList = "device_list"
)

type CurtailmentPreviewDeviceParams struct {
	OrgID             int64
	ScopeType         string
	DeviceSetIDs      []int64
	DeviceIdentifiers []string
	CooldownSince     time.Time
}

type CurtailmentPreviewDevice struct {
	DeviceID            int64
	DeviceIdentifier    string
	Manufacturer        string
	Model               string
	FirmwareVersion     string
	DriverName          string
	PairingStatus       string
	DeviceStatus        *string
	LatestMetricAt      *time.Time
	CurrentPowerW       *float64
	RecentPowerW        *float64
	RecentHashRateHS    *float64
	EfficiencyJH        *float64
	InActiveCurtailment bool
	InCooldown          bool
}

type CurtailmentStore interface {
	ListValidDeviceSetIDs(ctx context.Context, orgID int64, deviceSetIDs []int64) ([]int64, error)
	ListPreviewDevices(ctx context.Context, params CurtailmentPreviewDeviceParams) ([]CurtailmentPreviewDevice, error)
}
