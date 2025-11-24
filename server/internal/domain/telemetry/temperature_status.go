package telemetry

import (
	"time"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Temperature threshold constants in Celsius
const (
	// Temperature thresholds for status categorization
	TempColdMaxC     = 0.0  // Below 0°C = COLD
	TempOkMinC       = 0.0  // 0°C to 70°C = OK
	TempOkMaxC       = 70.0 // Upper bound for OK status
	TempHotMinC      = 70.0 // 70°C to 90°C = HOT
	TempHotMaxC      = 90.0 // Upper bound for HOT status
	TempCriticalMinC = 90.0 // Above 90°C = CRITICAL
)

// GetTemperatureStatus determines the temperature status based on the value in Celsius
func GetTemperatureStatus(tempC float64) pb.TemperatureStatus {
	if tempC < TempColdMaxC {
		return pb.TemperatureStatus_TEMPERATURE_STATUS_COLD
	} else if tempC <= TempOkMaxC {
		return pb.TemperatureStatus_TEMPERATURE_STATUS_OK
	} else if tempC <= TempHotMaxC {
		return pb.TemperatureStatus_TEMPERATURE_STATUS_HOT
	}
	return pb.TemperatureStatus_TEMPERATURE_STATUS_CRITICAL
}

// TemperatureStatusTracker tracks the count of devices in each temperature status
type TemperatureStatusTracker struct {
	counts map[pb.TemperatureStatus]int32
	total  int32
}

// NewTemperatureStatusTracker creates a new temperature status tracker
func NewTemperatureStatusTracker() *TemperatureStatusTracker {
	return &TemperatureStatusTracker{
		counts: make(map[pb.TemperatureStatus]int32),
		total:  0,
	}
}

// Update adds a device temperature to the tracker
func (t *TemperatureStatusTracker) Update(tempC float64) {
	status := GetTemperatureStatus(tempC)
	t.counts[status]++
	t.total++
}

// GetCounts returns a single temperature status count with all status counts
func (t *TemperatureStatusTracker) GetCounts(timestamp time.Time) *pb.TemperatureStatusCount {
	return &pb.TemperatureStatusCount{
		Timestamp:     timestamppb.New(timestamp),
		ColdCount:     t.counts[pb.TemperatureStatus_TEMPERATURE_STATUS_COLD],
		OkCount:       t.counts[pb.TemperatureStatus_TEMPERATURE_STATUS_OK],
		HotCount:      t.counts[pb.TemperatureStatus_TEMPERATURE_STATUS_HOT],
		CriticalCount: t.counts[pb.TemperatureStatus_TEMPERATURE_STATUS_CRITICAL],
	}
}

// Reset clears the tracker counts
func (t *TemperatureStatusTracker) Reset() {
	t.counts = make(map[pb.TemperatureStatus]int32)
	t.total = 0
}

// TemperatureStatusToString converts a TemperatureStatus enum to its string representation
func TemperatureStatusToString(status pb.TemperatureStatus) string {
	switch status {
	case pb.TemperatureStatus_TEMPERATURE_STATUS_UNSPECIFIED:
		return "unspecified"
	case pb.TemperatureStatus_TEMPERATURE_STATUS_COLD:
		return "cold"
	case pb.TemperatureStatus_TEMPERATURE_STATUS_OK:
		return "ok"
	case pb.TemperatureStatus_TEMPERATURE_STATUS_HOT:
		return "hot"
	case pb.TemperatureStatus_TEMPERATURE_STATUS_CRITICAL:
		return "critical"
	default:
		return "unspecified"
	}
}

// GetTemperatureStatusString returns the temperature status as a string for a given temperature in Celsius
// This is a convenience function that combines GetTemperatureStatus and TemperatureStatusToString
func GetTemperatureStatusString(tempC float64) string {
	status := GetTemperatureStatus(tempC)
	return TemperatureStatusToString(status)
}
