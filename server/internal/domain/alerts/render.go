package alerts

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// Slack limits: section text ≤3000, top-level fallback text should stay ≤4000, ≤50 blocks per message.
const (
	slackSectionMaxRunes  = 2900
	slackFallbackMaxRunes = 4000
	// Cap alert sections so the title and overflow line stay under Slack's 50-block limit.
	slackMaxAlertSections = 40
	// Keep the instance name from crowding the alert counts out of the truncated title.
	slackInstanceMaxRunes = 50
)

// Each alert line carries its own severity. An unrecognized or missing severity uses the critical dot:
// rules can carry any severity label, and a colour is a weak reason to under-signal one.
var slackSeverityDots = []struct{ severity, dot string }{
	{"critical", "🔴"},
	{"warning", "🟡"},
	{"info", "🔵"},
}

// renderSlack builds a Block Kit message that carries no alerting-engine internals. Alerts are rolled up per
// rule, so a fleet-wide outage is a handful of sections with device counts rather than thousands of device lines.
func renderSlack(publicURL string, alerts []Alert, _ map[string]DeviceIdentity) map[string]any {
	firing, resolved := partitionAlerts(alerts)
	firingGroups := groupAlerts(firing)
	resolvedGroups := groupAlerts(resolved)
	plain, linked := instanceLabels(publicURL)

	// Keep product and instance context in a neutral header. Severity belongs to each alert line so a
	// mixed batch is not represented by one umbrella icon.
	blocks := []map[string]any{mrkdwnSection("*" + linked + "*")}
	fallbackLines := []string{plain}
	remaining := slackMaxAlertSections
	appendGroups := func(groups []alertGroup, resolved bool) {
		for _, g := range groups {
			if remaining <= 0 {
				return
			}
			line := groupLine(g, resolved)
			blocks = append(blocks, mrkdwnSection(line))
			fallbackLines = append(fallbackLines, line)
			remaining--
		}
	}
	// A notification batch is one Alertmanager group, not a fleet-wide state snapshot. Retain each resolved
	// condition instead of turning a resolution-only group into a global all-clear.
	appendGroups(firingGroups, false)
	appendGroups(resolvedGroups, true)
	if overflow := len(firingGroups) + len(resolvedGroups) - slackMaxAlertSections; overflow > 0 {
		line := fmt.Sprintf("_…and %d more — open Proto Fleet for the full list._", overflow)
		blocks = append(blocks, mrkdwnSection(line))
		fallbackLines = append(fallbackLines, line)
	}

	// The top-level text is the notification/preview fallback for clients that don't render blocks. Include
	// every rendered line rather than reducing the preview to generic alert state.
	fallback := truncate(strings.Join(fallbackLines, "\n"), slackFallbackMaxRunes)
	return map[string]any{"text": fallback, "blocks": blocks}
}

// instanceLabels names the sending fleet: several instances can post to one channel, and nothing else in the
// message says which one fired, so the public URL's host names the sender and doubles as the link to it. It
// returns the plain label for the notification fallback and the linked one for the block. A value with no
// host is a misconfigured link, not an instance name.
func instanceLabels(publicURL string) (plain, linked string) {
	const product = "Proto Fleet"
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" {
		return product, product
	}
	host := truncate(u.Host, slackInstanceMaxRunes)
	// Only the block form is mrkdwn; escaping the fallback would show the entities themselves.
	return fmt.Sprintf("%s (%s)", product, host),
		fmt.Sprintf("%s (<%s|%s>)", product, publicURL, escapeMrkdwn(host))
}

func severityDot(severity string) string {
	severity = strings.TrimSpace(severity)
	for _, s := range slackSeverityDots {
		if strings.EqualFold(severity, s.severity) {
			return s.dot
		}
	}
	return slackSeverityDots[0].dot
}

// groupLine renders one rule's rollup as natural language. It intentionally omits individual device
// identifiers: one fleet-wide event can cover thousands of devices, and the linked app holds the drill-in.
func groupLine(g alertGroup, resolved bool) string {
	if resolved {
		return "✅ " + escapeMrkdwn(resolvedGroupCopy(g))
	}
	dot := severityDot(g.Severity)
	if g.DeviceCount > 0 {
		return dot + " " + escapeMrkdwn(activeDeviceGroupCopy(g))
	}
	return devicelessGroupLine(dot, g)
}

func activeDeviceGroupCopy(g alertGroup) string {
	summary := firstSummary(g)
	count := fmt.Sprintf("%d device%s", g.DeviceCount, plural(g.DeviceCount))
	switch RuleTemplate(g.Template) {
	case RuleTemplateOffline:
		if rest, ok := strings.CutPrefix(summary, "Device is offline"); ok {
			return customRuleDeviceCopy(g, count+" unreachable"+rest)
		}
	case RuleTemplateHashrate:
		for _, prefix := range []string{"Device hashrate has fallen below ", "Device hashrate is below "} {
			if rest, ok := strings.CutPrefix(summary, prefix); ok {
				return customRuleDeviceCopy(g, count+" hashing below "+rest)
			}
		}
	case RuleTemplateTemperature:
		if rest, ok := strings.CutPrefix(summary, "Max sensor temperature for device is "); ok {
			return customRuleDeviceCopy(g, count+" with sensor temperatures "+rest)
		}
	case RuleTemplatePool,
		RuleTemplateCommandFailure,
		RuleTemplateTelemetryPoll,
		RuleTemplateMQTTCurtailment,
		RuleTemplateMQTTDisconnected,
		RuleTemplateCurtailmentFanRestore,
		RuleTemplateMetricIngest,
		RuleTemplateHAReadiness:
		// These templates are device-less today or have no specialized device copy.
	default:
		// Other and future templates use their rule-provided summary below.
	}
	if summary != "" {
		return customRuleDeviceCopy(g, summary+" ("+count+" affected)")
	}
	return fmt.Sprintf("%s affecting %s", g.Name, count)
}

// Custom rule names carry the operator's scope or intent (for example, which site the rule watches).
// Provisioned rules keep the shorter condition-only copy because their names add no new context.
func customRuleDeviceCopy(g alertGroup, condition string) string {
	if !strings.HasPrefix(g.RuleGroup, "proto-fleet-user-") {
		return condition
	}
	return sentenceText(g.Name) + ": " + condition
}

func resolvedGroupCopy(g alertGroup) string {
	if g.DeviceCount > 0 {
		count := fmt.Sprintf("%d device%s", g.DeviceCount, plural(g.DeviceCount))
		switch RuleTemplate(g.Template) {
		case RuleTemplateOffline:
			return customRuleDeviceCopy(g, count+" reachable again")
		case RuleTemplateHashrate:
			return customRuleDeviceCopy(g, count+" hashing above alert threshold again")
		case RuleTemplateTemperature:
			return customRuleDeviceCopy(g, count+" below temperature threshold again")
		case RuleTemplatePool,
			RuleTemplateCommandFailure,
			RuleTemplateTelemetryPoll,
			RuleTemplateMQTTCurtailment,
			RuleTemplateMQTTDisconnected,
			RuleTemplateCurtailmentFanRestore,
			RuleTemplateMetricIngest,
			RuleTemplateHAReadiness:
			// Use the generic rule wording below for templates without device recovery copy.
		default:
			// Unknown templates use the same generic rule wording.
		}
		return fmt.Sprintf("%s resolved for %s", sentenceText(g.Name), count)
	}
	switch RuleTemplate(g.Template) {
	case RuleTemplateTelemetryPoll:
		return "Telemetry polling recovered"
	case RuleTemplateMQTTCurtailment:
		if source, ok := strings.CutPrefix(firstSummary(g), "Miners are curtailed by "); ok && source != "" {
			return "Curtailment by " + source + " ended"
		}
		return sentenceText(g.Name) + " resolved"
	case RuleTemplateMQTTDisconnected:
		return resolvedMQTTDisconnectedCopy(g)
	case RuleTemplateCurtailmentFanRestore:
		return fmt.Sprintf("Fan control restored for %d curtailment event%s", g.InstanceCount, plural(g.InstanceCount))
	case RuleTemplateMetricIngest:
		return "Metric ingest resumed"
	case RuleTemplateHAReadiness:
		return "HA ready to fail over"
	case RuleTemplateFleetNodeUnavailable:
		return resolvedFleetNodeUnavailableCopy(g)
	case RuleTemplateOffline, RuleTemplateHashrate, RuleTemplateTemperature,
		RuleTemplatePool, RuleTemplateCommandFailure:
		// Device-backed templates without a device label use the generic instance wording below.
	default:
		// Unknown templates use the same generic instance wording.
	}
	if g.InstanceCount > 1 {
		return fmt.Sprintf("%s resolved for %d instances", sentenceText(g.Name), g.InstanceCount)
	}
	return sentenceText(g.Name) + " resolved"
}

func resolvedMQTTDisconnectedCopy(g alertGroup) string {
	fallback := fmt.Sprintf("%d curtailment source%s reachable again", g.InstanceCount, plural(g.InstanceCount))
	if len(g.Summaries) == 0 {
		return fallback
	}
	lines := make([]string, 0, len(g.Summaries)+1)
	for _, summary := range g.Summaries {
		source, ok := strings.CutPrefix(sentenceText(summary), "Curtailment source ")
		if !ok {
			return fallback
		}
		source, ok = strings.CutSuffix(source, " is unreachable; cannot curtail")
		if !ok || source == "" {
			return fallback
		}
		lines = append(lines, "Curtailment source "+source+" reachable again")
	}
	if more := g.SummaryCount - len(g.Summaries); more > 0 {
		lines = append(lines, fmt.Sprintf("…and %d more curtailment source%s reachable again", more, plural(more)))
	}
	return strings.Join(lines, "\n✅ ")
}

func resolvedFleetNodeUnavailableCopy(g alertGroup) string {
	fallback := fmt.Sprintf("Connection%s restored to %d Fleet Node%s", plural(g.InstanceCount), g.InstanceCount, plural(g.InstanceCount))
	if len(g.Summaries) == 0 {
		return fallback
	}
	lines := make([]string, 0, len(g.Summaries)+1)
	for _, summary := range g.Summaries {
		node, ok := strings.CutPrefix(sentenceText(summary), "Fleet Node ")
		if !ok {
			return fallback
		}
		node, ok = strings.CutSuffix(node, " is unavailable")
		if !ok || node == "" {
			return fallback
		}
		lines = append(lines, "Connection restored to Fleet Node "+node)
	}
	if more := g.SummaryCount - len(g.Summaries); more > 0 {
		lines = append(lines, fmt.Sprintf("…and %d more Fleet Node connection%s restored", more, plural(more)))
	}
	return strings.Join(lines, "\n✅ ")
}

func devicelessGroupLine(dot string, g alertGroup) string {
	if len(g.Summaries) == 0 {
		if g.InstanceCount > 1 {
			return fmt.Sprintf("%s %d instances affected by %s", dot, g.InstanceCount, escapeMrkdwn(g.Name))
		}
		return dot + " " + escapeMrkdwn(sentenceText(g.Name))
	}
	if len(g.Summaries) == 1 && g.InstanceCount > 1 {
		affected := fmt.Sprintf("%d instance%s", g.InstanceCount, plural(g.InstanceCount))
		if RuleTemplate(g.Template) == RuleTemplateCurtailmentFanRestore {
			affected = fmt.Sprintf("%d curtailment event%s", g.InstanceCount, plural(g.InstanceCount))
		}
		return fmt.Sprintf("%s %s (%s affected)", dot, escapeMrkdwn(sentenceText(g.Summaries[0])), affected)
	}
	lines := make([]string, 0, len(g.Summaries)+1)
	for _, summary := range g.Summaries {
		lines = append(lines, dot+" "+escapeMrkdwn(sentenceText(summary)))
	}
	if more := g.SummaryCount - len(g.Summaries); more > 0 {
		lines = append(lines, fmt.Sprintf("_…and %d more_", more))
	}
	return strings.Join(lines, "\n")
}

func firstSummary(g alertGroup) string {
	if len(g.Summaries) == 0 {
		return ""
	}
	return sentenceText(g.Summaries[0])
}

func sentenceText(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), ".")
}

// escapeMrkdwn neutralizes the mrkdwn chars that let user-controlled text escape its own field: the link syntax,
// and the backtick, since a code span opened in one field closes at the next backtick anywhere in the section.
// It does not touch `*`, `_`, `~`, or `:shortcode:`, so user text can still format itself and interpolate emoji.
func escapeMrkdwn(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

// The cap lives here rather than at each call site, so no section can exceed it however it was built.
func mrkdwnSection(text string) map[string]any {
	return map[string]any{
		"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": truncate(text, slackSectionMaxRunes)},
	}
}

// Distinct summaries named inline before the "+N more" tail, for rules that interpolate the alert instance.
const groupSampleSummaries = 3

// alertGroup is one rule's rollup within a delivery batch. Every outward-facing renderer groups through it.
type alertGroup struct {
	Identity  string
	RuleUID   string
	Name      string
	RuleGroup string
	Severity  string
	Template  string
	// Distinct instance summaries, capped at groupSampleSummaries; SummaryCount is how many there were. Most
	// rules render summary from the rule, so a group has one; the ones that interpolate the firing instance
	// ("Curtailment source maestro-b is unreachable") have one per instance, and no single one describes them.
	Summaries    []string
	SummaryCount int
	// Instances in the group, which exceeds DeviceCount only when a rule fires on a non-device dimension.
	InstanceCount int
	DeviceCount   int
	// Dedupe sets behind DeviceCount and SummaryCount, released once the group is materialized.
	devices   map[string]bool
	summaries map[string]bool
}

// groupAlerts rolls a batch up per firing rule, widest blast radius first. RuleUID is authoritative;
// copy-defining fields separate legacy alerts that predate rule identity propagation.
func groupAlerts(alerts []Alert) []alertGroup {
	type groupKey struct{ name, ruleGroup, identity string }
	byKey := map[groupKey]*alertGroup{}
	for _, a := range alerts {
		name := a.Labels["alertname"]
		if name == "" {
			name = "Alert"
		}
		identity := alertGroupIdentity(a)
		key := groupKey{name, a.Labels[ruleLabelRuleGroup], identity}
		g := byKey[key]
		if g == nil {
			g = &alertGroup{
				Identity:  identity,
				RuleUID:   alertRuleUID(a),
				Name:      name,
				RuleGroup: a.Labels[ruleLabelRuleGroup],
				Severity:  a.Labels["severity"],
				Template:  a.Labels[ruleLabelTemplate],
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
			strings.Compare(a.Identity, b.Identity),
		)
	})
	return out
}

// alertGroupIdentity keeps distinct rules separate even when user-selected names collide. Current alerts carry
// RuleUID; legacy/unattributed alerts fall back to stable rule-level fields. Summary is intentionally excluded:
// some rules interpolate the alert instance there and still need all of their instances rolled up together.
func alertGroupIdentity(a Alert) string {
	if uid := alertRuleUID(a); uid != "" {
		return "uid:" + uid
	}
	return strings.Join([]string{
		"legacy",
		a.Labels[ruleLabelTemplate],
		a.Labels["severity"],
	}, "\x00")
}

func alertRuleUID(a Alert) string {
	if uid := strings.TrimSpace(a.RuleUID); uid != "" {
		return uid
	}
	return strings.TrimSpace(a.Labels[ruleLabelRuleUID])
}

// webhookAlert is the clean, Grafana-free shape delivered to generic webhook channels.
type webhookAlert struct {
	Status     string `json:"status"`
	RuleUID    string `json:"rule_uid,omitempty"`
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
	RuleUID   string `json:"rule_uid,omitempty"`
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
				RuleUID:    alertRuleUID(a),
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
				RuleUID:     g.RuleUID,
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
		"organization_id":   orgID,
		"firing":            convert(firing),
		alertStatusResolved: convert(resolved),
		"firing_groups":     convertGroups(firing),
		"resolved_groups":   convertGroups(resolved),
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
