// Package models holds the domain types for infrastructure devices
// (fans, sensors, PDUs, and other non-miner hardware).
package models

import "time"

// DeviceType mirrors the proto enum and the SMALLINT stored in
// infra_device.device_type. Re-declared as a typed constant set so the
// domain layer is independent of the proto package.
type DeviceType int16

const (
	DeviceTypeUnspecified DeviceType = 0
	DeviceTypeFan        DeviceType = 1
	DeviceTypeSensor     DeviceType = 2
	DeviceTypePDU        DeviceType = 3
)

// Valid reports whether the value matches one of the defined enum
// members. Used to reject malformed proto inputs at the service edge.
func (d DeviceType) Valid() bool {
	return d >= DeviceTypeUnspecified && d <= DeviceTypePDU
}

// DeviceStatus mirrors the proto enum for the operational status of an
// infrastructure device.
type DeviceStatus int16

const (
	DeviceStatusUnspecified DeviceStatus = 0
	DeviceStatusOnline     DeviceStatus = 1
	DeviceStatusDegraded   DeviceStatus = 2
	DeviceStatusOffline    DeviceStatus = 3
)

// Valid reports whether the value matches one of the defined enum members.
func (s DeviceStatus) Valid() bool {
	return s >= DeviceStatusUnspecified && s <= DeviceStatusOffline
}

// ControlMode mirrors the proto enum for how the device is managed
// (fleet software, PLC, or a hybrid of both).
type ControlMode int16

const (
	ControlModeUnspecified ControlMode = 0
	ControlModeFleet      ControlMode = 1
	ControlModePLC        ControlMode = 2
	ControlModeHybrid     ControlMode = 3
)

// Valid reports whether the value matches one of the defined enum members.
func (c ControlMode) Valid() bool {
	return c >= ControlModeUnspecified && c <= ControlModeHybrid
}

// InfraDevice is the canonical domain shape for an infrastructure
// device row.
type InfraDevice struct {
	ID           int64
	OrgID        int64
	Name         string
	DeviceType   int16
	Subtype      *string
	SiteID       *int64
	SiteName     string
	BuildingID   *int64
	BuildingName string
	IPAddress    *string
	Status       int16
	ControlMode  int16
	RPM          *float64
	Protocol     *string
	LastSeen     *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// InfraDeviceStats is the aggregate rollup returned by GetStats.
type InfraDeviceStats struct {
	TotalCount     int32
	OnlineCount    int32
	DegradedCount  int32
	OfflineCount   int32
	BuildingsCount int32
}

// CreateParams is the input shape for the infra device create flow.
type CreateParams struct {
	OrgID       int64
	Name        string
	DeviceType  int16
	Subtype     *string
	SiteID      *int64
	BuildingID  *int64
	IPAddress   *string
	Status      int16
	ControlMode int16
	RPM         *float64
	Protocol    *string
}

// UpdateParams is the input shape for infra device updates. Only the
// fields the caller intends to change are set; the store layer applies
// partial-update semantics.
type UpdateParams struct {
	ID          int64
	OrgID       int64
	Name        *string
	IPAddress   *string
	ControlMode *int16
}

// ListFilter selects which infra devices to return. All fields are
// optional narrowing clauses; an empty filter returns every live device
// in the org.
type ListFilter struct {
	OrgID      int64
	SiteID     *int64
	BuildingID *int64
	DeviceType *int16
	Status     *int16
}

// PairEntry carries one device from a bulk-pair request. Typically
// produced from a network scan result that the operator confirmed.
type PairEntry struct {
	Name        string
	DeviceType  int16
	Subtype     *string
	SiteID      *int64
	BuildingID  *int64
	IPAddress   *string
	Status      int16
	ControlMode int16
	Protocol    *string
}

// DiscoveredDevice represents a device found during a network scan
// that has not yet been paired into the fleet.
type DiscoveredDevice struct {
	IPAddress  string
	MACAddress *string
	Hostname   *string
	DeviceType int16
	Protocol   *string
}
