package alerts

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Slack limits: header plain_text ≤150 chars, section text ≤3000, ≤50 blocks per message.
const (
	slackHeaderMaxRunes  = 150
	slackSectionMaxRunes = 2900
	// Cap alert sections so header + link + 2 headings + overflow line stay under Slack's 50-block limit.
	slackMaxAlertSections = 40
)

// renderSlack builds a Block Kit message that carries no alerting-engine internals. Alerts are rolled up per
// rule, so a fleet-wide outage is a handful of sections with miner counts rather than thousands of miner lines.
func renderSlack(publicURL string, alerts []Alert, identities map[string]DeviceIdentity) map[string]any {
	firing, resolved := partitionAlerts(alerts)
	firingGroups := groupAlerts(firing)
	resolvedGroups := groupAlerts(resolved)
	title := slackTitle(firingGroups, distinctDevices(firing))

	blocks := []map[string]any{headerBlock(title)}
	if publicURL != "" {
		blocks = append(blocks, mrkdwnSection(fmt.Sprintf("<%s|Open Proto Fleet>", publicURL)))
	}
	remaining := slackMaxAlertSections
	appendSection := func(heading string, groups []alertGroup) {
		if len(groups) == 0 {
			return
		}
		blocks = append(blocks, mrkdwnSection("*"+heading+"*"))
		for _, g := range groups {
			if remaining <= 0 {
				break
			}
			blocks = append(blocks, mrkdwnSection(truncate(groupLine(g, identities), slackSectionMaxRunes)))
			remaining--
		}
	}
	appendSection("Firing", firingGroups)
	appendSection("Resolved", resolvedGroups)
	if overflow := len(firingGroups) + len(resolvedGroups) - slackMaxAlertSections; overflow > 0 {
		blocks = append(blocks, mrkdwnSection(fmt.Sprintf("_…and %d more — open Proto Fleet for the full list._", overflow)))
	}

	// The top-level text is the notification/preview fallback for clients that don't render blocks.
	return map[string]any{"text": title, "blocks": blocks}
}

func slackTitle(firing []alertGroup, firingDevices int) string {
	if len(firing) == 0 {
		return "✅ Proto Fleet — alerts resolved"
	}
	base := fmt.Sprintf("🔴 Proto Fleet — %d alert%s firing", len(firing), plural(len(firing)))
	// Fleet- and source-scoped alerts have no miners to count; don't claim "on 0 miners".
	if firingDevices == 0 {
		return base
	}
	return fmt.Sprintf("%s on %d miner%s", base, firingDevices, plural(firingDevices))
}

// groupLine renders one rule's rollup for Slack: the alert, its blast radius, and enough miner names to act on.
// Labels are escaped here, not in the rollup, so a non-Slack renderer formats the same group its own way.
func groupLine(g alertGroup, identities map[string]DeviceIdentity) string {
	var b strings.Builder
	b.WriteString("*" + escapeMrkdwn(g.Name) + "*")
	if g.Severity != "" {
		b.WriteString(" _(" + escapeMrkdwn(g.Severity) + ")_")
	}
	switch {
	case g.DeviceCount == 0:
		// No miners to name, but a rule that fires on another dimension (per curtailment source, per host)
		// still has one instance per affected thing; say how many so none of them go unreported.
		if g.InstanceCount > 1 {
			b.WriteString(fmt.Sprintf(" — %d instances", g.InstanceCount))
		}
	case g.DeviceCount == 1:
		// A single miner reads better named inline than as "1 miner" plus a list of one.
		b.WriteString(" — " + deviceLabel(g.SampleDeviceIDs[0], identities))
	default:
		b.WriteString(fmt.Sprintf(" — %d miners", g.DeviceCount))
		labels := make([]string, 0, len(g.SampleDeviceIDs))
		for _, id := range g.SampleDeviceIDs {
			labels = append(labels, deviceLabel(id, identities))
		}
		b.WriteString("\n" + strings.Join(labels, ", "))
		if more := g.DeviceCount - len(g.SampleDeviceIDs); more > 0 {
			b.WriteString(fmt.Sprintf(" and %d more", more))
		}
	}
	for _, summary := range g.Summaries {
		b.WriteString("\n" + escapeMrkdwn(summary))
	}
	if more := g.SummaryCount - len(g.Summaries); more > 0 {
		b.WriteString(fmt.Sprintf("\n_…and %d more_", more))
	}
	return b.String()
}

// escapeMrkdwn escapes Slack's reserved mrkdwn chars so user-controlled text can't break rendering or inject a `<url|text>` link.
func escapeMrkdwn(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func headerBlock(text string) map[string]any {
	return map[string]any{
		"type": "header",
		"text": map[string]any{"type": "plain_text", "text": truncate(text, slackHeaderMaxRunes), "emoji": true},
	}
}

func mrkdwnSection(text string) map[string]any {
	return map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": text},
	}
}

// Miners named inline per alert before the "+N more" tail; the app holds the full list.
const groupSampleDevices = 3

// Distinct summaries named inline before the "+N more" tail, for rules that interpolate the firing instance.
const groupSampleSummaries = 3

// alertGroup is one rule's rollup within a delivery batch. Every outward-facing renderer groups through it, so
// it carries device ids rather than display labels: each medium formats and escapes them its own way.
type alertGroup struct {
	Name      string
	RuleGroup string
	Severity  string
	// Distinct instance summaries, capped at groupSampleSummaries; SummaryCount is how many there were. Most
	// rules render summary from the rule, so a group has one; the ones that interpolate the firing instance
	// ("Curtailment source maestro-b is unreachable") have one per instance, and no single one describes them.
	Summaries    []string
	SummaryCount int
	// Instances in the group, which exceeds DeviceCount only when a rule fires on a non-device dimension.
	InstanceCount   int
	DeviceCount     int
	SampleDeviceIDs []string
	// Dedupe sets behind DeviceCount and SummaryCount, released once the group is materialized.
	devices   map[string]bool
	summaries map[string]bool
}

// groupAlerts rolls a batch up per firing rule (identified by alertname + rule group, matching the API's
// active rollup), widest blast radius first.
func groupAlerts(alerts []Alert) []alertGroup {
	type groupKey struct{ name, ruleGroup string }
	byKey := map[groupKey]*alertGroup{}
	for _, a := range alerts {
		name := a.Labels["alertname"]
		if name == "" {
			name = "Alert"
		}
		key := groupKey{name, a.Labels[ruleLabelRuleGroup]}
		g := byKey[key]
		if g == nil {
			g = &alertGroup{
				Name:      name,
				RuleGroup: a.Labels[ruleLabelRuleGroup],
				Severity:  a.Labels["severity"],
				devices:   map[string]bool{},
				summaries: map[string]bool{},
			}
			byKey[key] = g
		}
		g.InstanceCount++
		if summary := a.Annotations["summary"]; summary != "" && !g.summaries[summary] {
			g.summaries[summary] = true
			g.SummaryCount++
			if len(g.Summaries) < groupSampleSummaries {
				g.Summaries = append(g.Summaries, summary)
			}
		}
		id := a.Labels["device_id"]
		if id == "" || g.devices[id] {
			continue
		}
		g.devices[id] = true
		if len(g.SampleDeviceIDs) < groupSampleDevices {
			g.SampleDeviceIDs = append(g.SampleDeviceIDs, id)
		}
	}
	out := make([]alertGroup, 0, len(byKey))
	for _, g := range byKey {
		g.DeviceCount = len(g.devices)
		g.devices, g.summaries = nil, nil
		out = append(out, *g)
	}
	// Widest blast radius first; name then rule group break every tie, so map order can't leak into the output.
	slices.SortFunc(out, func(a, b alertGroup) int {
		return cmp.Or(
			cmp.Compare(b.DeviceCount, a.DeviceCount),
			strings.Compare(a.Name, b.Name),
			strings.Compare(a.RuleGroup, b.RuleGroup),
		)
	})
	return out
}

// distinctDevices counts the miners a batch covers in total, for the "N alerts on M miners" headline.
func distinctDevices(alerts []Alert) int {
	seen := map[string]bool{}
	for _, a := range alerts {
		if id := a.Labels["device_id"]; id != "" {
			seen[id] = true
		}
	}
	return len(seen)
}

func deviceLabel(id string, identities map[string]DeviceIdentity) string {
	ident := identities[id]
	name := escapeMrkdwn(strings.TrimSpace(ident.Name))
	mac := escapeMrkdwn(ident.MAC)
	switch {
	case name != "" && mac != "":
		return fmt.Sprintf("%s (%s)", name, mac)
	case name != "":
		return name
	case mac != "":
		return mac
	default:
		return escapeMrkdwn(id)
	}
}

// webhookAlert is the clean, Grafana-free shape delivered to generic webhook channels.
type webhookAlert struct {
	Status     string `json:"status"`
	AlertName  string `json:"alert_name"`
	Severity   string `json:"severity,omitempty"`
	Summary    string `json:"summary,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	DeviceMAC  string `json:"device_mac,omitempty"`
}

// webhookAlertGroup is the alert-first rollup of the batch. The per-miner detail stays in firing/resolved, so
// a consumer that only wants "what is firing and how bad" reads the groups and ignores the instance arrays.
type webhookAlertGroup struct {
	AlertName string `json:"alert_name"`
	Severity  string `json:"severity,omitempty"`
	RuleGroup string `json:"rule_group,omitempty"`
	// One of the group's distinct summaries; the per-instance arrays carry every instance's own.
	Summary     string `json:"summary,omitempty"`
	DeviceCount int    `json:"device_count"`
	AlertCount  int    `json:"alert_count"`
}

func renderWebhook(orgID int64, alerts []Alert, identities map[string]DeviceIdentity) map[string]any {
	firing, resolved := partitionAlerts(alerts)
	convert := func(list []Alert) []webhookAlert {
		out := make([]webhookAlert, 0, len(list))
		for _, a := range list {
			id := a.Labels["device_id"]
			ident := identities[id]
			out = append(out, webhookAlert{
				Status:     a.Status,
				AlertName:  a.Labels["alertname"],
				Severity:   a.Labels["severity"],
				Summary:    a.Annotations["summary"],
				DeviceID:   id,
				DeviceName: strings.TrimSpace(ident.Name),
				DeviceMAC:  ident.MAC,
			})
		}
		return out
	}
	convertGroups := func(list []Alert) []webhookAlertGroup {
		groups := groupAlerts(list)
		out := make([]webhookAlertGroup, 0, len(groups))
		for _, g := range groups {
			summary := ""
			if len(g.Summaries) > 0 {
				summary = g.Summaries[0]
			}
			out = append(out, webhookAlertGroup{
				AlertName:   g.Name,
				Severity:    g.Severity,
				RuleGroup:   g.RuleGroup,
				Summary:     summary,
				DeviceCount: g.DeviceCount,
				AlertCount:  g.InstanceCount,
			})
		}
		return out
	}
	return map[string]any{
		"organization_id": orgID,
		"firing":          convert(firing),
		"resolved":        convert(resolved),
		"firing_groups":   convertGroups(firing),
		"resolved_groups": convertGroups(resolved),
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// truncate caps s to maxRunes, counting runes (not bytes) so it never splits a UTF-8 sequence.
func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}
