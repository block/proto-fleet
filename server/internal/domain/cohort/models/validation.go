package models

import (
	"time"

	telemetrymodels "github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

// CohortFirmwareValidationWindow is one supported before/after comparison preset.
type CohortFirmwareValidationWindow string

const (
	CohortFirmwareValidationWindowOneHour         CohortFirmwareValidationWindow = "one_hour"
	CohortFirmwareValidationWindowSixHours        CohortFirmwareValidationWindow = "six_hours"
	CohortFirmwareValidationWindowTwentyFourHours CohortFirmwareValidationWindow = "twenty_four_hours"
)

// CohortFirmwareValidationState explains whether a comparison can be shown.
type CohortFirmwareValidationState string

const (
	CohortFirmwareValidationStateAvailable             CohortFirmwareValidationState = "available"
	CohortFirmwareValidationStateNoTarget              CohortFirmwareValidationState = "no_target"
	CohortFirmwareValidationStateTargetVersionUnknown  CohortFirmwareValidationState = "target_version_unknown"
	CohortFirmwareValidationStateNoBaseline            CohortFirmwareValidationState = "no_baseline"
	CohortFirmwareValidationStateStabilizing           CohortFirmwareValidationState = "stabilizing"
	CohortFirmwareValidationStateInsufficientTelemetry CohortFirmwareValidationState = "insufficient_telemetry"
	CohortFirmwareValidationStateHistoryExpired        CohortFirmwareValidationState = "history_expired"
)

// CohortFirmwareValidationTelemetryResolution is the common telemetry layer
// used for both comparison windows.
type CohortFirmwareValidationTelemetryResolution string

const (
	CohortFirmwareValidationTelemetryResolutionRaw    CohortFirmwareValidationTelemetryResolution = "raw"
	CohortFirmwareValidationTelemetryResolutionHourly CohortFirmwareValidationTelemetryResolution = "hourly"
)

// CohortFirmwareValidationParams selects one model-specific target and preset.
type CohortFirmwareValidationParams struct {
	OrgID        int64
	CohortID     int64
	Manufacturer string
	Model        string
	Window       CohortFirmwareValidationWindow
}

// CohortFirmwareValidationExclusions accounts for current matching members
// that cannot participate in a trustworthy comparison.
type CohortFirmwareValidationExclusions struct {
	AddedAfterRolloutCount   int32
	UnknownBaselineCount     int32
	AlreadyOnTargetCount     int32
	IncompleteCount          int32
	StabilizingCount         int32
	UntrustedTransitionCount int32
}

// CohortFirmwareValidationPoint is one elapsed-time point in an overlaid series.
type CohortFirmwareValidationPoint struct {
	Elapsed     time.Duration
	Value       float64
	DeviceCount int32
}

// CohortFirmwareValidationMetric compares one telemetry outcome.
type CohortFirmwareValidationMetric struct {
	MeasurementType              telemetrymodels.MeasurementType
	BaselinePoints               []CohortFirmwareValidationPoint
	TargetPoints                 []CohortFirmwareValidationPoint
	BaselineAverage              *float64
	TargetAverage                *float64
	AbsoluteDelta                *float64
	PercentageDelta              *float64
	BaselineReportingDeviceCount int32
	TargetReportingDeviceCount   int32
}

// CohortFirmwareValidationBaseline is one previous-version comparison group.
type CohortFirmwareValidationBaseline struct {
	PreviousFirmwareVersion string
	MemberCount             int32
	EligibleCount           int32
	State                   CohortFirmwareValidationState
	BaselineStartTime       time.Time
	BaselineEndTime         time.Time
	TargetStartTime         time.Time
	TargetEndTime           time.Time
	Metrics                 []CohortFirmwareValidationMetric
}

// CohortFirmwareValidation is the complete model-specific comparison response.
type CohortFirmwareValidation struct {
	State                 CohortFirmwareValidationState
	Manufacturer          string
	Model                 string
	TargetFirmwareFileID  string
	TargetFirmwareVersion string
	RolloutStartedAt      time.Time
	Window                CohortFirmwareValidationWindow
	StabilizationGap      time.Duration
	ChartGranularity      time.Duration
	TelemetryResolution   CohortFirmwareValidationTelemetryResolution
	TargetedCount         int32
	CompleteCount         int32
	Preliminary           bool
	Exclusions            CohortFirmwareValidationExclusions
	Baselines             []CohortFirmwareValidationBaseline
}
