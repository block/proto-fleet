import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { UseActiveAlertsResult } from "@/protoFleet/features/alerts/api/useActiveAlerts";
import ActiveAlertsCard from "@/protoFleet/features/alerts/components/ActiveAlertsCard";
import type { AlertHistoryEntry } from "@/protoFleet/features/alerts/types";

const activeAlertsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/features/alerts/api/useActiveAlerts", () => ({
  useActiveAlerts: () => activeAlertsMock(),
}));

const buildResult = (overrides: Partial<UseActiveAlertsResult> = {}): UseActiveAlertsResult => ({
  alerts: [],
  loading: false,
  error: null,
  denied: false,
  hasMore: false,
  ...overrides,
});

const buildAlert = (overrides: Partial<AlertHistoryEntry> = {}): AlertHistoryEntry => ({
  id: "alert-1",
  received_at: "2026-07-01T00:00:00Z",
  alert_name: "Alert",
  status: "firing",
  severity: "warning",
  rule_group: "miner",
  fingerprint: "fp-1",
  device_id: "",
  device_name: "",
  device_mac: "",
  template: "",
  summary: "",
  starts_at: null,
  ends_at: null,
  ...overrides,
});

describe("ActiveAlertsCard", () => {
  it("renders one callout per fleet-wide alert with severity-mapped intent", () => {
    const selfAlert = buildAlert({
      id: "alert-self",
      alert_name: "Metric ingest stalled",
      rule_group: "proto-fleet-self",
      severity: "critical",
      summary: "No telemetry received in 5 minutes",
    });
    const systemAlert = buildAlert({
      id: "alert-system",
      alert_name: "Host CPU high",
      rule_group: "proto-fleet-system",
      severity: "warning",
      summary: "CPU usage above 90% for 10 minutes",
    });
    activeAlertsMock.mockReturnValue(buildResult({ alerts: [selfAlert, systemAlert] }));

    render(<ActiveAlertsCard />);

    const callouts = screen.getAllByTestId("callout");
    expect(callouts).toHaveLength(2);

    const selfCallout = within(callouts[0]);
    expect(selfCallout.getByText("Metric ingest stalled")).toBeVisible();
    expect(selfCallout.getByText("No telemetry received in 5 minutes")).toBeVisible();
    expect(callouts[0].querySelector(".text-intent-critical-fill")).not.toBeNull();

    const systemCallout = within(callouts[1]);
    expect(systemCallout.getByText("Host CPU high")).toBeVisible();
    expect(systemCallout.getByText("CPU usage above 90% for 10 minutes")).toBeVisible();
    expect(callouts[1].querySelector(".text-intent-warning-fill")).not.toBeNull();
  });

  it("renders a fleet-wide callout alongside device alerts without listing it as a device row", () => {
    const fleetWideAlert = buildAlert({
      id: "alert-self",
      alert_name: "Metric ingest stalled",
      rule_group: "proto-fleet-self",
      severity: "critical",
    });
    const deviceAlert = buildAlert({
      id: "alert-device",
      alert_name: "Hashrate dropped",
      rule_group: "miner",
      device_id: "device-1",
      device_name: "Rig 1",
      device_mac: "AA:BB:CC:DD:EE:FF",
    });
    activeAlertsMock.mockReturnValue(buildResult({ alerts: [fleetWideAlert, deviceAlert] }));

    render(<ActiveAlertsCard />);

    expect(screen.getAllByTestId("callout")).toHaveLength(1);
    expect(screen.getByText("Metric ingest stalled")).toBeVisible();
    expect(screen.getByText("Rig 1")).toBeVisible();
    expect(screen.getByText("AA:BB:CC:DD:EE:FF")).toBeVisible();
    expect(screen.getByText("Hashrate dropped")).toBeVisible();
  });

  it("renders the empty state when there are no active alerts", () => {
    activeAlertsMock.mockReturnValue(buildResult());

    render(<ActiveAlertsCard />);

    expect(screen.getByText("No active alerts.")).toBeVisible();
    expect(screen.queryByTestId("callout")).not.toBeInTheDocument();
  });

  it("renders nothing when the request is denied", () => {
    activeAlertsMock.mockReturnValue(buildResult({ denied: true }));

    const { container } = render(<ActiveAlertsCard />);

    expect(container).toBeEmptyDOMElement();
  });
});
