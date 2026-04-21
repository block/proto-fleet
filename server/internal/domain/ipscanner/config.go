package ipscanner

import "time"

type Config struct {
	// Feature flag to enable/disable IP scanning
	Enabled bool `help:"Enable automatic IP address scanning for offline devices." default:"true" env:"IP_SCANNER_ENABLED"`

	// Scan interval controls how often to scan for offline devices
	ScanInterval time.Duration `help:"Interval at which to scan for offline devices." default:"5m" env:"IP_SCANNER_SCAN_INTERVAL"`

	// Concurrency controls
	MaxConcurrentSubnetScans      int           `help:"Maximum number of subnet scans to run concurrently." default:"5" env:"IP_SCANNER_MAX_CONCURRENT_SUBNET_SCANS"`
	MaxConcurrentIPScansPerSubnet int           `help:"Maximum number of concurrent IP scans per subnet." default:"20" env:"IP_SCANNER_MAX_CONCURRENT_IP_SCANS_PER_SUBNET"`
	ScanTimeout                   time.Duration `help:"Timeout for a single IP scan attempt." default:"30s" env:"IP_SCANNER_SCAN_TIMEOUT"`

	// Network scanning controls
	SubnetMaskBits int `help:"Subnet mask bits for IPv4 network scanning (e.g., 24 for /24, 16 for /16)." default:"24" env:"IP_SCANNER_SUBNET_MASK_BITS"`
}
