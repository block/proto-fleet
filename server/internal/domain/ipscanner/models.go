package ipscanner

// TargetDevice represents a device we're looking for during a subnet scan
type TargetDevice struct {
	DeviceID                   int64
	DeviceIdentifier           string
	DiscoveredDeviceIdentifier string
	DeviceMAC                  string
	DriverName                 string
	Port                       string
	URLScheme                  string
	OrgID                      int64
}

// SubnetScanTask represents a task to scan a subnet for multiple devices
type SubnetScanTask struct {
	Subnet        string
	TargetDevices []TargetDevice
}

// DeviceMatch represents a device that was found during scanning
type DeviceMatch struct {
	TargetDevice   TargetDevice
	DiscoveredIP   string
	DiscoveredPort string
	URLScheme      string
}

// SubnetScanResult represents the result of scanning a subnet
type SubnetScanResult struct {
	Subnet  string
	Matches []DeviceMatch
	Error   error
}
