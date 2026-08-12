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

func TestRenderSlackHeaderLinksToInstance(t *testing.T) {
	ids := map[string]DeviceIdentity{
		"dev-a": {Name: "miner-01", MAC: "aa:bb:cc:dd:ee:ff"},
		"dev-b": {Name: "miner-02", MAC: "11:22:33:44:55:66"},
	}
	msg := renderSlack("https://fleet.example.com", sampleAlerts(), ids)

	assert.Equal(t, "🔴 Proto Fleet — 2 alerts firing on 2 miners", msg["text"])

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, blocks)
	assert.Equal(t, "header", blocks[0]["type"])
	// The clickable instance link is a mrkdwn section (Block Kit headers can't hold links).
	assert.Equal(t, "<https://fleet.example.com|Open Proto Fleet>", sectionText(t, blocks[1]))

	body := mustJSON(t, msg)
	assert.Contains(t, body, "*Firing*")
	assert.Contains(t, body, "*Resolved*")
	assert.Contains(t, body, "*Device Temperature High* _(warning)_ — miner-02 (11:22:33:44:55:66)")
	assert.Contains(t, body, "Max sensor temperature is above 90C.")
	assert.Contains(t, body, "*Device Offline* _(warning)_ — miner-01 (aa:bb:cc:dd:ee:ff)")
}

func TestRenderSlackOmitsLinkWhenNoPublicURL(t *testing.T) {
	msg := renderSlack("", sampleAlerts(), nil)
	body := mustJSON(t, msg)
	assert.NotContains(t, body, "Open Proto Fleet")
	// Header is still present.
	assert.Contains(t, body, "Proto Fleet — 2 alerts firing")
}

func TestRenderSlackFallsBackToDeviceID(t *testing.T) {
	body := renderSlackJSON(t, "", sampleAlerts(), nil)
	assert.Contains(t, body, "— dev-b", "with no identity, the raw device id is shown")
}

func TestRenderSlackAllResolvedTitle(t *testing.T) {
	resolvedOnly := []Alert{{Status: "resolved", Labels: map[string]string{"alertname": "Device Offline"}}}
	assert.Equal(t, "✅ Proto Fleet — alerts resolved", renderSlack("", resolvedOnly, nil)["text"])
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

	assert.Equal(t, "🔴 Proto Fleet — 1 alert firing on 500 miners", msg["text"])

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	// header + link + "Firing" heading + one rolled-up section: the miner count no longer drives block count.
	assert.Len(t, blocks, 4)

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
	// Two sources roll into one group, so the count is the only signal that more than one is down.
	assert.Contains(t, text, "*Curtailment Source Unreachable* _(critical)_ — 2 instances")
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
	assert.Equal(t, "🔴 Proto Fleet — 2 alerts firing on 1 miner", msg["text"])
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
