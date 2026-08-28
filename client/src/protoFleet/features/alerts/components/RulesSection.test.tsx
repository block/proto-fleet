import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import RulesSection from "./RulesSection";
import { AlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import type { UseAlertsResult } from "@/protoFleet/features/alerts/api/useAlerts";
import type { Rule } from "@/protoFleet/features/alerts/types";

vi.mock("./AddMaintenanceWindowModal", () => ({ default: () => null }));
vi.mock("./AddRuleModal", () => ({ default: () => null }));
vi.mock("./EditDeliveryModal", () => ({ default: () => null }));
vi.mock("@/protoFleet/features/alerts/lib/useNow", () => ({ useNow: () => Date.UTC(2026, 7, 27) }));
vi.mock("@/protoFleet/store", () => ({ useHasPermission: () => false }));

const makeRule = (overrides: Partial<Rule>): Rule => ({
  id: "rule-1",
  organization_id: "org-1",
  name: "Device Offline",
  template: "offline",
  group: "proto-fleet-defaults",
  severity: "warning",
  summary: "Device is offline for at least five minutes.",
  description: "",
  duration_seconds: 300,
  enabled: true,
  origin: "provisioned",
  config: null,
  routing: { mode: "default", channel_ids: [] },
  ...overrides,
});

const renderRules = (rules: Rule[]) => {
  const alerts = {
    rules,
    maintenanceWindows: [],
    loading: false,
    pauseRule: vi.fn(),
    resumeRule: vi.fn(),
    removeRule: vi.fn(),
    removeMaintenanceWindow: vi.fn(),
  } as unknown as UseAlertsResult;

  render(
    <AlertsContext.Provider value={alerts}>
      <RulesSection />
    </AlertsContext.Provider>,
  );
};

describe("RulesSection", () => {
  it("shows origin and severity metadata", () => {
    renderRules([
      makeRule({}),
      makeRule({
        id: "rule-2",
        name: "Rack hashrate",
        severity: "critical",
        summary: "Rack hashrate is below 80% of expected.",
        origin: "user",
        config: { name: "Rack hashrate", duration_seconds: 600, hashrate: { mode: "pct_expected", value: 80 } },
      }),
    ]);

    expect(screen.getByText("Default rule")).toBeVisible();
    expect(screen.getByText("Custom rule")).toBeVisible();
    expect(screen.getByText("warning")).toBeVisible();
    expect(screen.getByText("critical")).toBeVisible();
    expect(screen.getByText("Device is offline for at least five minutes.")).toBeVisible();
    expect(screen.getByText("Rack hashrate is below 80% of expected.")).toBeVisible();
  });
});
