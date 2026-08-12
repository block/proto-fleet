import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import type { UseActiveAlertGroupsResult } from "@/protoFleet/features/alerts/api/useActiveAlertGroups";
import type { PagedAlertsFilter, UsePagedAlertsResult } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import ActiveAlertsCard from "@/protoFleet/features/alerts/components/ActiveAlertsCard";
import type { ActiveAlertGroup, AlertHistoryEntry } from "@/protoFleet/features/alerts/types";

const activeGroupsMock = vi.hoisted(() => vi.fn());
const pagedAlertsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/features/alerts/api/useActiveAlertGroups", () => ({
  useActiveAlertGroups: () => activeGroupsMock(),
}));

vi.mock("@/protoFleet/features/alerts/api/usePagedAlerts", () => ({
  usePagedAlerts: (filter: PagedAlertsFilter) => pagedAlertsMock(filter),
}));

const buildResult = (overrides: Partial<UseActiveAlertGroupsResult> = {}): UseActiveAlertGroupsResult => ({
  groups: [],
  loading: false,
  error: null,
  denied: false,
  hasMore: false,
  ...overrides,
});

const buildGroup = (overrides: Partial<ActiveAlertGroup> = {}): ActiveAlertGroup => {
  const group = {
    alert_name: "Alert",
    // Only a renamed alert differs from what the server stored, so follow alert_name unless a case says otherwise.
    stored_alert_name: overrides.alert_name ?? "Alert",
    rule_group: "miner",
    severity: "warning",
    device_count: 1,
    alert_count: 1,
    first_started_at: "2026-07-01T00:00:00Z",
    summary: "",
    ...overrides,
  };
  // key is derived by the API mapper, not sent by the server, so the fixture derives it too.
  return { ...group, key: JSON.stringify([group.rule_group, group.stored_alert_name]) };
};

const buildMiner = (overrides: Partial<AlertHistoryEntry> = {}): AlertHistoryEntry => ({
  id: "alert-1",
  received_at: "2026-07-01T00:00:00Z",
  alert_name: "Hashrate dropped",
  status: "firing",
  severity: "warning",
  rule_group: "miner",
  fingerprint: "fp-1",
  device_id: "device-1",
  device_name: "Rig 1",
  device_mac: "AA:BB:CC:DD:EE:FF",
  template: "",
  summary: "Hashrate below expected",
  starts_at: null,
  ends_at: null,
  ...overrides,
});

const buildMinersResult = (overrides: Partial<UsePagedAlertsResult> = {}): UsePagedAlertsResult => ({
  items: [],
  loading: false,
  error: null,
  hasMore: false,
  loadMore: vi.fn(),
  ...overrides,
});

describe("ActiveAlertsCard", () => {
  // Most cases never open the modal; the two that do override this with their own instances.
  beforeEach(() => {
    pagedAlertsMock.mockReturnValue(buildMinersResult());
  });

  it("renders one row per alert with its affected-miner count", () => {
    activeGroupsMock.mockReturnValue(
      buildResult({
        groups: [
          buildGroup({ alert_name: "Miner Offline", severity: "critical", device_count: 5000 }),
          buildGroup({ alert_name: "Hashrate dropped", device_count: 12 }),
        ],
      }),
    );
    render(<ActiveAlertsCard />);

    expect(screen.getByText("Miner Offline")).toBeVisible();
    // The miner count is the row's blast radius, not a per-miner row of its own.
    expect(screen.getByText("5,000 miners")).toBeVisible();
    expect(screen.getByText("Hashrate dropped")).toBeVisible();
    expect(screen.getByText("12 miners")).toBeVisible();
  });

  it("lists the affected miners when an alert is clicked", async () => {
    const group = buildGroup({
      alert_name: "Hashrate dropped",
      stored_alert_name: "HashrateDropped",
      device_count: 12,
    });
    activeGroupsMock.mockReturnValue(buildResult({ groups: [group] }));
    pagedAlertsMock.mockReturnValue(buildMinersResult({ items: [buildMiner()] }));

    render(<ActiveAlertsCard />);
    await userEvent.click(screen.getByText("Hashrate dropped"));

    // The drill-in filters on the stored rule title, not the display one a retired-rule rename rewrote.
    expect(pagedAlertsMock).toHaveBeenCalledWith(
      expect.objectContaining({ active_only: true, alert_name: "HashrateDropped", rule_group: "miner" }),
    );
    // Modal content asserts on presence: the overlay's transition classes leave it "not visible" in jsdom.
    expect(screen.getByText("Rig 1")).toBeInTheDocument();
    expect(screen.getByText("AA:BB:CC:DD:EE:FF")).toBeInTheDocument();
    expect(screen.getByText("12 miners affected")).toBeInTheDocument();
  });

  it("renders a device-less alert as a callout even when its rule group is unknown to the client", () => {
    activeGroupsMock.mockReturnValue(
      buildResult({
        groups: [
          buildGroup({
            alert_name: "Curtailment Fan Restore Failed",
            rule_group: "proto-fleet-curtailment",
            device_count: 0,
            summary: "Facility fan restore failed before miners resumed.",
          }),
        ],
      }),
    );
    render(<ActiveAlertsCard />);

    // No blast radius to rank, so it must not become a rollup row reading "0 miners".
    expect(screen.getAllByTestId("callout")).toHaveLength(1);
    expect(screen.queryByText("Affected Miners")).not.toBeInTheDocument();
    // Its one instance is already described by the callout's subtitle, so there is nothing to drill into.
    expect(screen.queryByText("View instances")).not.toBeInTheDocument();
  });

  it("counts the instances of a device-less alert firing on more than one source", () => {
    activeGroupsMock.mockReturnValue(
      buildResult({
        groups: [
          buildGroup({
            alert_name: "Curtailment Source Unreachable",
            rule_group: "proto-fleet-curtailment",
            device_count: 0,
            alert_count: 2,
            summary: "Curtailment source maestro-b is unreachable.",
          }),
        ],
      }),
    );
    render(<ActiveAlertsCard />);

    // The summary names only the newest source, so the count is what tells the operator another is down.
    const callout = within(screen.getByTestId("callout"));
    expect(callout.getByText("Curtailment Source Unreachable — 2 instances")).toBeVisible();
    expect(callout.getByText("Curtailment source maestro-b is unreachable.")).toBeVisible();
  });

  it("drills into a device-less alert's instances, which its summary alone cannot name", async () => {
    const group = buildGroup({
      alert_name: "Curtailment Source Unreachable",
      rule_group: "proto-fleet-curtailment",
      device_count: 0,
      alert_count: 2,
      summary: "Curtailment source maestro-b is unreachable.",
    });
    activeGroupsMock.mockReturnValue(buildResult({ groups: [group] }));
    pagedAlertsMock.mockReturnValue(
      buildMinersResult({
        items: [
          buildMiner({
            id: "src-a",
            device_id: "",
            device_name: "",
            device_mac: "",
            summary: "maestro-a is unreachable.",
          }),
          buildMiner({
            id: "src-b",
            device_id: "",
            device_name: "",
            device_mac: "",
            summary: "maestro-b is unreachable.",
          }),
        ],
      }),
    );

    render(<ActiveAlertsCard />);
    await userEvent.click(screen.getByText("View instances"));

    expect(screen.getByText("2 instances firing")).toBeInTheDocument();
    expect(screen.getByText("maestro-a is unreachable.")).toBeInTheDocument();
    expect(screen.getByText("maestro-b is unreachable.")).toBeInTheDocument();
    // Device columns would be empty for every row, so the table leaves them out.
    expect(screen.queryByText("MAC Address")).not.toBeInTheDocument();
  });

  it("renders one callout per fleet-wide alert with severity-mapped intent", () => {
    activeGroupsMock.mockReturnValue(
      buildResult({
        groups: [
          buildGroup({
            alert_name: "Metric ingest stalled",
            rule_group: "proto-fleet-self",
            severity: "critical",
            device_count: 0,
            summary: "No telemetry received in 5 minutes",
          }),
          buildGroup({
            alert_name: "Host CPU high",
            rule_group: "proto-fleet-system",
            severity: "warning",
            device_count: 0,
            summary: "CPU usage above 90% for 10 minutes",
          }),
        ],
      }),
    );
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

  it("renders a source-scoped alert as a callout alongside miner alert rows", () => {
    activeGroupsMock.mockReturnValue(
      buildResult({
        groups: [
          buildGroup({
            alert_name: "Curtailment Source Unreachable",
            severity: "critical",
            device_count: 0,
            summary: "Source maestro-a is disconnected",
          }),
          buildGroup({ alert_name: "Hashrate dropped", device_count: 3 }),
        ],
      }),
    );
    render(<ActiveAlertsCard />);

    expect(screen.getAllByTestId("callout")).toHaveLength(1);
    expect(screen.getByText("Curtailment Source Unreachable")).toBeVisible();
    expect(screen.getByText("Hashrate dropped")).toBeVisible();
    expect(screen.getByText("3 miners")).toBeVisible();
  });

  it("renders the empty state when there are no active alerts", () => {
    activeGroupsMock.mockReturnValue(buildResult());
    render(<ActiveAlertsCard />);

    expect(screen.getByText("No active alerts.")).toBeVisible();
    expect(screen.queryByTestId("callout")).not.toBeInTheDocument();
  });

  it("renders nothing when the request is denied", () => {
    activeGroupsMock.mockReturnValue(buildResult({ denied: true }));
    const { container } = render(<ActiveAlertsCard />);

    expect(container).toBeEmptyDOMElement();
  });
});
