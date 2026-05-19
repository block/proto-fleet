package promqlshim

import "time"

type Rule struct {
	ID          string
	Description string
	Severity    string
	For         time.Duration
}

// BuiltinRules is the entire rule set the shim recognises.
func BuiltinRules() []Rule {
	return []Rule{
		{
			ID:          "device-offline-default",
			Description: "Per-device offline detector. Fires when the latest sample is online=false or older than 10 minutes.",
			Severity:    "warning",
			For:         5 * time.Minute,
		},
		{
			ID:          "device-temperature-default",
			Description: "Per-device hot detector. Fires when the latest max sensor temperature is above 90 °C.",
			Severity:    "warning",
			For:         10 * time.Minute,
		},
		{
			ID:          "telemetry-poll-failure-default",
			Description: "Per-org poll failure rate detector. Fires when failures exceed successes over the last 10 minutes.",
			Severity:    "warning",
			For:         10 * time.Minute,
		},
	}
}
