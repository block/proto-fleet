package alerts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleAlerts mirrors a real Grafana batch: two firing device alerts + one resolved, each
// carrying the internal labels the old native message leaked.
func sampleAlerts() []Alert {
	labels := func(name, sev, dev, template string) map[string]string {
		return map[string]string{
			"alertname": name, "severity": sev, "device_id": dev,
			"organization_id": "1", "grafana_folder": "Proto Fleet",
			"proto_fleet_scope": "shared", "rule_group": "proto-fleet-defaults", "template": template,
		}
	}
	return []Alert{
		{Status: "firing", Labels: labels("Device Hashrate Low", "warning", "dev-a", "hashrate"),
			Annotations: map[string]string{"summary": "Device hashrate is below 75% of expected for at least ten minutes."}},
		{Status: "firing", Labels: labels("Device Temperature High", "warning", "dev-b", "temperature"),
			Annotations: map[string]string{"summary": "Max sensor temperature for device is above 90°C for at least ten minutes."}},
		{Status: "resolved", Labels: labels("Device Offline", "warning", "dev-a", "offline"),
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

func TestRenderSlackLinksTheInstanceHeaderAndUsesPerAlertCopy(t *testing.T) {
	ids := map[string]DeviceIdentity{
		"dev-a": {Name: "miner-01", MAC: "aa:bb:cc:dd:ee:ff"},
		"dev-b": {Name: "miner-02", MAC: "11:22:33:44:55:66"},
	}
	msg := renderSlack("https://fleet.example.com", sampleAlerts(), ids)

	// The fallback preview includes the same copy without link markup.
	assert.Equal(t, strings.Join([]string{
		"Proto Fleet (fleet.example.com)",
		"🟡 1 device hashing below 75% of expected for at least ten minutes",
		"🟡 1 device with sensor temperatures above 90°C for at least ten minutes",
		"✅ 1 device reachable again",
	}, "\n"), msg["text"])

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, blocks)
	// The neutral instance header is itself the link; severity appears only on each alert line.
	assert.Equal(t,
		"*Proto Fleet (<https://fleet.example.com|fleet.example.com>)*",
		sectionText(t, blocks[0]))

	body := mustJSON(t, msg)
	assert.NotContains(t, body, "firing")
	assert.NotContains(t, body, "_(warning)_")
	assert.NotContains(t, body, "miner-01")
	assert.NotContains(t, body, "miner-02")
	assert.NotContains(t, body, "dev-a")
	assert.NotContains(t, body, "dev-b")
}

func TestRenderSlackUsesNaturalDeviceCopyWithThresholdContext(t *testing.T) {
	tests := []struct {
		name     string
		template RuleTemplate
		summary  string
		count    int
		want     string
	}{
		{
			name:     "offline",
			template: RuleTemplateOffline,
			summary:  "Device is offline for at least five minutes.",
			count:    12,
			want:     "🔴 12 devices unreachable for at least five minutes",
		},
		{
			name:     "hashrate percent",
			template: RuleTemplateHashrate,
			summary:  "Device hashrate is below 75% of expected for at least ten minutes.",
			count:    84,
			want:     "🟡 84 devices hashing below 75% of expected for at least ten minutes",
		},
		{
			name:     "temperature",
			template: RuleTemplateTemperature,
			summary:  "Max sensor temperature for device is above 90°C for at least ten minutes.",
			count:    8,
			want:     "🟡 8 devices with sensor temperatures above 90°C for at least ten minutes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			severity := "warning"
			if tc.template == RuleTemplateOffline {
				severity = "critical"
			}
			assert.Equal(t, tc.want, groupLine(alertGroup{
				Severity: severity, Template: string(tc.template), DeviceCount: tc.count,
				Summaries: []string{tc.summary}, SummaryCount: 1,
			}, false))
		})
	}
}

func TestRenderSlackUsesAlertSpecificResolvedCopyInMixedBatch(t *testing.T) {
	firing := Alert{
		Status: "firing",
		Labels: map[string]string{
			"alertname": "Device Hashrate Low", "severity": "warning", "device_id": "dev-a", "template": "hashrate",
		},
		Annotations: map[string]string{"summary": "Device hashrate is below 75% of expected for at least ten minutes."},
	}
	resolved := Alert{
		Status: "resolved",
		Labels: map[string]string{
			"alertname": "Device Offline", "severity": "warning", "device_id": "dev-b", "template": "offline",
		},
	}

	assert.Equal(t, strings.Join([]string{
		"Proto Fleet (fleet.example.com)",
		"🟡 1 device hashing below 75% of expected for at least ten minutes",
		"✅ 1 device reachable again",
	}, "\n"), renderSlack("https://fleet.example.com", []Alert{firing, resolved}, nil)["text"])
}

func TestRenderSlackCountsPartialRecoveryWhileSameRuleStillFires(t *testing.T) {
	alert := func(status, deviceID string) Alert {
		return Alert{
			Status:  status,
			RuleUID: "offline-rule",
			Labels: map[string]string{
				"alertname": "Device Offline", "severity": "critical", "device_id": deviceID,
				"rule_group": "proto-fleet-defaults", "template": "offline",
			},
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."},
		}
	}
	alerts := make([]Alert, 0, 100)
	for i := range 95 {
		alerts = append(alerts, alert("firing", fmt.Sprintf("firing-%d", i)))
	}
	for i := range 5 {
		alerts = append(alerts, alert("resolved", fmt.Sprintf("resolved-%d", i)))
	}

	text := allSectionText(t, renderSlack("", alerts, nil))
	assert.Contains(t, text, "🔴 95 devices unreachable for at least five minutes")
	assert.Contains(t, text, "✅ 5 devices reachable again")
	assert.NotContains(t, text, "All devices reachable")
}

func TestRenderSlackUsesThresholdNeutralHashrateRecoveryCopy(t *testing.T) {
	assert.Equal(t, "✅ 3 devices hashing above alert threshold again", groupLine(alertGroup{
		Template: string(RuleTemplateHashrate), DeviceCount: 3,
	}, true))
}

func TestRenderSlackRecoveryStaysScopedAcrossRules(t *testing.T) {
	alert := func(status, ruleUID, name, deviceID string) Alert {
		return Alert{
			Status: status, RuleUID: ruleUID,
			Labels: map[string]string{
				"alertname": name, "severity": "critical", "device_id": deviceID,
				"rule_group": "proto-fleet-user-7", "template": "offline",
			},
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."},
		}
	}
	alerts := []Alert{
		alert("firing", "offline-site-a", "Site A offline", "dev-a"),
		alert("resolved", "offline-site-b", "Site B offline", "dev-b"),
	}

	text := allSectionText(t, renderSlack("", alerts, nil))
	assert.Contains(t, text, "🔴 Site A offline: 1 device unreachable for at least five minutes")
	assert.Contains(t, text, "✅ Site B offline: 1 device reachable again")
	assert.NotContains(t, text, "All devices reachable")
}

func TestRenderSlackKeepsSameNamedUserRulesSeparateByRuleUID(t *testing.T) {
	alerts := []Alert{
		{
			Status: "firing", RuleUID: "user-offline",
			Labels: map[string]string{
				"alertname": "Watch miners", "severity": "critical", "device_id": "dev-a",
				"rule_group": "proto-fleet-user-7", "template": "offline",
			},
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."},
		},
		{
			Status: "firing", RuleUID: "user-temperature",
			Labels: map[string]string{
				"alertname": "Watch miners", "severity": "warning", "device_id": "dev-b",
				"rule_group": "proto-fleet-user-7", "template": "temperature",
			},
			Annotations: map[string]string{"summary": "Max sensor temperature for device is above 95°C for at least ten minutes."},
		},
	}

	msg := renderSlack("", alerts, nil)
	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, blocks, 3, "the shared name and rule group must not merge distinct rule UIDs")
	text := allSectionText(t, msg)
	assert.Contains(t, text, "Watch miners: 1 device unreachable for at least five minutes")
	assert.Contains(t, text, "Watch miners: 1 device with sensor temperatures above 95°C for at least ten minutes")
}

func TestRenderSlackOmitsLinkWhenNoPublicURL(t *testing.T) {
	msg := renderSlack("", sampleAlerts(), nil)
	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	// With nothing to link to, the header is the plain product name rather than an empty link.
	assert.Equal(t, "*Proto Fleet*", sectionText(t, blocks[0]))
}

func TestRenderSlackDotMatchesEachAlertSeverity(t *testing.T) {
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
		{"critical alone is red", []Alert{alert("A", "critical")}, "🔴"},
		{"case and padding are still the same severity", []Alert{alert("A", " Warning ")}, "🟡"},
		// A rule can carry any severity label; a colour is a weak reason to signal one as less than it is.
		{"an unrecognized severity is not downgraded", []Alert{alert("A", "page-me"), alert("B", "info")}, "🔴"},
		{"a missing severity is not downgraded", []Alert{alert("A", "")}, "🔴"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blocks, ok := renderSlack("", tc.alerts, nil)["blocks"].([]map[string]any)
			require.True(t, ok)
			require.GreaterOrEqual(t, len(blocks), 2)
			assert.True(t, strings.HasPrefix(sectionText(t, blocks[1]), tc.want),
				"want %s dot, got %q", tc.want, sectionText(t, blocks[1]))
		})
	}
}

func TestRenderSlackResolutionOnlyBatchKeepsConditionSpecificCopy(t *testing.T) {
	resolvedOnly := []Alert{{
		Status: "resolved", RuleUID: "protofleet-ha-readiness",
		Labels: map[string]string{
			"alertname": "HA Failover Readiness Degraded", "template": "ha-readiness",
		},
	}}
	msg := renderSlack("https://fleet.example.com", resolvedOnly, nil)
	assert.Equal(t, "Proto Fleet (fleet.example.com)\n✅ HA ready to fail over", msg["text"])
	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 2)
	assert.Equal(t, "✅ HA ready to fail over", sectionText(t, blocks[1]))
	assert.NotContains(t, allSectionText(t, msg), "All alerts resolved")
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
			want:      "Proto Fleet (fleet.rockdale.example.com)",
		},
		{
			name:      "port kept, since two local instances differ only by it",
			publicURL: "http://localhost:8080",
			want:      "Proto Fleet (localhost:8080)",
		},
		{
			name:      "no host to name, so the title is left as it was",
			publicURL: "fleet.navarro.example.com",
			want:      "Proto Fleet",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, ok := renderSlack(tc.publicURL, sampleAlerts(), nil)["text"].(string)
			require.True(t, ok)
			assert.Equal(t, tc.want, strings.Split(text, "\n")[0])
		})
	}
}

func TestRenderSlackTruncatesALongInstanceName(t *testing.T) {
	msg := renderSlack("https://"+strings.Repeat("a", 200)+".example.com", sampleAlerts(), nil)

	want := "Proto Fleet (" + strings.Repeat("a", slackInstanceMaxRunes) + ")"
	text, ok := msg["text"].(string)
	require.True(t, ok)
	assert.Equal(t, want, strings.Split(text, "\n")[0])
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
				"device_id": fmt.Sprintf("dev-%02d", i), "rule_group": "proto-fleet-defaults", "template": "offline",
			},
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."},
		})
	}
	return out
}

func TestRenderSlackRollsUpOneAlertAcrossManyMiners(t *testing.T) {
	msg := renderSlack("https://fleet.example.com", outageAlerts(500), nil)

	assert.Equal(t, "Proto Fleet (fleet.example.com)\n🔴 500 devices unreachable for at least five minutes", msg["text"])

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	// title + one rolled-up section: the miner count no longer drives block count.
	assert.Len(t, blocks, 2)

	text := allSectionText(t, msg)
	assert.Contains(t, text, "🔴 500 devices unreachable for at least five minutes")
	assert.NotContains(t, text, "dev-00")
	assert.NotContains(t, text, "critical")
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
	// The rule interpolates the source into summary, so each affected source remains a separate alert line.
	assert.Contains(t, text, "🔴 Curtailment source maestro-a is unreachable; cannot curtail")
	assert.Contains(t, text, "🔴 Curtailment source maestro-b is unreachable; cannot curtail")
	assert.NotContains(t, text, "_(critical)_")
}

func TestRenderSlackCountsRepeatedDevicelessSummaries(t *testing.T) {
	alert := func(eventID string) Alert {
		return Alert{
			Status: "firing", RuleUID: "protofleet-curtailment-fan-restore-fail",
			Labels: map[string]string{
				"alertname": "Curtailment Fan Restore Failed", "severity": "critical",
				"template": "curtailment-fan-restore", "kind": eventID,
			},
			Annotations: map[string]string{"summary": "Facility fan restore failed before miners resumed."},
		}
	}
	text := allSectionText(t, renderSlack("", []Alert{alert("event-a"), alert("event-b")}, nil))

	assert.Contains(t, text, "🔴 Facility fan restore failed before miners resumed (2 curtailment events affected)")
}

func TestRenderSlackResolvedCurtailmentSourcesNameEveryRecoveredCondition(t *testing.T) {
	source := func(kind string) Alert {
		return Alert{
			Status: "resolved", RuleUID: "protofleet-mqtt-source-disconnected",
			Labels: map[string]string{
				"alertname": "Curtailment Source Unreachable", "severity": "critical", "template": "mqtt-disconnected",
			},
			Annotations: map[string]string{"summary": "Curtailment source " + kind + " is unreachable; cannot curtail."},
		}
	}
	text := allSectionText(t, renderSlack("", []Alert{
		source("maestro-a"), source("maestro-b"), source("maestro-c"), source("maestro-d"), source("maestro-e"),
	}, nil))

	assert.Contains(t, text, "✅ Curtailment source maestro-a reachable again")
	assert.Contains(t, text, "✅ Curtailment source maestro-b reachable again")
	assert.Contains(t, text, "✅ Curtailment source maestro-c reachable again")
	assert.Contains(t, text, "✅ …and 2 more curtailment sources reachable again")
	assert.NotContains(t, text, "maestro-d")
	assert.NotContains(t, text, "maestro-e")
	assert.NotContains(t, text, "All alerts resolved")
	assert.NotContains(t, text, "unreachable")
}

func TestRenderSlackResolvedFleetNodeNamesRecoveredNode(t *testing.T) {
	alert := Alert{
		Status: "resolved", RuleUID: "protofleet-fleet-node-unavailable",
		Labels: map[string]string{
			"alertname": "Fleet Node Unavailable", "severity": "critical", "template": "fleet-node-unavailable",
		},
		Annotations: map[string]string{"summary": "Fleet Node node-a is unavailable."},
	}

	text := allSectionText(t, renderSlack("", []Alert{alert}, nil))
	assert.Contains(t, text, "✅ Connection restored to Fleet Node node-a")
	assert.NotContains(t, text, "is unavailable")
}

func TestRenderSlackResolvedFleetNodesNameSamplesAndCountOverflow(t *testing.T) {
	node := func(name string) Alert {
		return Alert{
			Status: "resolved", RuleUID: "protofleet-fleet-node-unavailable",
			Labels: map[string]string{
				"alertname": "Fleet Node Unavailable", "severity": "critical", "template": "fleet-node-unavailable",
			},
			Annotations: map[string]string{"summary": "Fleet Node " + name + " is unavailable."},
		}
	}

	text := allSectionText(t, renderSlack("", []Alert{
		node("node-a"), node("node-b"), node("node-c"), node("node-d"), node("node-e"),
	}, nil))
	assert.Contains(t, text, "✅ Connection restored to Fleet Node node-a")
	assert.Contains(t, text, "✅ Connection restored to Fleet Node node-b")
	assert.Contains(t, text, "✅ Connection restored to Fleet Node node-c")
	assert.Contains(t, text, "✅ …and 2 more Fleet Node connections restored")
	assert.NotContains(t, text, "node-d")
	assert.NotContains(t, text, "node-e")
	assert.NotContains(t, text, "is unavailable")
}

func TestRenderSlackResolvedFleetNodesFallsBackForMalformedSummary(t *testing.T) {
	text := groupLine(alertGroup{
		Name: "Fleet Node Unavailable", Template: string(RuleTemplateFleetNodeUnavailable), InstanceCount: 2,
		Summaries: []string{"node-a stopped checking in"}, SummaryCount: 1,
	}, true)

	assert.Equal(t, "✅ Connections restored to 2 Fleet Nodes", text)
}

func TestRenderSlackUsesConditionSpecificCurtailmentRecoveryCopy(t *testing.T) {
	assert.Equal(t, "✅ Curtailment by maestro-a ended", groupLine(alertGroup{
		Name: "Curtailment Active", Template: string(RuleTemplateMQTTCurtailment), InstanceCount: 1,
		Summaries: []string{"Miners are curtailed by maestro-a"}, SummaryCount: 1,
	}, true))
	assert.Equal(t, "✅ Fan control restored for 2 curtailment events", groupLine(alertGroup{
		Name: "Curtailment Fan Restore Failed", Template: string(RuleTemplateCurtailmentFanRestore), InstanceCount: 2,
	}, true))
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
	assert.Contains(t, text, "Curtailment source c is unreachable; cannot curtail")
	assert.NotContains(t, text, "Curtailment source d is unreachable")
	assert.Contains(t, text, "…and 2 more")
}

// Every instance of a rule that renders summary from the rule carries the same text, so it stays one line.
func TestRenderSlackKeepsOneSummaryForARuleTextGroup(t *testing.T) {
	text := allSectionText(t, renderSlack("", outageAlerts(3), nil))
	assert.Equal(t, 1, strings.Count(text, "3 devices unreachable for at least five minutes"))
	assert.NotContains(t, text, "…and")
}

func TestRenderSlackCountsEachDeviceOnceWithinAnAlert(t *testing.T) {
	alert := func() Alert {
		return Alert{
			Status: "firing",
			Labels: map[string]string{"alertname": "Device Offline", "severity": "warning", "device_id": "dev-a"},
		}
	}
	msg := renderSlack("", []Alert{alert(), alert()}, nil)
	assert.Contains(t, msg["text"], "🟡 Device Offline affecting 1 device")
	assert.NotContains(t, msg["text"], "2 devices")
}

func TestRenderSlackOrdersGroupsByBlastRadius(t *testing.T) {
	alerts := append(outageAlerts(5), Alert{
		Status: "firing",
		Labels: map[string]string{"alertname": "Device Temperature High", "severity": "warning", "device_id": "dev-x"},
	})
	text := allSectionText(t, renderSlack("", alerts, nil))
	assert.Less(t, strings.Index(text, "5 devices unreachable"), strings.Index(text, "Device Temperature High"))
}

func TestRenderSlackKeepsSummaryForFleetWideAlert(t *testing.T) {
	// No device_id: the summary is the entire alert, so it must survive the rollup.
	alerts := []Alert{{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "Metric Ingest Stalled", "severity": "critical"},
		Annotations: map[string]string{"summary": "No telemetry received in 5 minutes."},
	}}
	msg := renderSlack("", alerts, nil)
	assert.Equal(t, "Proto Fleet\n🔴 No telemetry received in 5 minutes", msg["text"])
	assert.Contains(t, allSectionText(t, msg), "No telemetry received in 5 minutes")
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

func TestRenderWebhookKeepsSameNamedRulesAttributableByRuleUID(t *testing.T) {
	alerts := []Alert{
		{
			Status: "firing", RuleUID: "user-offline",
			Labels: map[string]string{
				"alertname": "Watch miners", "severity": "warning", "device_id": "dev-a",
				"rule_group": "proto-fleet-user-7", "template": "offline",
			},
			Annotations: map[string]string{"summary": "Device is offline for at least five minutes."},
		},
		{
			Status: "firing", RuleUID: "user-temperature",
			Labels: map[string]string{
				"alertname": "Watch miners", "severity": "warning", "device_id": "dev-b",
				"rule_group": "proto-fleet-user-7", "template": "temperature",
			},
			Annotations: map[string]string{"summary": "Max sensor temperature for device is above 95°C for at least ten minutes."},
		},
	}

	out := renderWebhook(7, alerts, nil)
	groups, ok := out["firing_groups"].([]webhookAlertGroup)
	require.True(t, ok)
	require.Len(t, groups, 2)
	assert.ElementsMatch(t, []string{"user-offline", "user-temperature"}, []string{groups[0].RuleUID, groups[1].RuleUID})

	firing, ok := out["firing"].([]webhookAlert)
	require.True(t, ok)
	require.Len(t, firing, 2)
	assert.ElementsMatch(t, []string{"user-offline", "user-temperature"}, []string{firing[0].RuleUID, firing[1].RuleUID})
}

func TestRenderSlackEscapesUserControlledText(t *testing.T) {
	alerts := []Alert{
		{
			Status: "firing",
			Labels: map[string]string{"alertname": "A & B", "severity": "warning", "device_id": "dev-a"},
		},
		{
			Status:      "firing",
			Labels:      map[string]string{"alertname": "Summary", "severity": "warning"},
			Annotations: map[string]string{"summary": "x < y > z"},
		},
	}
	text := allSectionText(t, renderSlack("", alerts, nil))
	// Reserved chars are escaped so alert names and summaries can't inject mrkdwn links.
	assert.Contains(t, text, "A &amp; B")
	assert.Contains(t, text, "x &lt; y &gt; z")
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

func TestRenderSlackCapsFallbackTextIndependently(t *testing.T) {
	alerts := []Alert{{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "Long summary", "severity": "warning"},
		Annotations: map[string]string{"summary": strings.Repeat("温", slackFallbackMaxRunes+100)},
	}}
	msg := renderSlack("", alerts, nil)

	fallback, ok := msg["text"].(string)
	require.True(t, ok)
	assert.Equal(t, slackFallbackMaxRunes, utf8.RuneCountInString(fallback))

	blocks, ok := msg["blocks"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 2)
	assert.Equal(t, slackSectionMaxRunes, utf8.RuneCountInString(sectionText(t, blocks[1])))
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
