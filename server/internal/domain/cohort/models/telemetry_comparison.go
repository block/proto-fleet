package models

import "time"

// CohortTelemetryComparisonWindow is one supported overview comparison preset.
type CohortTelemetryComparisonWindow string

const (
	CohortTelemetryComparisonWindowOneHour         CohortTelemetryComparisonWindow = "one_hour"
	CohortTelemetryComparisonWindowSixHours        CohortTelemetryComparisonWindow = "six_hours"
	CohortTelemetryComparisonWindowTwentyFourHours CohortTelemetryComparisonWindow = "twenty_four_hours"
)

// CohortTelemetryComparisonMetric identifies one operating outcome.
type CohortTelemetryComparisonMetric string

const (
	CohortTelemetryComparisonMetricHashrate   CohortTelemetryComparisonMetric = "hashrate"
	CohortTelemetryComparisonMetricEfficiency CohortTelemetryComparisonMetric = "efficiency"
	CohortTelemetryComparisonMetricPower      CohortTelemetryComparisonMetric = "power"
)

// CohortTelemetryComparisonParams selects active cohorts and a comparison window.
type CohortTelemetryComparisonParams struct {
	OrgID     int64
	CohortIDs []int64
	Window    CohortTelemetryComparisonWindow
}

// CohortTelemetryComparisonMembership contains the effective current members
// for one active cohort. Default cohort membership is implicit.
type CohortTelemetryComparisonMembership struct {
	CohortID          int64
	Label             string
	IsDefault         bool
	DeviceIdentifiers []string
}

// CohortTelemetryComparisonDistribution summarizes each device's percentage
// change from its own baseline. Absolute medians use the same paired devices.
type CohortTelemetryComparisonDistribution struct {
	Metric                       CohortTelemetryComparisonMetric
	BaselineMedian               *float64
	ComparisonMedian             *float64
	MedianPercentageChange       *float64
	P25PercentageChange          *float64
	P75PercentageChange          *float64
	EligibleDeviceCount          int32
	BaselineReportingDeviceCount int32
	CurrentReportingDeviceCount  int32
	ZeroBaselineDeviceCount      int32
}

// CohortTelemetryComparisonSeries is one selected cohort's paired outcome summary.
type CohortTelemetryComparisonSeries struct {
	CohortID                            int64
	Label                               string
	IsDefault                           bool
	MemberCount                         int64
	Distributions                       []CohortTelemetryComparisonDistribution
	CurrentNonHashingDeviceCount        int32
	BaselineAggregateEfficiency         *float64
	ComparisonAggregateEfficiency       *float64
	AggregateEfficiencyPercentageChange *float64
	AggregateEfficiencyDeviceCount      int32
}

// CohortTelemetryComparison is the complete multi-cohort outcome response.
type CohortTelemetryComparison struct {
	BaselineStart   time.Time
	BaselineEnd     time.Time
	ComparisonStart time.Time
	ComparisonEnd   time.Time
	Window          CohortTelemetryComparisonWindow
	Series          []CohortTelemetryComparisonSeries
}
