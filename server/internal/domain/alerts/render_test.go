package alerts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleAlerts mirrors a real Grafana batch: two firing device alerts + one resolved, each
// carrying the internal labels the old native message leaked.
func sampleAlerts() []Alert {
	labels := func(name, sev, dev string) map[string]string {
		return map[string]string{
			"alertname": name, "severity": sev, "device_id": dev,
			"organization_id": "1", "grafana_folder": "Proto Fleet",
			"proto_fleet_scope": "shared", "rule_group": "proto-fleet-defaults", "template": "x",
		}
	}
	return []Alert{
		{Status: "firing", Labels: labels("Device Hashrate Low", "warning", "dev-a"),
			Annotations: map[string]string{"summary": "Device hashrate has fallen below expected."}},
		{Status: "firing", Labels: labels("Device Temperature High", "warning", "dev-b"),
			Annotations: map[string]string{"summary": "Max sensor temperature is above 90C."}},
		{Status: "resolved", Labels: labels("Device Offline", "warning", "dev-a"),
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."}},
	}
}

func renderSlackJSON(t *testing.T, publicURL string, alerts []Alert, ids map[string]DeviceIdentity) string {
	t.Helper()
	b, err := json.Marshal(renderSlack(publicURL, alerts, ids))
	require.NoError(t, err)
	return string(b)
}

func TestRenderSlackHidesAlertingInternals(t *testing.T) {
	body := renderSlackJSON(t, "https://fleet.example.com", sampleAlerts(), nil)
	for _, leak := range []string{
		"grafana", "Grafana", "Source", "Silence", "__alert_rule_uid__",
		"proto_fleet_scope", "rule_group", "grafana_folder", "localhost",
	} {
		assert.NotContainsf(t, body, leak, "rendered Slack message must not expose %q", leak)
	}
}

func TestRenderSlackTitleLinksTheInstanceName(t *testing.T) {
	ids := map[string]DeviceIdentity{
		"dev-a": {Name: "miner-01", MAC: "aa:bb:cc:dd:ee:ff"},
		"dev-b": {Name: "miner-02", MAC: "11:22:33:44:55:66"},
	}
	msg := renderSlack("https://fleet.example.com", sampleAlerts(), ids)

	// The fallback preview is the same title without link markup, which a blockless client would show raw.
	assert.Equal(t, "🟡 Proto Fleet (fleet.example.com) — 2 alerts firing on 2 miners", msg["text"])

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, blocks)
	// The instance name is itself the link, so the message carries no separate "open the app" line.
	assert.Equal(t,
		"*🟡 Proto Fleet (<https://fleet.example.com|fleet.example.com>) — 2 alerts firing on 2 miners*",
		sectionText(t, blocks[0]))

	body := mustJSON(t, msg)
	// Firing alerts need no heading — the title counts them — and resolved rows carry their status in the name.
	assert.NotContains(t, body, "*Firing*")
	assert.NotContains(t, body, "*Resolved*")
	assert.Contains(t, body, "*Device Temperature High* _(warning)_ — miner-02 (`11:22:33:44:55:66`)")
	assert.Contains(t, body, "Max sensor temperature is above 90C.")
	resolvedText := sectionText(t, blocks[len(blocks)-1])
	assert.Equal(t, "*Resolved: Device Offline* — miner-01 (`aa:bb:cc:dd:ee:ff`)", resolvedText)
	assert.NotContains(t, resolvedText, "warning")
	assert.NotContains(t, resolvedText, "Device is offline for at least five minutes.")
}

func TestRenderSlackOmitsLinkWhenNoPublicURL(t *testing.T) {
	msg := renderSlack("", sampleAlerts(), nil)
	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	// With nothing to link to, the title is the plain product name rather than an empty link.
	assert.Equal(t, "*🟡 Proto Fleet — 2 alerts firing on 2 miners*", sectionText(t, blocks[0]))
}

func TestRenderSlackDotMatchesTheWorstFiringSeverity(t *testing.T) {
	alert := func(name, severity string) Alert {
		return Alert{Status: "firing", Labels: map[string]string{"alertname": name, "severity": severity}}
	}
	tests := []struct {
		name   string
		alerts []Alert
		want   string
	}{
		{"info alone is blue", []Alert{alert("A", "info")}, "🔵"},
		{"warning alone is yellow", []Alert{alert("A", "warning")}, "🟡"},
		{"one critical reddens a batch of warnings", []Alert{alert("A", "warning"), alert("B", "critical")}, "🔴"},
		{"case and padding are still the same severity", []Alert{alert("A", " Warning ")}, "🟡"},
		// A rule can carry any severity label; a colour is a weak reason to signal one as less than it is.
		{"an unrecognized severity is not downgraded", []Alert{alert("A", "page-me"), alert("B", "info")}, "🔴"},
		{"a missing severity is not downgraded", []Alert{alert("A", "")}, "🔴"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, ok := renderSlack("", tc.alerts, nil)["text"].(string)
			require.True(t, ok)
			assert.True(t, strings.HasPrefix(title, tc.want), "want %s dot, got %q", tc.want, title)
		})
	}
}

func TestRenderSlackFallsBackToDeviceID(t *testing.T) {
	body := renderSlackJSON(t, "", sampleAlerts(), nil)
	assert.Contains(t, body, "— dev-b", "with no identity, the raw device id is shown")
}

func TestRenderSlackRendersMACsAsCode(t *testing.T) {
	ids := map[string]DeviceIdentity{
		// ":ab:" is a Slack shortcode (🆎) and "ab" is a valid octet, so a bare MAC interpolates mid-address.
		"dev-a": {Name: "miner-01", MAC: "12:ab:34:cd:56:ef"},
		// A backtick in any field of the section would close a MAC's span early, name and address alike.
		"dev-b": {Name: "miner`02", MAC: "aa:`:bb"},
	}
	text := allSectionText(t, renderSlack("", sampleAlerts(), ids))

	assert.Contains(t, text, "miner-01 (`12:ab:34:cd:56:ef`)")
	assert.Contains(t, text, "miner02 (`aa::bb`)")
}

func TestRenderSlackAllResolvedTitle(t *testing.T) {
	resolvedOnly := []Alert{{Status: "resolved", Labels: map[string]string{"alertname": "Device Offline"}}}
	assert.Equal(t, "✅ Proto Fleet — alerts resolved", renderSlack("", resolvedOnly, nil)["text"])
	// A quiet channel shared by several fleets still has to say whose alerts cleared.
	msg := renderSlack("https://fleet.example.com", resolvedOnly, nil)
	assert.Equal(t, "✅ Proto Fleet (fleet.example.com) — alerts resolved", msg["text"])
}

func TestRenderSlackNamesTheInstanceInTheTitle(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
		want      string
	}{
		{
			name:      "host only, so the scheme and path stay out of the title",
			publicURL: "https://fleet.rockdale.example.com/miners?tab=all",
			want:      "🟡 Proto Fleet (fleet.rockdale.example.com) — 2 alerts firing on 2 miners",
		},
		{
			name:      "port kept, since two local instances differ only by it",
			publicURL: "http://localhost:8080",
			want:      "🟡 Proto Fleet (localhost:8080) — 2 alerts firing on 2 miners",
		},
		{
			name:      "no host to name, so the title is left as it was",
			publicURL: "fleet.navarro.example.com",
			want:      "🟡 Proto Fleet — 2 alerts firing on 2 miners",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, renderSlack(tc.publicURL, sampleAlerts(), nil)["text"])
		})
	}
}

func TestRenderSlackTruncatesALongInstanceName(t *testing.T) {
	msg := renderSlack("https://"+strings.Repeat("a", 200)+".example.com", sampleAlerts(), nil)

	want := "🟡 Proto Fleet (" + strings.Repeat("a", slackInstanceMaxRunes) + ") — 2 alerts firing on 2 miners"
	assert.Equal(t, want, msg["text"], "the counts survive a hostname that would fill the title")
}

func TestRenderWebhookResolvesDeviceMetadata(t *testing.T) {
	ids := map[string]DeviceIdentity{"dev-b": {Name: "miner-02", MAC: "11:22:33:44:55:66"}}
	out := renderWebhook(42, sampleAlerts(), ids)

	assert.Equal(t, int64(42), out["organization_id"])
	firing, ok := out["firing"].([]webhookAlert)
	require.True(t, ok)
	require.Len(t, firing, 2)
	var temp webhookAlert
	for _, a := range firing {
		if a.AlertName == "Device Temperature High" {
			temp = a
		}
	}
	assert.Equal(t, "miner-02", temp.DeviceName)
	assert.Equal(t, "11:22:33:44:55:66", temp.DeviceMAC)
	assert.Equal(t, "warning", temp.Severity)

	resolved, ok := out["resolved"].([]webhookAlert)
	require.True(t, ok)
	require.Len(t, resolved, 1)
	assert.Equal(t, "Device Offline", resolved[0].AlertName)
}

// outageAlerts is one rule firing across many miners: the shape that used to render one Slack line per miner. Its
// summary is the rule-level sentence, naming the threshold rather than the miner, so it must survive the rollup.
func outageAlerts(count int) []Alert {
	var out []Alert
	for i := range count {
		out = append(out, Alert{
			Status: "firing",
			Labels: map[string]string{
				"alertname": "Device Offline", "severity": "critical",
				"device_id": fmt.Sprintf("dev-%02d", i), "rule_group": "proto-fleet-defaults",
			},
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."},
		})
	}
	return out
}

func TestRenderSlackRollsUpOneAlertAcrossManyMiners(t *testing.T) {
	msg := renderSlack("https://fleet.example.com", outageAlerts(500), nil)

	assert.Equal(t, "🔴 Proto Fleet (fleet.example.com) — 1 alert firing on 500 miners", msg["text"])

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	// title + one rolled-up section: the miner count no longer drives block count.
	assert.Len(t, blocks, 2)

	text := allSectionText(t, msg)
	assert.Contains(t, text, "*Device Offline* _(critical)_ — 500 miners")
	assert.Contains(t, text, "dev-00, dev-01, dev-02 and 497 more")
	// The rule's threshold text is the only thing that says what "offline" means; the rollup must keep it.
	assert.Contains(t, text, "Device is offline for at least five minutes.")
}

func TestRenderSlackCountsInstancesForDevicelessAlerts(t *testing.T) {
	source := func(kind string) Alert {
		return Alert{
			Status:      "firing",
			Labels:      map[string]string{"alertname": "Curtailment Source Unreachable", "severity": "critical"},
			Annotations: map[string]string{"summary": "Curtailment source " + kind + " is unreachable; cannot curtail."},
		}
	}
	text := allSectionText(t, renderSlack("", []Alert{source("maestro-a"), source("maestro-b")}, nil))
	assert.Contains(t, text, "*Curtailment Source Unreachable* _(critical)_ — 2 instances")
	// The rule interpolates the source into summary, so naming only one would leave the other unreported.
	assert.Contains(t, text, "Curtailment source maestro-a is unreachable; cannot curtail.")
	assert.Contains(t, text, "Curtailment source maestro-b is unreachable; cannot curtail.")
}

func TestRenderSlackResolvedDevicelessAlertsKeepIdentifyingSummaries(t *testing.T) {
	source := func(kind string) Alert {
		return Alert{
			Status:      "resolved",
			Labels:      map[string]string{"alertname": "Curtailment Source Unreachable", "severity": "critical"},
			Annotations: map[string]string{"summary": "Curtailment source " + kind + " is unreachable; cannot curtail."},
		}
	}
	text := allSectionText(t, renderSlack("", []Alert{source("maestro-a"), source("maestro-b")}, nil))

	assert.Contains(t, text, "*Resolved: Curtailment Source Unreachable* — 2 instances")
	assert.NotContains(t, text, "_(critical)_", "resolved alerts omit severity")
	assert.Contains(t, text, "Curtailment source maestro-a is unreachable; cannot curtail.")
	assert.Contains(t, text, "Curtailment source maestro-b is unreachable; cannot curtail.")
}

func TestRenderSlackTailsSummariesPastTheSampleCap(t *testing.T) {
	source := func(kind string) Alert {
		return Alert{
			Status:      "firing",
			Labels:      map[string]string{"alertname": "Curtailment Source Unreachable", "severity": "critical"},
			Annotations: map[string]string{"summary": "Curtailment source " + kind + " is unreachable; cannot curtail."},
		}
	}
	alerts := []Alert{source("a"), source("b"), source("c"), source("d"), source("e")}
	text := allSectionText(t, renderSlack("", alerts, nil))
	// Past the cap the section says how many it left out rather than growing without bound.
	assert.Contains(t, text, "Curtailment source c is unreachable; cannot curtail.")
	assert.NotContains(t, text, "Curtailment source d is unreachable")
	assert.Contains(t, text, "…and 2 more")
}

// Every instance of a rule that renders summary from the rule carries the same text, so it stays one line.
func TestRenderSlackKeepsOneSummaryForARuleTextGroup(t *testing.T) {
	text := allSectionText(t, renderSlack("", outageAlerts(3), nil))
	assert.Equal(t, 1, strings.Count(text, "Device is offline for at least five minutes."))
	assert.NotContains(t, text, "…and")
}

func TestRenderSlackCountsEachMinerOnceAcrossAlerts(t *testing.T) {
	both := func(name string) Alert {
		return Alert{
			Status: "firing",
			Labels: map[string]string{"alertname": name, "severity": "warning", "device_id": "dev-a"},
		}
	}
	msg := renderSlack("", []Alert{both("Device Offline"), both("Device Hashrate Low")}, nil)
	// One miner with two alerts is one affected miner, not two.
	assert.Equal(t, "🟡 Proto Fleet — 2 alerts firing on 1 miner", msg["text"])
}

func TestRenderSlackOrdersGroupsByBlastRadius(t *testing.T) {
	alerts := append(outageAlerts(5), Alert{
		Status: "firing",
		Labels: map[string]string{"alertname": "Device Temperature High", "severity": "warning", "device_id": "dev-x"},
	})
	text := allSectionText(t, renderSlack("", alerts, nil))
	assert.Less(t, strings.Index(text, "Device Offline"), strings.Index(text, "Device Temperature High"))
}

func TestRenderSlackKeepsSummaryForFleetWideAlert(t *testing.T) {
	// No device_id: the summary is the entire alert, so it must survive the rollup.
	alerts := []Alert{{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "Metric Ingest Stalled", "severity": "critical"},
		Annotations: map[string]string{"summary": "No telemetry received in 5 minutes."},
	}}
	msg := renderSlack("", alerts, nil)
	assert.Equal(t, "🔴 Proto Fleet — 1 alert firing", msg["text"], "no miners to count")
	assert.Contains(t, allSectionText(t, msg), "No telemetry received in 5 minutes.")
}

func TestRenderWebhookIncludesGroupRollup(t *testing.T) {
	out := renderWebhook(42, append(outageAlerts(500), sampleAlerts()...), nil)

	groups, ok := out["firing_groups"].([]webhookAlertGroup)
	require.True(t, ok)
	require.Len(t, groups, 3)
	assert.Equal(t, "Device Offline", groups[0].AlertName)
	assert.Equal(t, 500, groups[0].DeviceCount)
	assert.Equal(t, 500, groups[0].AlertCount)
	assert.Equal(t, "Device is offline for at least five minutes.", groups[0].Summary, "the rule's threshold text survives the rollup")
	// The per-miner detail is still there for consumers that want it.
	firing, ok := out["firing"].([]webhookAlert)
	require.True(t, ok)
	assert.Len(t, firing, 502)

	resolvedGroups, ok := out["resolved_groups"].([]webhookAlertGroup)
	require.True(t, ok)
	require.Len(t, resolvedGroups, 1)
	assert.Equal(t, "Device Offline", resolvedGroups[0].AlertName)
	assert.Equal(t, 1, resolvedGroups[0].DeviceCount)
	assert.Equal(t, "Device is offline for at least five minutes.", resolvedGroups[0].Summary)
}

func TestRenderSlackEscapesUserControlledText(t *testing.T) {
	ids := map[string]DeviceIdentity{"dev-a": {Name: "<https://evil.example|click>", MAC: "m"}}
	alerts := []Alert{{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "A & B", "severity": "warning", "device_id": "dev-a"},
		Annotations: map[string]string{"summary": "x < y > z"},
	}}
	text := allSectionText(t, renderSlack("", alerts, ids))
	// The reserved chars are escaped, so a device name can't inject a mrkdwn link.
	assert.Contains(t, text, "&lt;https://evil.example|click&gt;")
	assert.Contains(t, text, "A &amp; B")
	assert.Contains(t, text, "x &lt; y &gt; z")
	assert.NotContains(t, text, "<https://evil.example|click>")
}

func TestRenderSlackCapsBlocksForLargeBatch(t *testing.T) {
	var alerts []Alert
	for i := range 60 {
		alerts = append(alerts, Alert{Status: "firing", Labels: map[string]string{"alertname": fmt.Sprintf("Alert %02d", i)}})
	}
	msg := renderSlack("https://fleet.example.com", alerts, nil)
	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(blocks), 50, "must stay under Slack's 50-block-per-message limit")
	assert.Contains(t, mustJSON(t, msg), "more — open Proto Fleet")
}

func allSectionText(t *testing.T, msg map[string]any) string {
	t.Helper()
	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	var out string
	for _, b := range blocks {
		if text, ok := b["text"].(map[string]any); ok {
			if s, ok := text["text"].(string); ok {
				out += s + "\n"
			}
		}
	}
	return out
}

func sectionText(t *testing.T, block map[string]any) string {
	t.Helper()
	text, ok := block["text"].(map[string]any)
	require.True(t, ok, "block has no text object")
	s, ok := text["text"].(string)
	require.True(t, ok)
	return s
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
