// Package models provides v2 telemetry data models for Bitcoin mining devices.
//
// Design principles:
// - All metrics are optional (*MetricValue) to handle partial data from diverse hardware
// - Component-level detail supports heterogeneous mining hardware configurations
// - Metadata supports both point-in-time and aggregated values
// - Health status is separate from operational metrics for clearer alerting
// - Enums serialize as strings for API stability and readability
package models

import "time"

// MetricValue represents a single telemetry measurement with optional statistical metadata.
// The Value field is always present, while MetaData is optional and provides additional
// context for aggregated or bounded values.
type MetricValue struct {
	Value float64 `json:"value"`

	Kind     MetricKind           `json:"kind,omitempty"` // Defaults to gauge if not set
	MetaData *MetricValueMetaData `json:"metadata,omitempty"`
}

// MetricValueMetaData provides statistical context for a metric value.
// It supports aggregated values (Min/Max/Avg/StdDev over a time window) and
// metric classification (Gauge/Rate/Counter) for proper interpretation.
type MetricValueMetaData struct {
	Window *time.Duration `json:"window,omitempty"`  // Time window for aggregated values
	Min    *float64       `json:"min,omitempty"`     // Minimum value in the window
	Max    *float64       `json:"max,omitempty"`     // Maximum value in the window
	Avg    *float64       `json:"avg,omitempty"`     // Average value over the window
	StdDev *float64       `json:"std_dev,omitempty"` // Standard deviation

	Timestamp *time.Time `json:"timestamp,omitempty"` // Optional timestamp, only use if it differs from parent metric timestamp
}
