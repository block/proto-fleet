package models

import "time"

type Telemetry struct {
	Measurement string            `json:"measurement"`
	Fields      map[string]any    `json:"fields"`
	Tags        map[string]string `json:"tags"`
	Timestamp   time.Time         `json:"timestamp"` // Unix timestamp in milliseconds
}
