package ct1

import (
	"sort"
	"strings"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Reuse the bulk map: board causes retain their MAC, while repeated PSU
// conditions belong to the module PSU. Historical shutdown errors are excluded.
func moduleIssues(boards []Board, now time.Time, identityConflict bool) *sdk.ContainerModuleIssues {
	out := &sdk.ContainerModuleIssues{Complete: len(boards) == boardsPerModule && !identityConflict, SourceTimes: map[string]time.Time{}}
	if identityConflict {
		return out
	}
	issues := map[string]sdk.ContainerModuleIssue{}
	add := func(component, id, cause, severity string, at time.Time) {
		if strings.TrimSpace(cause) == "" {
			return
		}
		if at.After(now) {
			at = now
		}
		key := component + ":" + id + ":" + cause
		old, exists := issues[key]
		if !exists || at.After(old.ObservedAt) || (at.Equal(old.ObservedAt) && severity == "fault") {
			issues[key] = sdk.ContainerModuleIssue{Component: component, ComponentID: id, Cause: cause, Severity: severity, ObservedAt: at}
		}
	}
	var psu *PSUThermal
	var psuAt time.Time
	states := map[string]bool{}
	for _, b := range boards {
		h := b.Health
		if h != nil && !h.Timestamp.IsZero() && (out.ObservedAt.IsZero() || h.Timestamp.Before(out.ObservedAt)) {
			out.ObservedAt = h.Timestamp
		}
		if h == nil || h.Timestamp.IsZero() || now.Sub(h.Timestamp) >= healthMaxAge || h.Timestamp.After(now.Add(clockSkewAllowance)) {
			out.Complete = false
			continue
		}
		sourceAt := h.Timestamp
		if sourceAt.After(now) {
			sourceAt = now
		}
		out.SourceTimes["hashboard:"+b.MACAddress] = sourceAt
		if h.Errors == nil || h.Warnings == nil {
			out.Complete = false
		}
		states[b.MiningState] = true
		for _, group := range []struct {
			items    []Issue
			severity string
		}{{h.Errors, "fault"}, {h.Warnings, "warning"}} {
			for _, issue := range group.items {
				component, id := "hashboard", b.MACAddress
				if strings.HasPrefix(issue.Cause, "PSU_") || strings.Contains(issue.Cause, "BOARD_POWER_SUPPLY") {
					component, id = "psu", "psu"
				}
				cause := issue.Cause
				if cause == "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE" {
					cause = "PSU_OVER_TEMPERATURE"
				}
				add(component, id, cause, group.severity, h.Timestamp)
			}
		}
		if b.MiningState == miningStateMining {
			if h.HashrailOK != nil && !*h.HashrailOK {
				add("hashboard", b.MACAddress, "HASHRAIL_NOT_OK", "fault", h.Timestamp)
			}
			if h.RefclksOK != nil && !*h.RefclksOK {
				add("hashboard", b.MACAddress, "REFERENCE_CLOCKS_NOT_OK", "fault", h.Timestamp)
			}
		}
		if p := h.PSUThermal; p != nil && p.ThermalStateValid {
			at := h.Timestamp
			if p.ThermalStateAgeMs != nil {
				if !nonnegative(p.ThermalStateAgeMs) || *p.ThermalStateAgeMs >= float64(healthMaxAge.Milliseconds()) {
					continue
				}
				at = at.Add(-time.Duration(*p.ThermalStateAgeMs * float64(time.Millisecond)))
			}
			if now.Sub(at) < healthMaxAge && (psu == nil || at.After(psuAt)) {
				state := *p
				psu, psuAt = &state, at
			} else if psu != nil && at.Equal(psuAt) {
				psu.OverTemperatureFault = psu.OverTemperatureFault || p.OverTemperatureFault
				psu.OverTemperatureWarning = psu.OverTemperatureWarning || p.OverTemperatureWarning
			}
		}
	}
	if out.ObservedAt.After(now) {
		out.ObservedAt = now
	}
	if psu != nil {
		out.SourceTimes["psu"] = psuAt
		if psuAt.Before(out.ObservedAt) {
			out.ObservedAt = psuAt
		}
		if psu.OverTemperatureFault {
			add("psu", "psu", "PSU_OVER_TEMPERATURE", "fault", psuAt)
		}
		if psu.OverTemperatureWarning {
			add("psu", "psu", "PSU_OVER_TEMPERATURE", "warning", psuAt)
		}
	}
	if out.Complete && len(states) > 1 {
		add("module", "module", "MIXED_BOARD_STATES", "observation", out.ObservedAt)
	}
	if psu == nil {
		out.Complete = false
	}
	for _, issue := range issues {
		out.Items = append(out.Items, issue)
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		rank := map[string]int{"fault": 0, "warning": 1, "observation": 2}
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		return a.Component+":"+a.ComponentID+":"+a.Cause < b.Component+":"+b.ComponentID+":"+b.Cause
	})
	return out
}
