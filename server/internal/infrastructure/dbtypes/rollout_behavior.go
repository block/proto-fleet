// Package dbtypes contains typed values used by generated database queries.
package dbtypes

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

// RolloutBehaviorSnapshot is the immutable update configuration captured when
// a rollout starts. MaxConcurrentOffline records the initial budget for
// historical views; dispatch always reads the current channel budget.
//
// Validation here describes the stored shape and bounds. The rollout domain
// owns method-specific defaults, accepted combinations, and execution policy.
//
//nolint:recvcheck // sql.Scanner must mutate the receiver; driver.Valuer must also work on sqlc's value parameters.
type RolloutBehaviorSnapshot struct {
	Method                       string   `json:"method"`
	OrderBy                      string   `json:"order_by"`
	BatchSize                    int32    `json:"batch_size"`
	PilotSize                    int32    `json:"pilot_size"`
	WaitBetweenBatchesSeconds    int32    `json:"wait_between_batches_seconds"`
	ReviewAfterEachBatch         bool     `json:"review_after_each_batch"`
	AutoContinue                 bool     `json:"auto_continue"`
	StabilizationSeconds         int32    `json:"stabilization_seconds"`
	MaxHashrateDropPercent       *float64 `json:"max_hashrate_drop_percent,omitempty"`
	MaxEfficiencyIncreasePercent *float64 `json:"max_efficiency_increase_percent,omitempty"`
	MaxTempIncreaseC             *float64 `json:"max_temp_increase_c,omitempty"`
	MaxNewErrors                 *int32   `json:"max_new_errors,omitempty"`
	MinSampleCoveragePercent     *float64 `json:"min_sample_coverage_percent,omitempty"`
	MaxConcurrentOffline         int32    `json:"max_concurrent_offline"`
	ControllerTimeoutSeconds     int32    `json:"controller_timeout_seconds"`
}

// Scan rejects malformed snapshots instead of allowing an incomplete decode
// to silently change how a persisted rollout proceeds.
func (s *RolloutBehaviorSnapshot) Scan(src any) error {
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("rollout behavior snapshot: expected JSON object, got %T", src)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("rollout behavior snapshot: expected JSON object")
	}
	for key, value := range fields {
		// encoding/json otherwise accepts case-insensitive tag matches. Keep
		// the stored keys exact so aliases cannot bypass JSONB constraints.
		nullable := false
		switch key {
		case "method", "order_by", "batch_size", "pilot_size", "wait_between_batches_seconds", "review_after_each_batch", "auto_continue", "stabilization_seconds", "max_concurrent_offline", "controller_timeout_seconds":
		case "max_hashrate_drop_percent", "max_efficiency_increase_percent", "max_temp_increase_c", "max_new_errors", "min_sample_coverage_percent":
			nullable = true
		default:
			return fmt.Errorf("rollout behavior snapshot: unknown field %q", key)
		}
		if !nullable && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("rollout behavior snapshot: %s must not be null", key)
		}
	}
	var next RolloutBehaviorSnapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&next); err != nil {
		return fmt.Errorf("rollout behavior snapshot: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("rollout behavior snapshot: unexpected trailing JSON")
	}
	if err := next.validate(); err != nil {
		return err
	}
	*s = next
	return nil
}

func (s RolloutBehaviorSnapshot) Value() (driver.Value, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("rollout behavior snapshot: %w", err)
	}
	return string(data), nil
}

func (s RolloutBehaviorSnapshot) validate() error {
	switch s.Method {
	case "all_at_once", "batched", "pilot_then_continue", "delegated":
	default:
		return fmt.Errorf("rollout behavior snapshot: unknown method %q", s.Method)
	}
	switch s.OrderBy {
	case "least_efficient_first", "random":
	default:
		return fmt.Errorf("rollout behavior snapshot: unknown order %q", s.OrderBy)
	}
	for name, value := range map[string]int32{
		"batch_size": s.BatchSize, "pilot_size": s.PilotSize,
		"wait_between_batches_seconds": s.WaitBetweenBatchesSeconds,
		"stabilization_seconds":        s.StabilizationSeconds,
		"max_concurrent_offline":       s.MaxConcurrentOffline,
		"controller_timeout_seconds":   s.ControllerTimeoutSeconds,
	} {
		if value < 0 {
			return fmt.Errorf("rollout behavior snapshot: %s must not be negative", name)
		}
	}
	for name, value := range map[string]*float64{
		"max_hashrate_drop_percent":       s.MaxHashrateDropPercent,
		"max_efficiency_increase_percent": s.MaxEfficiencyIncreasePercent,
		"max_temp_increase_c":             s.MaxTempIncreaseC,
		"min_sample_coverage_percent":     s.MinSampleCoveragePercent,
	} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("rollout behavior snapshot: %s must be finite and nonnegative", name)
		}
	}
	if s.MaxHashrateDropPercent != nil && *s.MaxHashrateDropPercent > 100 {
		return fmt.Errorf("rollout behavior snapshot: max_hashrate_drop_percent must not exceed 100")
	}
	if s.MinSampleCoveragePercent != nil && (*s.MinSampleCoveragePercent <= 0 || *s.MinSampleCoveragePercent > 100) {
		return fmt.Errorf("rollout behavior snapshot: min_sample_coverage_percent must be above 0 and at most 100")
	}
	if s.MaxNewErrors != nil && *s.MaxNewErrors < 0 {
		return fmt.Errorf("rollout behavior snapshot: max_new_errors must not be negative")
	}
	return nil
}
