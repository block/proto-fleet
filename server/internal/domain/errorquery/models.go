// Package errorquery provides error querying and management capabilities for mining devices.
package errorquery

import (
	"time"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
)

// ErrorRecord represents an error instance in the internal domain model.
type ErrorRecord struct {
	ErrorID           string
	MinerError        errorsv1.MinerError
	Summary           string // Human-readable description for client modal display
	CauseSummary      string
	RecommendedAction string
	Severity          errorsv1.Severity
	FirstSeenAt       time.Time
	LastSeenAt        time.Time
	ClosedAt          *time.Time
	VendorAttributes  map[string]string
	DeviceID          int64
	ComponentID       string // Format: "{deviceID}_{type}_{index}" or empty for device-level errors
	Impact            string
}

// SeedData represents seed data for a device's errors.
// FOR TESTING/DEVELOPMENT ONLY - used to inject deterministic error data.
type SeedData struct {
	DeviceID   int64
	DeviceType string
	Errors     []ErrorRecord
}

// DeviceInfo holds device information retrieved from the device store.
type DeviceInfo struct {
	DeviceID   int64
	DeviceType string
	OrgID      int64
}
