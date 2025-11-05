package models

import (
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pairing/v1"
)

// DeviceOrgIdentifier uniquely identifies a device within an organization
type DeviceOrgIdentifier struct {
	DeviceIdentifier string
	OrgID            int64
}

// DiscoveredDevice represents a device that has been discovered on the network
type DiscoveredDevice struct {
	pb.Device
	IsActive        bool
	OrgID           int64
	FirstDiscovered time.Time
	LastSeen        time.Time
}

// GetDeviceOrgIdentifier returns the device organization identifier
func (d *DiscoveredDevice) GetDeviceOrgIdentifier() DeviceOrgIdentifier {
	return DeviceOrgIdentifier{
		DeviceIdentifier: d.Device.DeviceIdentifier,
		OrgID:            d.OrgID,
	}
}

func (d *DiscoveredDevice) UpdateNetworkInfo(ipAddress string, port string, urlScheme string) {
	d.IpAddress = ipAddress
	d.Port = port
	d.UrlScheme = urlScheme
}
