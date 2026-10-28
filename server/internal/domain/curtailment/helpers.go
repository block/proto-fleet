package curtailment

import "math"

// ToStringSet builds a membership set, collapsing duplicate values.
func ToStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// IsFinite reports whether a telemetry or control value is neither NaN nor Inf.
func IsFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
