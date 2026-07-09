// Package models holds the domain types for infrastructure devices.
package models

import (
	"encoding/json"
	"time"
)

// Device kinds. Stored as text in infrastructure_device.device_kind
// and mirrored as strings on the proto so new kinds (dampers, pumps)
// need no wire change.
const (
	KindSingleFan = "single_fan"
	KindFanGroup  = "fan_group"
)

// ValidKind reports whether the supplied device kind is known.
func ValidKind(kind string) bool {
	return kind == KindSingleFan || kind == KindFanGroup
}

// Device is the canonical domain shape for an infrastructure device
// row. DriverConfig is opaque to the core; only the driver adapter
// matching DriverType interprets it.
type Device struct {
	ID           int64
	OrgID        int64
	SiteID       int64
	SiteLabel    string
	BuildingName string
	Name         string
	DeviceKind   string
	FanCount     int32
	Enabled      bool
	DriverType   string
	DriverConfig json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateParams is the input shape for device creation.
type CreateParams struct {
	OrgID        int64
	SiteID       int64
	BuildingName string
	Name         string
	DeviceKind   string
	FanCount     int32
	Enabled      bool
	DriverType   string
	DriverConfig json.RawMessage
}

// UpdateParams is the input shape for device updates.
type UpdateParams struct {
	OrgID        int64
	ID           int64
	SiteID       int64
	BuildingName string
	Name         string
	DeviceKind   string
	FanCount     int32
	Enabled      bool
	DriverType   string
	DriverConfig json.RawMessage
}

// ListFilter selects the list scope.
type ListFilter struct {
	OrgID   int64
	SiteIDs []int64
}
