package alerts

import (
	"fmt"
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

// renderSlack builds a Block Kit message that carries no alerting-engine internals.
func renderSlack(publicURL string, alerts []Alert, identities map[string]DeviceIdentity) map[string]any {
	firing, resolved := partitionAlerts(alerts)
	title := slackTitle(firing)

	blocks := []map[string]any{headerBlock(title)}
	if publicURL != "" {
		blocks = append(blocks, mrkdwnSection(fmt.Sprintf("<%s|Open Proto Fleet>", publicURL)))
	}
	remaining := slackMaxAlertSections
	appendSection := func(heading string, list []Alert) {
		if len(list) == 0 {
			return
		}
		blocks = append(blocks, mrkdwnSection("*"+heading+"*"))
		for _, a := range list {
			if remaining <= 0 {
				break
			}
			blocks = append(blocks, mrkdwnSection(truncate(alertLine(a, identities), slackSectionMaxRunes)))
			remaining--
		}
	}
	appendSection("Firing", firing)
	appendSection("Resolved", resolved)
	if overflow := len(firing) + len(resolved) - slackMaxAlertSections; overflow > 0 {
		blocks = append(blocks, mrkdwnSection(fmt.Sprintf("_…and %d more — open Proto Fleet for the full list._", overflow)))
	}

	// The top-level text is the notification/preview fallback for clients that don't render blocks.
	return map[string]any{"text": title, "blocks": blocks}
}

func slackTitle(firing []Alert) string {
	if len(firing) > 0 {
		return fmt.Sprintf("🔴 Proto Fleet — %d alert%s firing", len(firing), plural(len(firing)))
	}
	return "✅ Proto Fleet — alerts resolved"
}

func alertLine(a Alert, identities map[string]DeviceIdentity) string {
	name := a.Labels["alertname"]
	if name == "" {
		name = "Alert"
	}
	var b strings.Builder
	b.WriteString("*" + escapeMrkdwn(name) + "*")
	if sev := a.Labels["severity"]; sev != "" {
		b.WriteString(" _(" + escapeMrkdwn(sev) + ")_")
	}
	b.WriteString(deviceSuffix(a, identities))
	if summary := a.Annotations["summary"]; summary != "" {
		b.WriteString("\n" + escapeMrkdwn(summary))
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
	return map[string]any{
		"organization_id": orgID,
		"firing":          convert(firing),
		"resolved":        convert(resolved),
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
