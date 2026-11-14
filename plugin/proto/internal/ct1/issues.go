package ct1

import (
	"sort"
	"strings"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Firmware causes are short enum names. Bound their retained count and bytes
// so verbose controller health cannot crowd out telemetry on the plugin RPC.
const maxModuleIssues = 64
const maxIssueCauseBytes = 256
const maxIssueComponentIDBytes = 128
const maxContainerIssueBytes = 1 << 20

// Reserve strings plus ample protobuf field/timestamp overhead. The shared
// budget includes module wrappers and source-time keys as well as issue items.
type issueBudget struct {
	remaining int
}

func (b *issueBudget) retain(bytes int) bool {
	if bytes > b.remaining {
		return false
	}
	b.remaining -= bytes
	return true
}

// Reuse the bulk map: board causes retain their MAC, while repeated PSU
// conditions belong to the module PSU. Historical shutdown errors are excluded.
func moduleIssues(boards []Board, now time.Time, identityConflict bool) *sdk.ContainerModuleIssues {
	budget := issueBudget{remaining: maxContainerIssueBytes}
	return moduleIssuesWithBudget(boards, now, identityConflict, &budget)
}

func moduleIssuesWithBudget(boards []Board, now time.Time, identityConflict bool, budget *issueBudget) *sdk.ContainerModuleIssues {
	issues, _ := moduleIssueEvidence(boards, now, identityConflict, budget)
	return issues
}

// issueClaim is one fresh sample's own claim of an issue.
type issueClaim struct {
	// source is the sample's SourceTimes key.
	source   string
	severity string
	at       time.Time
}

// issueSources describes the fresh samples behind a module issue report. Error
// polling needs what the report does not keep: each sample's own time and its
// own claims. A power-supply cause belongs to the board that reported it as
// well as to the shared PSU reading, and the two can disagree.
type issueSources struct {
	// sampledAt is each fresh sample's time by SourceTimes key:
	// "hashboard:<mac>" for a board sample and "psu" for the shared thermal
	// state. Unlike SourceTimes it is not capped at the collection time and it
	// survives a truncated report, so it orders one source's samples exactly.
	sampledAt map[string]time.Time
	// claims lists, for each issue, the samples that asserted it.
	claims map[string][]issueClaim
}

func issueClaimKey(issue sdk.ContainerModuleIssue) string {
	return issue.Component + ":" + issue.ComponentID + ":" + issue.Cause
}

// claim keeps one claim per sample. A sample that lists a cause as both a
// fault and a warning claims the fault.
func (s issueSources) claim(key, source, severity string, at time.Time) {
	for i := range s.claims[key] {
		if s.claims[key][i].source == source {
			if severity == "fault" {
				s.claims[key][i].severity = severity
			}
			return
		}
	}
	s.claims[key] = append(s.claims[key], issueClaim{source: source, severity: severity, at: at})
}

func moduleIssueEvidence(boards []Board, now time.Time, identityConflict bool, budget *issueBudget) (*sdk.ContainerModuleIssues, issueSources) {
	if !budget.retain(64) {
		return nil, issueSources{}
	}
	out := &sdk.ContainerModuleIssues{Complete: len(boards) == boardsPerModule && !identityConflict, SourceTimes: map[string]time.Time{}}
	if identityConflict {
		return out, issueSources{}
	}
	issues := map[string]sdk.ContainerModuleIssue{}
	sources := issueSources{sampledAt: map[string]time.Time{}, claims: map[string][]issueClaim{}}
	truncated := false
	sourceKey := func(component, id string) string {
		if id == "" {
			return component
		}
		return component + ":" + id
	}
	addSource := func(component, id string, at time.Time) {
		if len(id) > maxIssueComponentIDBytes {
			truncated = true
			return
		}
		key := sourceKey(component, id)
		if _, exists := out.SourceTimes[key]; !exists && !budget.retain(len(key)+64) {
			truncated = true
			return
		}
		out.SourceTimes[key] = at
	}
	add := func(origin, component, id, cause, severity string, at time.Time) {
		if len(cause) > maxIssueCauseBytes || len(id) > maxIssueComponentIDBytes {
			truncated = true
			return
		}
		if strings.TrimSpace(cause) == "" {
			return
		}
		if at.After(now) {
			at = now
		}
		key := component + ":" + id + ":" + cause
		code, _ := canonicalIssue(cause, severity == "warning")
		old, exists := issues[key]
		if !exists && (len(issues) >= maxModuleIssues || !budget.retain(len(component)+len(id)+len(cause)+128)) {
			truncated = true
			return
		}
		sources.claim(key, origin, severity, at)
		if !exists || at.After(old.ObservedAt) || (at.Equal(old.ObservedAt) && severity == "fault") {
			issues[key] = sdk.ContainerModuleIssue{Component: component, ComponentID: id, Cause: cause, Severity: severity, CanonicalError: code, ObservedAt: at}
		}
	}
	var psu *PSUThermal
	var psuAt time.Time
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
		// Use the inventory selector for both faults and clear-reading watermarks.
		b.MACAddress = normalizedBoardMAC(b.MACAddress)
		addSource("hashboard", b.MACAddress, sourceAt)
		origin := sourceKey("hashboard", b.MACAddress)
		sources.sampledAt[origin] = h.Timestamp
		if h.Errors == nil || h.Warnings == nil {
			out.Complete = false
		}
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
				add(origin, component, id, cause, group.severity, h.Timestamp)
			}
		}
		if b.MiningState == miningStateMining {
			if h.HashrailOK != nil && !*h.HashrailOK {
				add(origin, "hashboard", b.MACAddress, "HASHRAIL_NOT_OK", "fault", h.Timestamp)
			}
			if h.RefclksOK != nil && !*h.RefclksOK {
				add(origin, "hashboard", b.MACAddress, "REFERENCE_CLOCKS_NOT_OK", "fault", h.Timestamp)
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
		addSource("psu", "", psuAt)
		sources.sampledAt["psu"] = psuAt
		if psuAt.Before(out.ObservedAt) {
			out.ObservedAt = psuAt
		}
		if psu.OverTemperatureFault {
			add("psu", "psu", "psu", "PSU_OVER_TEMPERATURE", "fault", psuAt)
		}
		if psu.OverTemperatureWarning {
			add("psu", "psu", "psu", "PSU_OVER_TEMPERATURE", "warning", psuAt)
		}
	}
	if psu == nil {
		out.Complete = false
	}
	if truncated {
		out.Complete = false
		// Omitted causes are not clear readings. Do not consume source
		// watermarks that a later untruncated report needs to recover.
		out.SourceTimes = nil
	}
	for _, issue := range issues {
		out.Items = append(out.Items, issue)
	}
	rank := map[string]int{"fault": 0, "warning": 1, "observation": 2}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if a.Severity != b.Severity {
			return rank[a.Severity] < rank[b.Severity]
		}
		return a.Component+":"+a.ComponentID+":"+a.Cause < b.Component+":"+b.ComponentID+":"+b.Cause
	})
	return out, sources
}
