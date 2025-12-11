// Package testdata provides test utilities for the error query service.
// This package is intended for TESTING AND DEVELOPMENT ONLY.
// It provides deterministic error scenarios for UI development, integration testing, and demos.
// In production, errors are sourced from actual device telemetry.
package testdata

import (
	"fmt"
	"os"
	"strconv"
	"time"

	errorsv1 "github.com/btc-mining/proto-fleet/server/generated/grpc/errors/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/errorquery"
	"gopkg.in/yaml.v3"
)

// SeedFile represents the structure of the YAML seed file.
// FOR TESTING/DEVELOPMENT ONLY.
type SeedFile struct {
	Devices []DeviceSeed `yaml:"devices"`
}

// DeviceSeed represents seed data for a single device in YAML.
// FOR TESTING/DEVELOPMENT ONLY.
type DeviceSeed struct {
	DeviceID   int64       `yaml:"device_id"`
	DeviceType string      `yaml:"device_type"`
	Errors     []ErrorSeed `yaml:"errors"`
}

// ErrorSeed represents a single error in YAML format.
// FOR TESTING/DEVELOPMENT ONLY.
type ErrorSeed struct {
	// CanonicalError can be specified as string name or integer code.
	CanonicalError string `yaml:"canonical_error"`

	// Severity can be specified as string name or integer.
	Severity string `yaml:"severity"`

	Summary           string            `yaml:"summary,omitempty"`
	CauseSummary      string            `yaml:"cause_summary,omitempty"`
	RecommendedAction string            `yaml:"recommended_action,omitempty"`
	Impact            string            `yaml:"impact,omitempty"`
	ComponentID       string            `yaml:"component_id,omitempty"`
	VendorAttributes  map[string]string `yaml:"vendor_attributes,omitempty"`

	// Optional time fields - if not specified, defaults are used.
	FirstSeenAgo string `yaml:"first_seen_ago,omitempty"` // e.g., "2h", "7d", "30m"
	LastSeenAgo  string `yaml:"last_seen_ago,omitempty"`  // e.g., "5m", "1h"
	Closed       bool   `yaml:"closed,omitempty"`         // If true, error is marked as closed
}

// LoadSeedFile loads seed data from a YAML file.
// FOR TESTING/DEVELOPMENT ONLY.
func LoadSeedFile(path string) ([]errorquery.SeedData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read seed file: %w", err)
	}

	var seedFile SeedFile
	if err := yaml.Unmarshal(data, &seedFile); err != nil {
		return nil, fmt.Errorf("failed to parse seed file: %w", err)
	}

	return convertSeedFile(seedFile)
}

// convertSeedFile converts YAML seed data to internal SeedData format.
func convertSeedFile(seedFile SeedFile) ([]errorquery.SeedData, error) {
	var result []errorquery.SeedData

	metadata := errorquery.BuildMinerErrorMetadata()

	for _, device := range seedFile.Devices {
		var errors []errorquery.ErrorRecord

		for _, errSeed := range device.Errors {
			record, err := convertErrorSeed(errSeed, device.DeviceID, metadata)
			if err != nil {
				return nil, fmt.Errorf("device %d: %w", device.DeviceID, err)
			}
			errors = append(errors, record)
		}

		result = append(result, errorquery.SeedData{
			DeviceID:   strconv.FormatInt(device.DeviceID, 10),
			DeviceType: device.DeviceType,
			Errors:     errors,
		})
	}

	return result, nil
}

// convertErrorSeed converts a single YAML error to ErrorRecord.
func convertErrorSeed(seed ErrorSeed, deviceID int64, metadata map[errorsv1.MinerError]*errorquery.MinerErrorMetadata) (errorquery.ErrorRecord, error) {
	// Parse miner error.
	minerError, err := parseMinerError(seed.CanonicalError)
	if err != nil {
		return errorquery.ErrorRecord{}, fmt.Errorf("invalid canonical_error %q: %w", seed.CanonicalError, err)
	}

	// Parse severity.
	severity, err := parseSeverity(seed.Severity)
	if err != nil {
		return errorquery.ErrorRecord{}, fmt.Errorf("invalid severity %q: %w", seed.Severity, err)
	}

	// Get defaults from metadata if not specified.
	meta := metadata[minerError]
	summary := seed.Summary
	causeSummary := seed.CauseSummary
	recommendedAction := seed.RecommendedAction
	impact := seed.Impact

	if meta != nil {
		if summary == "" {
			summary = meta.DefaultSummary
		}
		if causeSummary == "" {
			causeSummary = meta.DefaultSummary
		}
		if recommendedAction == "" {
			recommendedAction = meta.DefaultAction
		}
		if impact == "" {
			impact = meta.DefaultImpact
		}
		if severity == errorsv1.Severity_SEVERITY_UNSPECIFIED {
			severity = meta.DefaultSeverity
		}
	}

	// Parse timestamps.
	now := time.Now()
	firstSeenAt := now.Add(-2 * time.Hour)  // Default: 2 hours ago
	lastSeenAt := now.Add(-5 * time.Minute) // Default: 5 minutes ago

	if seed.FirstSeenAgo != "" {
		duration, err := parseDuration(seed.FirstSeenAgo)
		if err != nil {
			return errorquery.ErrorRecord{}, fmt.Errorf("invalid first_seen_ago %q: %w", seed.FirstSeenAgo, err)
		}
		firstSeenAt = now.Add(-duration)
	}

	if seed.LastSeenAgo != "" {
		duration, err := parseDuration(seed.LastSeenAgo)
		if err != nil {
			return errorquery.ErrorRecord{}, fmt.Errorf("invalid last_seen_ago %q: %w", seed.LastSeenAgo, err)
		}
		lastSeenAt = now.Add(-duration)
	}

	// Ensure lastSeenAt is after firstSeenAt.
	if lastSeenAt.Before(firstSeenAt) {
		lastSeenAt = firstSeenAt.Add(time.Minute)
	}

	var closedAt *time.Time
	if seed.Closed {
		closed := lastSeenAt.Add(time.Minute)
		closedAt = &closed
	}

	return errorquery.ErrorRecord{
		// ErrorID will be generated by FakeErrorManager.Seed()
		MinerError:        minerError,
		Severity:          severity,
		Summary:           summary,
		CauseSummary:      causeSummary,
		RecommendedAction: recommendedAction,
		Impact:            impact,
		FirstSeenAt:       firstSeenAt,
		LastSeenAt:        lastSeenAt,
		ClosedAt:          closedAt,
		VendorAttributes:  seed.VendorAttributes,
		DeviceID:          strconv.FormatInt(deviceID, 10),
		ComponentID:       seed.ComponentID,
	}, nil
}

// parseMinerError parses a miner error from string.
func parseMinerError(s string) (errorsv1.MinerError, error) {
	if s == "" {
		return errorsv1.MinerError_MINER_ERROR_UNSPECIFIED, fmt.Errorf("canonical_error is required")
	}

	// Try parsing as enum name (with or without prefix).
	if val, ok := errorsv1.MinerError_value[s]; ok {
		return errorsv1.MinerError(val), nil
	}

	// Try with MINER_ERROR_ prefix.
	prefixed := "MINER_ERROR_" + s
	if val, ok := errorsv1.MinerError_value[prefixed]; ok {
		return errorsv1.MinerError(val), nil
	}

	return errorsv1.MinerError_MINER_ERROR_UNSPECIFIED, fmt.Errorf("unknown miner error: %s", s)
}

// parseSeverity parses a severity from string.
func parseSeverity(s string) (errorsv1.Severity, error) {
	if s == "" {
		return errorsv1.Severity_SEVERITY_UNSPECIFIED, nil // Will use default from metadata.
	}

	// Try parsing as enum name (with or without prefix).
	if val, ok := errorsv1.Severity_value[s]; ok {
		return errorsv1.Severity(val), nil
	}

	// Try with SEVERITY_ prefix.
	prefixed := "SEVERITY_" + s
	if val, ok := errorsv1.Severity_value[prefixed]; ok {
		return errorsv1.Severity(val), nil
	}

	// Try common short names.
	switch s {
	case "critical", "CRITICAL":
		return errorsv1.Severity_SEVERITY_CRITICAL, nil
	case "major", "MAJOR":
		return errorsv1.Severity_SEVERITY_MAJOR, nil
	case "minor", "MINOR":
		return errorsv1.Severity_SEVERITY_MINOR, nil
	case "info", "INFO":
		return errorsv1.Severity_SEVERITY_INFO, nil
	}

	return errorsv1.Severity_SEVERITY_UNSPECIFIED, fmt.Errorf("unknown severity: %s", s)
}

// parseDuration parses a duration string with support for days.
func parseDuration(s string) (time.Duration, error) {
	// Check for day suffix (not supported by time.ParseDuration).
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %w", err)
	}
	return d, nil
}
