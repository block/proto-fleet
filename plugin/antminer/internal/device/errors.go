package device

// This file implements error detection for Antminer devices.
// Unlike Proto firmware which reports explicit error codes, Antminer devices
// use CGMiner RPC which provides metrics. Errors are inferred heuristically
// from abnormal metric values (high temps, hardware errors, etc.).

import (
	"fmt"
	"strconv"
	"time"

	"github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	sdkerrors "github.com/btc-mining/proto-fleet/server/sdk/v1/errors"
)

// Temperature thresholds (Celsius)
const (
	tempOverheatCriticalCelsius = 95.0
	tempOverheatMajorCelsius    = 85.0
	tempUnderheatMinorCelsius   = 0.0
)

// Hardware error thresholds
const (
	hwErrorPercentMajor       = 5.0
	hwErrorPercentMinor       = 1.0
	hwErrorCountMinor   int64 = 1000
)

// Share rejection thresholds
const (
	rejectedPercentMajor       = 10.0
	rejectedPercentMinor       = 5.0
	staleSharesThreshold int64 = 100
)

// Pool connectivity thresholds
const (
	poolFailuresThreshold int64 = 10
)

// Hashboard status constants
const (
	hashboardStatusAlive = "Alive"
	hashboardEnabledYes  = "Y"
	poolStatusAlive      = "Alive"
)

// Impact and action message constants
const (
	impactReducedHashrate    = "Reduced mining hashrate and revenue"
	impactMiningMayStop      = "Mining may stop to prevent hardware damage"
	impactHashboardOffline   = "Hashboard offline, reduced mining capacity"
	impactPoolConnectivity   = "Unable to submit shares to pool"
	actionCheckCooling       = "Check cooling system, fans, and airflow"
	actionCheckHashboard     = "Check hashboard connections and power"
	actionCheckPoolConfig    = "Verify pool configuration and network connectivity"
	actionMonitorPerformance = "Monitor device performance"
)

// detectErrors aggregates all detected errors from RPC responses.
func detectErrors(summary *rpc.SummaryResponse, devs *rpc.DevsResponse, pools *rpc.PoolsResponse, deviceID string) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError
	now := time.Now()

	// Track if per-board HW errors were found to avoid duplicate summary-level errors
	var perBoardHWErrors []sdkerrors.DeviceError

	// Detect errors from device/hashboard data
	if devs != nil && len(devs.Devs) > 0 {
		errors = append(errors, detectTemperatureErrors(devs.Devs, deviceID, now)...)
		errors = append(errors, detectHashboardStatusErrors(devs.Devs, deviceID, now)...)
		perBoardHWErrors = detectPerBoardHardwareErrors(devs.Devs, deviceID, now)
		errors = append(errors, perBoardHWErrors...)
	}

	// Detect errors from summary data
	if summary != nil && len(summary.Summary) > 0 {
		summaryInfo := &summary.Summary[0]
		// Only add summary-level HW errors if no per-board HW errors exist
		// to avoid duplicate reporting of the same underlying issue
		if len(perBoardHWErrors) == 0 {
			errors = append(errors, detectSummaryHardwareErrors(summaryInfo, deviceID, now)...)
		}
		errors = append(errors, detectShareRejectionErrors(summaryInfo, deviceID, now)...)
	}

	// Detect errors from pool data
	if pools != nil && len(pools.Pools) > 0 {
		errors = append(errors, detectPoolErrors(pools.Pools, deviceID, now)...)
	}

	return errors
}

// detectTemperatureErrors checks boards for temperature issues.
func detectTemperatureErrors(devs []rpc.DevInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, dev := range devs {
		temp := dev.GetTemperature()
		if temp == 0 {
			continue
		}

		ascID := strconv.Itoa(dev.ASC)

		if temp >= tempOverheatCriticalCelsius {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardOverTemperature,
				Severity:          sdkerrors.SeverityCritical,
				Summary:           fmt.Sprintf("Hashboard %d temperature %.1f°C exceeds critical threshold (%.0f°C)", dev.ASC, temp, tempOverheatCriticalCelsius),
				CauseSummary:      "Hashboard critically overheating",
				RecommendedAction: actionCheckCooling,
				Impact:            impactMiningMayStop,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"temperature_celsius": fmt.Sprintf("%.1f", temp),
					"threshold_celsius":   fmt.Sprintf("%.0f", tempOverheatCriticalCelsius),
					"asc_index":           ascID,
				},
			})
		} else if temp >= tempOverheatMajorCelsius {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardOverTemperature,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Hashboard %d temperature %.1f°C exceeds warning threshold (%.0f°C)", dev.ASC, temp, tempOverheatMajorCelsius),
				CauseSummary:      "Hashboard running hot",
				RecommendedAction: actionCheckCooling,
				Impact:            impactReducedHashrate,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"temperature_celsius": fmt.Sprintf("%.1f", temp),
					"threshold_celsius":   fmt.Sprintf("%.0f", tempOverheatMajorCelsius),
					"asc_index":           ascID,
				},
			})
		} else if temp < tempUnderheatMinorCelsius {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardASICUnderTemperature,
				Severity:          sdkerrors.SeverityMinor,
				Summary:           fmt.Sprintf("Hashboard %d temperature %.1f°C below minimum threshold (%.0f°C)", dev.ASC, temp, tempUnderheatMinorCelsius),
				CauseSummary:      "Hashboard temperature too low",
				RecommendedAction: "Check ambient temperature conditions",
				Impact:            impactReducedHashrate,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"temperature_celsius": fmt.Sprintf("%.1f", temp),
					"threshold_celsius":   fmt.Sprintf("%.0f", tempUnderheatMinorCelsius),
					"asc_index":           ascID,
				},
			})
		}
	}

	return errors
}

// detectHashboardStatusErrors checks board status and communication.
func detectHashboardStatusErrors(devs []rpc.DevInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, dev := range devs {
		ascID := strconv.Itoa(dev.ASC)

		// Check if board is not alive (communication lost)
		if dev.Status != hashboardStatusAlive {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.ASICChainCommunicationLost,
				Severity:          sdkerrors.SeverityCritical,
				Summary:           fmt.Sprintf("Hashboard %d status is '%s' (expected '%s')", dev.ASC, dev.Status, hashboardStatusAlive),
				CauseSummary:      "Hashboard communication lost",
				RecommendedAction: actionCheckHashboard,
				Impact:            impactHashboardOffline,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"status":    dev.Status,
					"enabled":   dev.Enabled,
					"asc_index": ascID,
				},
			})
			continue
		}

		// Check if board is disabled
		if dev.Enabled != hashboardEnabledYes {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardNotPresent,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Hashboard %d is disabled", dev.ASC),
				CauseSummary:      "Hashboard disabled",
				RecommendedAction: actionCheckHashboard,
				Impact:            impactHashboardOffline,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"status":    dev.Status,
					"enabled":   dev.Enabled,
					"asc_index": ascID,
				},
			})
			continue
		}

		// Check if board is alive but not hashing
		if dev.MHSAv == 0 {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashrateBelowTarget,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Hashboard %d is alive but not producing hashrate", dev.ASC),
				CauseSummary:      "Hashboard not hashing",
				RecommendedAction: actionCheckHashboard,
				Impact:            impactHashboardOffline,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"status":    dev.Status,
					"enabled":   dev.Enabled,
					"mhs_av":    fmt.Sprintf("%.2f", dev.MHSAv),
					"asc_index": ascID,
				},
			})
		}
	}

	return errors
}

// detectPerBoardHardwareErrors checks per-board hardware error rates.
func detectPerBoardHardwareErrors(devs []rpc.DevInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, dev := range devs {
		ascID := strconv.Itoa(dev.ASC)

		// Check hardware error percentage
		if dev.DeviceHardwarePercent >= hwErrorPercentMajor {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardWarnCRCHigh,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Hashboard %d has %.2f%% hardware error rate (threshold %.1f%%)", dev.ASC, dev.DeviceHardwarePercent, hwErrorPercentMajor),
				CauseSummary:      "High hardware error rate on hashboard",
				RecommendedAction: actionCheckHashboard,
				Impact:            impactReducedHashrate,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"hw_error_percent":  fmt.Sprintf("%.2f", dev.DeviceHardwarePercent),
					"hw_error_count":    strconv.FormatInt(dev.HardwareErrors, 10),
					"threshold_percent": fmt.Sprintf("%.1f", hwErrorPercentMajor),
					"asc_index":         ascID,
				},
			})
		} else if dev.DeviceHardwarePercent >= hwErrorPercentMinor {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardWarnCRCHigh,
				Severity:          sdkerrors.SeverityMinor,
				Summary:           fmt.Sprintf("Hashboard %d has %.2f%% hardware error rate (threshold %.1f%%)", dev.ASC, dev.DeviceHardwarePercent, hwErrorPercentMinor),
				CauseSummary:      "Elevated hardware error rate on hashboard",
				RecommendedAction: actionMonitorPerformance,
				Impact:            impactReducedHashrate,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"hw_error_percent":  fmt.Sprintf("%.2f", dev.DeviceHardwarePercent),
					"hw_error_count":    strconv.FormatInt(dev.HardwareErrors, 10),
					"threshold_percent": fmt.Sprintf("%.1f", hwErrorPercentMinor),
					"asc_index":         ascID,
				},
			})
		} else if dev.HardwareErrors >= hwErrorCountMinor {
			// Also check absolute count for boards with low total work
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.HashboardWarnCRCHigh,
				Severity:          sdkerrors.SeverityMinor,
				Summary:           fmt.Sprintf("Hashboard %d has %d hardware errors (threshold %d)", dev.ASC, dev.HardwareErrors, hwErrorCountMinor),
				CauseSummary:      "Elevated hardware error count on hashboard",
				RecommendedAction: actionMonitorPerformance,
				Impact:            impactReducedHashrate,
				ComponentType:     sdkerrors.ComponentTypeHashBoard,
				ComponentID:       &ascID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"hw_error_percent": fmt.Sprintf("%.2f", dev.DeviceHardwarePercent),
					"hw_error_count":   strconv.FormatInt(dev.HardwareErrors, 10),
					"threshold_count":  strconv.FormatInt(hwErrorCountMinor, 10),
					"asc_index":        ascID,
				},
			})
		}
	}

	return errors
}

// detectSummaryHardwareErrors checks device-level hardware error rates from summary.
func detectSummaryHardwareErrors(summary *rpc.SummaryInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	// Check device-wide hardware error percentage
	if summary.DeviceHardwarePercent >= hwErrorPercentMajor {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashboardWarnCRCHigh,
			Severity:          sdkerrors.SeverityMajor,
			Summary:           fmt.Sprintf("Device has %.2f%% overall hardware error rate (threshold %.1f%%)", summary.DeviceHardwarePercent, hwErrorPercentMajor),
			CauseSummary:      "High device-wide hardware error rate",
			RecommendedAction: actionCheckHashboard,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				"hw_error_percent":  fmt.Sprintf("%.2f", summary.DeviceHardwarePercent),
				"hw_error_count":    strconv.FormatInt(summary.HardwareErrors, 10),
				"threshold_percent": fmt.Sprintf("%.1f", hwErrorPercentMajor),
			},
		})
	} else if summary.DeviceHardwarePercent >= hwErrorPercentMinor {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashboardWarnCRCHigh,
			Severity:          sdkerrors.SeverityMinor,
			Summary:           fmt.Sprintf("Device has %.2f%% overall hardware error rate (threshold %.1f%%)", summary.DeviceHardwarePercent, hwErrorPercentMinor),
			CauseSummary:      "Elevated device-wide hardware error rate",
			RecommendedAction: actionMonitorPerformance,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				"hw_error_percent":  fmt.Sprintf("%.2f", summary.DeviceHardwarePercent),
				"hw_error_count":    strconv.FormatInt(summary.HardwareErrors, 10),
				"threshold_percent": fmt.Sprintf("%.1f", hwErrorPercentMinor),
			},
		})
	}

	return errors
}

// detectShareRejectionErrors checks share rejection and stale rates.
func detectShareRejectionErrors(summary *rpc.SummaryInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	// Check rejection percentage
	if summary.DeviceRejectedPercent >= rejectedPercentMajor {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashrateBelowTarget,
			Severity:          sdkerrors.SeverityMajor,
			Summary:           fmt.Sprintf("Device has %.2f%% share rejection rate (threshold %.1f%%)", summary.DeviceRejectedPercent, rejectedPercentMajor),
			CauseSummary:      "High share rejection rate",
			RecommendedAction: actionCheckPoolConfig,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				"rejected_percent":  fmt.Sprintf("%.2f", summary.DeviceRejectedPercent),
				"rejected_count":    strconv.FormatInt(summary.Rejected, 10),
				"accepted_count":    strconv.FormatInt(summary.Accepted, 10),
				"threshold_percent": fmt.Sprintf("%.1f", rejectedPercentMajor),
			},
		})
	} else if summary.DeviceRejectedPercent >= rejectedPercentMinor {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashrateBelowTarget,
			Severity:          sdkerrors.SeverityMinor,
			Summary:           fmt.Sprintf("Device has %.2f%% share rejection rate (threshold %.1f%%)", summary.DeviceRejectedPercent, rejectedPercentMinor),
			CauseSummary:      "Elevated share rejection rate",
			RecommendedAction: actionCheckPoolConfig,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				"rejected_percent":  fmt.Sprintf("%.2f", summary.DeviceRejectedPercent),
				"rejected_count":    strconv.FormatInt(summary.Rejected, 10),
				"accepted_count":    strconv.FormatInt(summary.Accepted, 10),
				"threshold_percent": fmt.Sprintf("%.1f", rejectedPercentMinor),
			},
		})
	}

	// Check stale shares
	if summary.Stale >= staleSharesThreshold {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashrateBelowTarget,
			Severity:          sdkerrors.SeverityMinor,
			Summary:           fmt.Sprintf("Device has %d stale shares (threshold %d)", summary.Stale, staleSharesThreshold),
			CauseSummary:      "High stale share count",
			RecommendedAction: actionCheckPoolConfig,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				"stale_count":        strconv.FormatInt(summary.Stale, 10),
				"threshold_count":    strconv.FormatInt(staleSharesThreshold, 10),
				"pool_stale_percent": fmt.Sprintf("%.2f", summary.PoolStalePercent),
			},
		})
	}

	return errors
}

// detectPoolErrors checks pool connectivity issues.
func detectPoolErrors(pools []rpc.PoolInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, pool := range pools {
		poolID := strconv.Itoa(pool.Pool)

		// Check pool status
		if pool.Status != poolStatusAlive {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.DeviceCommunicationLost,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Pool %d (%s) status is '%s' (expected '%s')", pool.Pool, pool.URL, pool.Status, poolStatusAlive),
				CauseSummary:      "Pool connection not alive",
				RecommendedAction: actionCheckPoolConfig,
				Impact:            impactPoolConnectivity,
				ComponentType:     sdkerrors.ComponentTypeControlBoard,
				ComponentID:       &poolID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"pool_url":    pool.URL,
					"pool_status": pool.Status,
					"pool_index":  poolID,
				},
			})
			continue
		}

		// Check get failures
		if pool.GetFailures >= poolFailuresThreshold {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.DeviceCommunicationLost,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Pool %d (%s) has %d get failures (threshold %d)", pool.Pool, pool.URL, pool.GetFailures, poolFailuresThreshold),
				CauseSummary:      "High pool get failure count",
				RecommendedAction: actionCheckPoolConfig,
				Impact:            impactPoolConnectivity,
				ComponentType:     sdkerrors.ComponentTypeControlBoard,
				ComponentID:       &poolID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"pool_url":        pool.URL,
					"get_failures":    strconv.FormatInt(pool.GetFailures, 10),
					"remote_failures": strconv.FormatInt(pool.RemoteFailures, 10),
					"threshold":       strconv.FormatInt(poolFailuresThreshold, 10),
					"pool_index":      poolID,
				},
			})
		}

		// Check remote failures
		if pool.RemoteFailures >= poolFailuresThreshold {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.DeviceCommunicationLost,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Pool %d (%s) has %d remote failures (threshold %d)", pool.Pool, pool.URL, pool.RemoteFailures, poolFailuresThreshold),
				CauseSummary:      "High pool remote failure count",
				RecommendedAction: actionCheckPoolConfig,
				Impact:            impactPoolConnectivity,
				ComponentType:     sdkerrors.ComponentTypeControlBoard,
				ComponentID:       &poolID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					"pool_url":        pool.URL,
					"get_failures":    strconv.FormatInt(pool.GetFailures, 10),
					"remote_failures": strconv.FormatInt(pool.RemoteFailures, 10),
					"threshold":       strconv.FormatInt(poolFailuresThreshold, 10),
					"pool_index":      poolID,
				},
			})
		}
	}

	return errors
}
