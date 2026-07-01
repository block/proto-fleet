package alerts

import (
	"encoding/json"
	"fmt"
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

	assert.Equal(t, "🔴 Proto Fleet — 2 alerts firing", msg["text"])

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
