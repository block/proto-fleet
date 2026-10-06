import { describe, expect, it } from "vitest";

import { isRuleFullyMuted } from "./useAlerts";
import type { MaintenanceWindow, Rule, RuleRouting } from "@/protoFleet/features/alerts/types";

const makeWindow = (overrides: Partial<MaintenanceWindow>): MaintenanceWindow => ({
  id: "w1",
  organization_id: "7",
  rule_ids: [],
  channel_ids: [],
  starts_at: "2026-01-01T00:00:00Z",
  ends_at: "2026-01-02T00:00:00Z",
  comment: "",
  created_by: "",
  created_at: "2026-01-01T00:00:00Z",
  ...overrides,
});

const makeRule = (routing: RuleRouting | null): Rule => ({
  id: "rule-9",
  organization_id: "7",
  name: "Rule 9",
  template: "offline",
  group: "defaults",
  severity: "critical",
  summary: "",
  description: "",
  duration_seconds: 0,
  enabled: true,
  origin: "provisioned",
  config: null,
  routing,
});

describe("isRuleFullyMuted", () => {
  it("marks any rule muted under an every-rule every-channel window", () => {
    expect(isRuleFullyMuted(makeRule(null), [makeWindow({})])).toBe(true);
  });

  it("marks a covered rule muted under an every-channel window, but not other rules", () => {
    const windows = [makeWindow({ rule_ids: ["rule-9"] })];
    expect(isRuleFullyMuted(makeRule(null), windows)).toBe(true);
    expect(isRuleFullyMuted({ ...makeRule(null), id: "other" }, windows)).toBe(false);
  });

  it("leaves a default-routed rule active under a channel-scoped window (it pages elsewhere)", () => {
    expect(isRuleFullyMuted(makeRule({ mode: "default", channel_ids: [] }), [makeWindow({ channel_ids: ["3"] })])).toBe(
      false,
    );
  });

  it("marks a custom-routed rule muted when windows jointly cover all its routed channels", () => {
    const rule = makeRule({ mode: "custom", channel_ids: ["3", "5"] });
    const windows = [
      makeWindow({ id: "w1", channel_ids: ["3"] }),
      makeWindow({ id: "w2", rule_ids: ["rule-9"], channel_ids: ["5"] }),
    ];
    expect(isRuleFullyMuted(rule, windows)).toBe(true);
  });

  it("leaves a custom-routed rule active while any routed channel stays unmuted", () => {
    const rule = makeRule({ mode: "custom", channel_ids: ["3", "5"] });
    expect(isRuleFullyMuted(rule, [makeWindow({ channel_ids: ["3"] })])).toBe(false);
  });

  it("ignores channel-scoped windows targeting other rules", () => {
    const rule = makeRule({ mode: "custom", channel_ids: ["3"] });
    expect(isRuleFullyMuted(rule, [makeWindow({ rule_ids: ["other"], channel_ids: ["3"] })])).toBe(false);
  });

  it("never marks an orphaned custom route (no routed channels) muted by vacuous coverage", () => {
    expect(isRuleFullyMuted(makeRule({ mode: "custom", channel_ids: [] }), [makeWindow({ channel_ids: ["3"] })])).toBe(
      false,
    );
  });
});
