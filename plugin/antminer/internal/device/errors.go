package device

// This file implements error detection for Antminer devices.
// Unlike Proto firmware which reports explicit error codes, Antminer devices
// use CGMiner RPC which provides metrics. Errors are inferred heuristically
// from abnormal metric values (high temps, hardware errors, etc.).

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdkerrors "github.com/block/proto-fleet/server/sdk/v1/errors"
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

// Hashboard error status types (used in helper functions)
const (
	statusNotHashing        = "not_hashing"
	statusDisabled          = "disabled"
	statusCommunicationLost = "communication_lost"
)

// Vendor attribute keys
const (
	attrASCIndex         = "asc_index"
	attrChainIndex       = "chain_index"
	attrSerialNumber     = "serial_number"
	attrStatus           = "status"
	attrEnabled          = "enabled"
	attrMHSAv            = "mhs_av"
	attrTempCelsius      = "temperature_celsius"
	attrThresholdCelsius = "threshold_celsius"
	attrHWErrorPercent   = "hw_error_percent"
	attrHWErrorCount     = "hw_error_count"
	attrThresholdPercent = "threshold_percent"
	attrThresholdCount   = "threshold_count"
	attrRateRealGHS      = "rate_real_ghs"
	attrRateIdealGHS     = "rate_ideal_ghs"
	attrFanIndex         = "fan_index"
	attrFanRPM           = "fan_rpm"
	attrRejectedPercent  = "rejected_percent"
	attrRejectedCount    = "rejected_count"
	attrAcceptedCount    = "accepted_count"
	attrStaleCount       = "stale_count"
	attrPoolStalePercent = "pool_stale_percent"
	attrPoolURL          = "pool_url"
	attrPoolStatus       = "pool_status"
	attrPoolIndex        = "pool_index"
	attrPSUIndex         = "psu_index"
	attrPSUStatus        = "psu_status"
	attrGetFailures      = "get_failures"
	attrRemoteFailures   = "remote_failures"
	attrThreshold        = "threshold"
)

// Cause summary message constants
const (
	causeHighDeviceHWError      = "High device-wide hardware error rate"
	causeElevatedDeviceHWError  = "Elevated device-wide hardware error rate"
	causeHighShareRejection     = "High share rejection rate"
	causeElevatedShareRejection = "Elevated share rejection rate"
	causeHighStaleShares        = "High stale share count"
	causePoolNotAlive           = "Pool connection not alive"
	causeHighPoolGetFailures    = "High pool get failure count"
	causeHighPoolRemoteFailures = "High pool remote failure count"
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

// Helper functions to create error structures with common fields

// createTemperatureError creates a temperature-related error with common fields
func createTemperatureError(severity sdkerrors.Severity, boardIndex int, temp float64, threshold float64, deviceID string, componentID string, now time.Time, extraAttrs map[string]string) sdkerrors.DeviceError {
	var errorType sdkerrors.MinerError
	var causeSummary, action, impact string

	if temp < tempUnderheatMinorCelsius {
		errorType = sdkerrors.HashboardASICUnderTemperature
		causeSummary = "Hashboard temperature too low"
		action = "Check ambient temperature conditions"
		impact = impactReducedHashrate
	} else {
		errorType = sdkerrors.HashboardOverTemperature
		if severity == sdkerrors.SeverityCritical {
			causeSummary = "Hashboard critically overheating"
			impact = impactMiningMayStop
		} else {
			causeSummary = "Hashboard running hot"
			impact = impactReducedHashrate
		}
		action = actionCheckCooling
	}

	attrs := map[string]string{
		attrTempCelsius:      fmt.Sprintf("%.1f", temp),
		attrThresholdCelsius: fmt.Sprintf("%.0f", threshold),
	}
	for k, v := range extraAttrs {
		attrs[k] = v
	}

	return sdkerrors.DeviceError{
		MinerError:        errorType,
		Severity:          severity,
		Summary:           fmt.Sprintf("Hashboard %d temperature %.1f°C %s threshold (%.0f°C)", boardIndex, temp, getSeverityVerb(severity, temp < tempUnderheatMinorCelsius), threshold),
		CauseSummary:      causeSummary,
		RecommendedAction: action,
		Impact:            impact,
		ComponentType:     sdkerrors.ComponentTypeHashBoard,
		ComponentID:       &componentID,
		FirstSeenAt:       now,
		LastSeenAt:        now,
		DeviceID:          deviceID,
		VendorAttributes:  attrs,
	}
}

// getSeverityVerb returns the appropriate verb for temperature errors
func getSeverityVerb(severity sdkerrors.Severity, isUnder bool) string {
	if isUnder {
		return "below minimum"
	}
	if severity == sdkerrors.SeverityCritical {
		return "exceeds critical"
	}
	return "exceeds warning"
}

// createHashboardStatusError creates a hashboard status error with common fields
func createHashboardStatusError(severity sdkerrors.Severity, boardIndex int, status string, deviceID string, componentID string, now time.Time, extraAttrs map[string]string) sdkerrors.DeviceError {
	var errorType sdkerrors.MinerError
	var summary, causeSummary, impact string

	if status == statusNotHashing {
		errorType = sdkerrors.HashrateBelowTarget
		summary = fmt.Sprintf("Hashboard %d is not producing hashrate", boardIndex)
		causeSummary = "Hashboard not hashing"
	} else if status == statusDisabled {
		errorType = sdkerrors.HashboardNotPresent
		summary = fmt.Sprintf("Hashboard %d is disabled", boardIndex)
		causeSummary = "Hashboard disabled"
	} else {
		// Communication lost - include actual status if available in vendor attrs
		errorType = sdkerrors.ASICChainCommunicationLost
		actualStatus := extraAttrs[attrStatus]
		if actualStatus != "" && actualStatus != hashboardStatusAlive {
			summary = fmt.Sprintf("Hashboard %d status is '%s' (expected '%s')", boardIndex, actualStatus, hashboardStatusAlive)
		} else {
			summary = fmt.Sprintf("Hashboard %d communication lost", boardIndex)
		}
		causeSummary = "Hashboard communication lost"
	}

	impact = impactHashboardOffline

	return sdkerrors.DeviceError{
		MinerError:        errorType,
		Severity:          severity,
		Summary:           summary,
		CauseSummary:      causeSummary,
		RecommendedAction: actionCheckHashboard,
		Impact:            impact,
		ComponentType:     sdkerrors.ComponentTypeHashBoard,
		ComponentID:       &componentID,
		FirstSeenAt:       now,
		LastSeenAt:        now,
		DeviceID:          deviceID,
		VendorAttributes:  extraAttrs,
	}
}

// createHardwareErrorError creates a hardware error with common fields
func createHardwareErrorError(severity sdkerrors.Severity, boardIndex int, hwPercent float64, hwCount int, threshold string, deviceID string, componentID string, now time.Time, extraAttrs map[string]string) sdkerrors.DeviceError {
	var summary string
	if hwPercent >= hwErrorPercentMinor {
		summary = fmt.Sprintf("Hashboard %d has %.2f%% hardware error rate (threshold %s)", boardIndex, hwPercent, threshold)
	} else {
		summary = fmt.Sprintf("Hashboard %d has %d hardware errors (threshold %s)", boardIndex, hwCount, threshold)
	}

	var causeSummary, action string
	if severity == sdkerrors.SeverityMajor {
		causeSummary = "High hardware error rate on hashboard"
		action = actionCheckHashboard
	} else {
		causeSummary = "Elevated hardware error rate on hashboard"
		action = actionMonitorPerformance
	}

	attrs := map[string]string{
		attrHWErrorPercent:                         fmt.Sprintf("%.2f", hwPercent),
		attrHWErrorCount:                           strconv.Itoa(hwCount),
		"threshold_" + getThresholdType(hwPercent): threshold,
	}
	for k, v := range extraAttrs {
		attrs[k] = v
	}

	return sdkerrors.DeviceError{
		MinerError:        sdkerrors.HashboardWarnCRCHigh,
		Severity:          severity,
		Summary:           summary,
		CauseSummary:      causeSummary,
		RecommendedAction: action,
		Impact:            impactReducedHashrate,
		ComponentType:     sdkerrors.ComponentTypeHashBoard,
		ComponentID:       &componentID,
		FirstSeenAt:       now,
		LastSeenAt:        now,
		DeviceID:          deviceID,
		VendorAttributes:  attrs,
	}
}

// getThresholdType returns "percent" or "count" based on error rate
func getThresholdType(hwPercent float64) string {
	if hwPercent >= hwErrorPercentMinor {
		return "percent"
	}
	return "count"
}

// detectErrors aggregates all detected errors from RPC and Web API responses.
// Prioritizes stats.cgi data when available, falling back to RPC devs data.
func detectErrors(summary *rpc.SummaryResponse, devs *rpc.DevsResponse, pools *rpc.PoolsResponse, stats *web.StatsInfo, deviceID string, sleeping bool) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError
	now := time.Now()

	// Track if per-board HW errors were found to avoid duplicate summary-level errors
	var perBoardHWErrors []sdkerrors.DeviceError

	// Detect errors from device/hashboard data
	// Prefer stats API data when available, fall back to devs RPC
	if stats != nil && len(stats.STATS) > 0 && len(stats.STATS[0].Chain) > 0 {
		// Use stats.cgi data for temperature, hashboard status, and per-board errors
		slog.Debug("Using stats.cgi API data for per-chain error detection", "deviceID", deviceID, "chainCount", len(stats.STATS[0].Chain))
		errors = append(errors, detectTemperatureErrorsFromStats(stats.STATS[0].Chain, deviceID, now)...)
		errors = append(errors, detectHashboardStatusErrorsFromStats(stats.STATS[0].Chain, deviceID, now, sleeping)...)
		errors = append(errors, detectFanErrorsFromStats(stats.STATS[0].Fan, stats.STATS[0].FanNum, deviceID, now)...)
		errors = append(errors, detectPSUErrorsFromStats(stats.STATS[0].PSU, deviceID, now)...)
		perBoardHWErrors = detectPerBoardHardwareErrorsFromStats(stats.STATS[0].Chain, deviceID, now)
		errors = append(errors, perBoardHWErrors...)
	} else if devs != nil && len(devs.Devs) > 0 {
		// Fallback to RPC devs data
		slog.Debug("Falling back to RPC devs API for per-board error detection", "deviceID", deviceID, "devCount", len(devs.Devs))
		errors = append(errors, detectTemperatureErrors(devs.Devs, deviceID, now)...)
		errors = append(errors, detectHashboardStatusErrors(devs.Devs, deviceID, now, sleeping)...)
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

func detectFanErrorsFromStats(fanSpeeds []int, activeFanCount int, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	checkedFans := len(fanSpeeds)
	if activeFanCount >= 0 && activeFanCount < checkedFans {
		checkedFans = activeFanCount
	}

	for i := range checkedFans {
		rpm := fanSpeeds[i]
		if rpm > 0 {
			continue
		}

		fanSlot := i + 1
		fanID := strconv.Itoa(fanSlot)
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.FanFailed,
			Severity:          sdkerrors.SeverityCritical,
			Summary:           fmt.Sprintf("Fan %d has stopped working", fanSlot),
			CauseSummary:      "Cooling fan stopped reporting RPM",
			RecommendedAction: "Replace failed fan immediately",
			Impact:            "Miner will thermal throttle or shut down",
			ComponentType:     sdkerrors.ComponentTypeFan,
			ComponentID:       &fanID,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrFanIndex: fanID,
				attrFanRPM:   strconv.Itoa(rpm),
			},
		})
	}

	return errors
}

func detectPSUErrorsFromStats(psu *web.PSUStats, deviceID string, now time.Time) []sdkerrors.DeviceError {
	if psu == nil || strings.EqualFold(psu.Status, "ok") || psu.Status == "" {
		return nil
	}

	psuSlot := psu.Index + 1
	psuID := strconv.Itoa(psuSlot)
	return []sdkerrors.DeviceError{
		{
			MinerError:        sdkerrors.PSUFaultGeneric,
			Severity:          sdkerrors.SeverityMajor,
			Summary:           fmt.Sprintf("PSU %d status is '%s'", psuSlot, psu.Status),
			CauseSummary:      "Power supply reported a fault",
			RecommendedAction: "Inspect PSU for damage or overheating",
			Impact:            "Power delivery may be compromised",
			ComponentType:     sdkerrors.ComponentTypePSU,
			ComponentID:       &psuID,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrPSUIndex:  psuID,
				attrPSUStatus: psu.Status,
			},
		},
	}
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
		vendorAttrs := map[string]string{attrASCIndex: ascID}

		if temp >= tempOverheatCriticalCelsius {
			errors = append(errors, createTemperatureError(
				sdkerrors.SeverityCritical, dev.ASC, temp, tempOverheatCriticalCelsius,
				deviceID, ascID, now, vendorAttrs,
			))
		} else if temp >= tempOverheatMajorCelsius {
			errors = append(errors, createTemperatureError(
				sdkerrors.SeverityMajor, dev.ASC, temp, tempOverheatMajorCelsius,
				deviceID, ascID, now, vendorAttrs,
			))
		} else if temp < tempUnderheatMinorCelsius {
			errors = append(errors, createTemperatureError(
				sdkerrors.SeverityMinor, dev.ASC, temp, tempUnderheatMinorCelsius,
				deviceID, ascID, now, vendorAttrs,
			))
		}
	}

	return errors
}

// detectHashboardStatusErrors checks board status and communication.
func detectHashboardStatusErrors(devs []rpc.DevInfo, deviceID string, now time.Time, sleeping bool) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, dev := range devs {
		ascID := strconv.Itoa(dev.ASC)
		vendorAttrs := map[string]string{
			attrStatus:   dev.Status,
			attrEnabled:  dev.Enabled,
			attrASCIndex: ascID,
		}

		// Check if board is not alive (communication lost)
		if dev.Status != hashboardStatusAlive {
			errors = append(errors, createHashboardStatusError(
				sdkerrors.SeverityCritical, dev.ASC, statusCommunicationLost,
				deviceID, ascID, now, vendorAttrs,
			))
			continue
		}

		// Check if board is disabled
		if dev.Enabled != hashboardEnabledYes {
			errors = append(errors, createHashboardStatusError(
				sdkerrors.SeverityMajor, dev.ASC, statusDisabled,
				deviceID, ascID, now, vendorAttrs,
			))
			continue
		}

		// Check if board is alive but not hashing
		if !sleeping && dev.MHSAv == 0 {
			vendorAttrs[attrMHSAv] = fmt.Sprintf("%.2f", dev.MHSAv)
			errors = append(errors, createHashboardStatusError(
				sdkerrors.SeverityMajor, dev.ASC, statusNotHashing,
				deviceID, ascID, now, vendorAttrs,
			))
		}
	}

	return errors
}

// detectPerBoardHardwareErrors checks per-board hardware error rates.
func detectPerBoardHardwareErrors(devs []rpc.DevInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, dev := range devs {
		ascID := strconv.Itoa(dev.ASC)
		vendorAttrs := map[string]string{attrASCIndex: ascID}

		// Check hardware error percentage
		if dev.DeviceHardwarePercent >= hwErrorPercentMajor {
			errors = append(errors, createHardwareErrorError(
				sdkerrors.SeverityMajor, dev.ASC, dev.DeviceHardwarePercent,
				int(dev.HardwareErrors), fmt.Sprintf("%.1f%%", hwErrorPercentMajor),
				deviceID, ascID, now, vendorAttrs,
			))
		} else if dev.DeviceHardwarePercent >= hwErrorPercentMinor {
			errors = append(errors, createHardwareErrorError(
				sdkerrors.SeverityMinor, dev.ASC, dev.DeviceHardwarePercent,
				int(dev.HardwareErrors), fmt.Sprintf("%.1f%%", hwErrorPercentMinor),
				deviceID, ascID, now, vendorAttrs,
			))
		} else if dev.HardwareErrors >= hwErrorCountMinor {
			// Also check absolute count for boards with low total work
			errors = append(errors, createHardwareErrorError(
				sdkerrors.SeverityMinor, dev.ASC, dev.DeviceHardwarePercent,
				int(dev.HardwareErrors), strconv.FormatInt(hwErrorCountMinor, 10),
				deviceID, ascID, now, vendorAttrs,
			))
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
			CauseSummary:      causeHighDeviceHWError,
			RecommendedAction: actionCheckHashboard,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrHWErrorPercent:   fmt.Sprintf("%.2f", summary.DeviceHardwarePercent),
				attrHWErrorCount:     strconv.FormatInt(summary.HardwareErrors, 10),
				attrThresholdPercent: fmt.Sprintf("%.1f", hwErrorPercentMajor),
			},
		})
	} else if summary.DeviceHardwarePercent >= hwErrorPercentMinor {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashboardWarnCRCHigh,
			Severity:          sdkerrors.SeverityMinor,
			Summary:           fmt.Sprintf("Device has %.2f%% overall hardware error rate (threshold %.1f%%)", summary.DeviceHardwarePercent, hwErrorPercentMinor),
			CauseSummary:      causeElevatedDeviceHWError,
			RecommendedAction: actionMonitorPerformance,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrHWErrorPercent:   fmt.Sprintf("%.2f", summary.DeviceHardwarePercent),
				attrHWErrorCount:     strconv.FormatInt(summary.HardwareErrors, 10),
				attrThresholdPercent: fmt.Sprintf("%.1f", hwErrorPercentMinor),
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
			CauseSummary:      causeHighShareRejection,
			RecommendedAction: actionCheckPoolConfig,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrRejectedPercent:  fmt.Sprintf("%.2f", summary.DeviceRejectedPercent),
				attrRejectedCount:    strconv.FormatInt(summary.Rejected, 10),
				attrAcceptedCount:    strconv.FormatInt(summary.Accepted, 10),
				attrThresholdPercent: fmt.Sprintf("%.1f", rejectedPercentMajor),
			},
		})
	} else if summary.DeviceRejectedPercent >= rejectedPercentMinor {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashrateBelowTarget,
			Severity:          sdkerrors.SeverityMinor,
			Summary:           fmt.Sprintf("Device has %.2f%% share rejection rate (threshold %.1f%%)", summary.DeviceRejectedPercent, rejectedPercentMinor),
			CauseSummary:      causeElevatedShareRejection,
			RecommendedAction: actionCheckPoolConfig,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrRejectedPercent:  fmt.Sprintf("%.2f", summary.DeviceRejectedPercent),
				attrRejectedCount:    strconv.FormatInt(summary.Rejected, 10),
				attrAcceptedCount:    strconv.FormatInt(summary.Accepted, 10),
				attrThresholdPercent: fmt.Sprintf("%.1f", rejectedPercentMinor),
			},
		})
	}

	// Check stale shares
	if summary.Stale >= staleSharesThreshold {
		errors = append(errors, sdkerrors.DeviceError{
			MinerError:        sdkerrors.HashrateBelowTarget,
			Severity:          sdkerrors.SeverityMinor,
			Summary:           fmt.Sprintf("Device has %d stale shares (threshold %d)", summary.Stale, staleSharesThreshold),
			CauseSummary:      causeHighStaleShares,
			RecommendedAction: actionCheckPoolConfig,
			Impact:            impactReducedHashrate,
			ComponentType:     sdkerrors.ComponentTypeControlBoard,
			FirstSeenAt:       now,
			LastSeenAt:        now,
			DeviceID:          deviceID,
			VendorAttributes: map[string]string{
				attrStaleCount:       strconv.FormatInt(summary.Stale, 10),
				attrThresholdCount:   strconv.FormatInt(staleSharesThreshold, 10),
				attrPoolStalePercent: fmt.Sprintf("%.2f", summary.PoolStalePercent),
			},
		})
	}

	return errors
}

// detectPoolErrors checks pool connectivity issues.
// Only reports errors if ALL pools are not working (to avoid false alarms when failover pools exist).
func detectPoolErrors(pools []rpc.PoolInfo, deviceID string, now time.Time) []sdkerrors.DeviceError {
	if len(pools) == 0 {
		return nil
	}

	// First pass: check if at least one pool is working
	hasWorkingPool := false
	for _, pool := range pools {
		if pool.Status == poolStatusAlive &&
			pool.GetFailures < poolFailuresThreshold &&
			pool.RemoteFailures < poolFailuresThreshold {
			hasWorkingPool = true
			break
		}
	}

	// If any pool is working, don't report errors (failover is functioning)
	if hasWorkingPool {
		return nil
	}

	// All pools are experiencing issues - report errors for each problematic pool
	var errors []sdkerrors.DeviceError

	for _, pool := range pools {
		poolID := strconv.Itoa(pool.Pool)

		// Check pool status
		if pool.Status != poolStatusAlive {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.VendorErrorUnmapped,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Pool %d (%s) status is '%s' (expected '%s')", pool.Pool, pool.URL, pool.Status, poolStatusAlive),
				CauseSummary:      causePoolNotAlive,
				RecommendedAction: actionCheckPoolConfig,
				Impact:            impactPoolConnectivity,
				ComponentType:     sdkerrors.ComponentTypeUnspecified,
				ComponentID:       &poolID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					attrPoolURL:    pool.URL,
					attrPoolStatus: pool.Status,
					attrPoolIndex:  poolID,
				},
			})
			continue
		}

		// Check get failures
		if pool.GetFailures >= poolFailuresThreshold {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.VendorErrorUnmapped,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Pool %d (%s) has %d get failures (threshold %d)", pool.Pool, pool.URL, pool.GetFailures, poolFailuresThreshold),
				CauseSummary:      causeHighPoolGetFailures,
				RecommendedAction: actionCheckPoolConfig,
				Impact:            impactPoolConnectivity,
				ComponentType:     sdkerrors.ComponentTypeUnspecified,
				ComponentID:       &poolID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					attrPoolURL:        pool.URL,
					attrGetFailures:    strconv.FormatInt(pool.GetFailures, 10),
					attrRemoteFailures: strconv.FormatInt(pool.RemoteFailures, 10),
					attrThreshold:      strconv.FormatInt(poolFailuresThreshold, 10),
					attrPoolIndex:      poolID,
				},
			})
		}

		// Check remote failures
		if pool.RemoteFailures >= poolFailuresThreshold {
			errors = append(errors, sdkerrors.DeviceError{
				MinerError:        sdkerrors.VendorErrorUnmapped,
				Severity:          sdkerrors.SeverityMajor,
				Summary:           fmt.Sprintf("Pool %d (%s) has %d remote failures (threshold %d)", pool.Pool, pool.URL, pool.RemoteFailures, poolFailuresThreshold),
				CauseSummary:      causeHighPoolRemoteFailures,
				RecommendedAction: actionCheckPoolConfig,
				Impact:            impactPoolConnectivity,
				ComponentType:     sdkerrors.ComponentTypeUnspecified,
				ComponentID:       &poolID,
				FirstSeenAt:       now,
				LastSeenAt:        now,
				DeviceID:          deviceID,
				VendorAttributes: map[string]string{
					attrPoolURL:        pool.URL,
					attrGetFailures:    strconv.FormatInt(pool.GetFailures, 10),
					attrRemoteFailures: strconv.FormatInt(pool.RemoteFailures, 10),
					attrThreshold:      strconv.FormatInt(poolFailuresThreshold, 10),
					attrPoolIndex:      poolID,
				},
			})
		}
	}

	return errors
}

// Stats API-based error detection functions
// These functions use the stats.cgi Web API data for more accurate per-chain telemetry

// detectTemperatureErrorsFromStats checks chains for temperature issues using stats API data.
func detectTemperatureErrorsFromStats(chains []web.ChainStats, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, chain := range chains {
		// Calculate max temperature from the temp_chip array
		if len(chain.TempChip) == 0 {
			continue
		}

		var maxTemp float64
		for _, temp := range chain.TempChip {
			if temp > maxTemp {
				maxTemp = temp
			}
		}

		chainID := strconv.Itoa(chain.Index)
		vendorAttrs := map[string]string{
			attrChainIndex:   chainID,
			attrSerialNumber: chain.SN,
		}

		if maxTemp >= tempOverheatCriticalCelsius {
			errors = append(errors, createTemperatureError(
				sdkerrors.SeverityCritical, chain.Index, maxTemp, tempOverheatCriticalCelsius,
				deviceID, chainID, now, vendorAttrs,
			))
		} else if maxTemp >= tempOverheatMajorCelsius {
			errors = append(errors, createTemperatureError(
				sdkerrors.SeverityMajor, chain.Index, maxTemp, tempOverheatMajorCelsius,
				deviceID, chainID, now, vendorAttrs,
			))
		} else if maxTemp < tempUnderheatMinorCelsius {
			errors = append(errors, createTemperatureError(
				sdkerrors.SeverityMinor, chain.Index, maxTemp, tempUnderheatMinorCelsius,
				deviceID, chainID, now, vendorAttrs,
			))
		}
	}

	return errors
}

// detectHashboardStatusErrorsFromStats checks chain status using stats API data.
func detectHashboardStatusErrorsFromStats(chains []web.ChainStats, deviceID string, now time.Time, sleeping bool) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, chain := range chains {
		chainID := strconv.Itoa(chain.Index)

		// Check if chain is not hashing (RateReal is 0 or negative)
		if !sleeping && chain.RateReal <= 0 {
			vendorAttrs := map[string]string{
				attrRateRealGHS:  fmt.Sprintf("%.2f", chain.RateReal),
				attrChainIndex:   chainID,
				attrSerialNumber: chain.SN,
			}
			// Only include RateIdeal if it's a valid positive value
			if chain.RateIdeal > 0 {
				vendorAttrs[attrRateIdealGHS] = fmt.Sprintf("%.2f", chain.RateIdeal)
			}

			errors = append(errors, createHashboardStatusError(
				sdkerrors.SeverityMajor, chain.Index, statusNotHashing,
				deviceID, chainID, now, vendorAttrs,
			))
		}
	}

	return errors
}

// detectPerBoardHardwareErrorsFromStats checks per-chain hardware error rates using stats API data.
func detectPerBoardHardwareErrorsFromStats(chains []web.ChainStats, deviceID string, now time.Time) []sdkerrors.DeviceError {
	var errors []sdkerrors.DeviceError

	for _, chain := range chains {
		chainID := strconv.Itoa(chain.Index)
		vendorAttrs := map[string]string{
			attrChainIndex:   chainID,
			attrSerialNumber: chain.SN,
		}

		// Check hardware error percentage (HWP field)
		if chain.HWP >= hwErrorPercentMajor {
			errors = append(errors, createHardwareErrorError(
				sdkerrors.SeverityMajor, chain.Index, chain.HWP,
				chain.HW, fmt.Sprintf("%.1f%%", hwErrorPercentMajor),
				deviceID, chainID, now, vendorAttrs,
			))
		} else if chain.HWP >= hwErrorPercentMinor {
			errors = append(errors, createHardwareErrorError(
				sdkerrors.SeverityMinor, chain.Index, chain.HWP,
				chain.HW, fmt.Sprintf("%.1f%%", hwErrorPercentMinor),
				deviceID, chainID, now, vendorAttrs,
			))
		} else if chain.HW >= int(hwErrorCountMinor) {
			// Also check absolute count for boards with low total work
			errors = append(errors, createHardwareErrorError(
				sdkerrors.SeverityMinor, chain.Index, chain.HWP,
				chain.HW, strconv.FormatInt(hwErrorCountMinor, 10),
				deviceID, chainID, now, vendorAttrs,
			))
		}
	}

	return errors
}
