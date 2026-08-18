import { useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import ActiveAlertsPill from "./ActiveAlertsPill";
import type { PagedAlertsFilter, UsePagedAlertsResult } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import AlertInstancesModal from "@/protoFleet/features/alerts/components/AlertInstancesModal";
import type { ActiveAlertGroup, AlertHistoryEntry } from "@/protoFleet/features/alerts/types";

const pagedAlertsMock = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/features/alerts/api/usePagedAlerts", () => ({
  usePagedAlerts: (filter: PagedAlertsFilter) => pagedAlertsMock(filter),
}));

const buildGroup = (overrides: Partial<ActiveAlertGroup> = {}): ActiveAlertGroup => {
  const group = {
    alert_name: "Alert",
    // Only a renamed alert differs from what the server stored, so follow alert_name unless a case says otherwise.
    stored_alert_name: overrides.alert_name ?? "Alert",
    rule_group: "miner",
    device_count: 1,
    alert_count: 1,
    first_started_at: "2026-07-01T00:00:00Z",
    // Only a group with no miners carries one, since only those have no drill-in to describe them.
    summary: "",
    ...overrides,
  };
  // key is derived by the API mapper, not sent by the server, so the fixture derives it too.
  return { ...group, key: JSON.stringify([group.rule_group, group.stored_alert_name]) };
};

const buildInstance = (overrides: Partial<AlertHistoryEntry> = {}): AlertHistoryEntry => ({
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

const buildInstancesResult = (overrides: Partial<UsePagedAlertsResult> = {}): UsePagedAlertsResult => ({
  items: [],
  loading: false,
  error: null,
  hasMore: false,
  loadMore: vi.fn(),
  ...overrides,
});

interface RenderOptions {
  groups: ActiveAlertGroup[];
  error?: string | null;
  hasMore?: boolean;
}

// Mirrors PageHeader's gate (useActiveAlertsPillData.hasVisiblePill): the pill goes once nothing is firing and
// the poll is healthy, and the header owns the drill-in so that dropping the pill cannot close it.
const PillHost = ({ groups, error = null, hasMore = false }: RenderOptions) => {
  const [selectedGroup, setSelectedGroup] = useState<ActiveAlertGroup | null>(null);

  return (
    <>
      {groups.length > 0 || error !== null ? (
        <ActiveAlertsPill groups={groups} error={error} hasMore={hasMore} onSelectGroup={setSelectedGroup} />
      ) : null}
      {selectedGroup ? <AlertInstancesModal group={selectedGroup} onClose={() => setSelectedGroup(null)} /> : null}
    </>
  );
};

const renderPill = (options: RenderOptions) => {
  const view = render(
    <MemoryRouter>
      <PillHost {...options} />
    </MemoryRouter>,
  );

  return {
    ...view,
    rerenderPill: (next: RenderOptions) =>
      view.rerender(
        <MemoryRouter>
          <PillHost {...next} />
        </MemoryRouter>,
      ),
  };
};

const openPopover = async () => {
  await userEvent.click(screen.getByRole("button", { name: "View active alerts" }));
};

describe("ActiveAlertsPill", () => {
  beforeEach(() => {
    // Without this, a `toHaveBeenCalledWith` assertion matches any earlier test's render instead of its own.
    vi.clearAllMocks();
    // Most cases never drill in; the ones that do override this with their own instances.
    pagedAlertsMock.mockReturnValue(buildInstancesResult());
  });

  it("counts the firing alerts on the trigger behind the alert icon", () => {
    renderPill({
      groups: [buildGroup({ alert_name: "Miner Offline" }), buildGroup({ alert_name: "Hashrate dropped" })],
    });

    expect(screen.getByText("2 active alerts")).toBeVisible();
    expect(screen.getByTestId("alert-icon")).toBeVisible();
  });

  it("lists each alert with its affected-miner count", async () => {
    renderPill({
      groups: [
        buildGroup({ alert_name: "Miner Offline", device_count: 5000 }),
        buildGroup({ alert_name: "Hashrate dropped", device_count: 12 }),
      ],
    });
    await openPopover();

    // Popover content asserts on presence: its transition classes leave it "not visible" in jsdom.
    expect(screen.getByText("Miner Offline")).toBeInTheDocument();
    // The miner count is the alert's blast radius, not a per-miner row of its own.
    expect(screen.getByText("5,000 miners affected")).toBeInTheDocument();
    expect(screen.getByText("12 miners affected")).toBeInTheDocument();
    // Each has miners to list, so each says it can be opened for them.
    expect(screen.getAllByTestId("alert-drill-in-chevron")).toHaveLength(2);
  });

  it("lists the affected miners when an alert is clicked", async () => {
    const group = buildGroup({
      alert_name: "Hashrate dropped",
      stored_alert_name: "HashrateDropped",
      device_count: 12,
    });
    pagedAlertsMock.mockReturnValue(buildInstancesResult({ items: [buildInstance()] }));

    renderPill({ groups: [group] });
    await openPopover();
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

  it("keeps the drill-in open when the alert it lists resolves and the pill disappears", async () => {
    const group = buildGroup({ alert_name: "Hashrate dropped", device_count: 12 });
    pagedAlertsMock.mockReturnValue(buildInstancesResult({ items: [buildInstance()] }));

    const { rerenderPill } = renderPill({ groups: [group] });
    await openPopover();
    await userEvent.click(screen.getByText("Hashrate dropped"));

    // The poll clears the last firing alert mid-read, so the header drops the pill the modal was opened from.
    rerenderPill({ groups: [] });

    expect(screen.queryByRole("button", { name: "View active alerts" })).not.toBeInTheDocument();
    expect(screen.getByText("Rig 1")).toBeInTheDocument();
  });

  it("says what a device-less alert reported inline, since it has no per-miner drill-in to open", async () => {
    renderPill({
      groups: [
        buildGroup({
          alert_name: "Metric ingest stalled",
          rule_group: "proto-fleet-self",
          device_count: 0,
          summary: "No telemetry received in 5 minutes",
        }),
      ],
    });
    await openPopover();

    expect(screen.getByText("No telemetry received in 5 minutes")).toBeInTheDocument();
    expect(screen.queryByText("0 miners affected")).not.toBeInTheDocument();
    // Nothing to drill into, so the row is inert and offers no affordance saying otherwise.
    expect(screen.getByText("Metric ingest stalled").closest("button")).toBeNull();
    expect(screen.queryByTestId("alert-drill-in-chevron")).not.toBeInTheDocument();
  });

  it("drills into a lone device-less alert whose summary the server withheld", async () => {
    renderPill({
      groups: [
        buildGroup({
          alert_name: "Curtailment Fan Restore Failed",
          rule_group: "proto-fleet-curtailment",
          device_count: 0,
          // Only the MQTT source templates clear the server's redaction, so this row arrives with nothing to say.
          summary: "",
        }),
      ],
    });
    await openPopover();

    // Title and firing-since alone don't diagnose it, so the instance behind the row stays reachable.
    expect(screen.getByTestId("alert-drill-in-chevron")).toBeInTheDocument();
    expect(screen.getByText("Curtailment Fan Restore Failed").closest("button")).not.toBeNull();
  });

  it("drills into a device-less alert firing on more than one dimension, under a plural title", async () => {
    renderPill({
      groups: [
        buildGroup({
          alert_name: "Curtailment Source Unreachable",
          rule_group: "proto-fleet-curtailment",
          device_count: 0,
          alert_count: 2,
          summary: "maestro-b is unreachable",
        }),
      ],
    });
    await openPopover();

    // One title names one source, and two are down: the row counts them and the drill-in names the rest.
    expect(screen.getByText("Curtailment Sources Unreachable")).toBeInTheDocument();
    expect(screen.getByText("2 instances")).toBeInTheDocument();
    expect(screen.getByTestId("alert-drill-in-chevron")).toBeInTheDocument();
  });

  it("lists the firing instances of a device-less alert without miner columns", async () => {
    const group = buildGroup({
      alert_name: "Curtailment Source Unreachable",
      rule_group: "proto-fleet-curtailment",
      device_count: 0,
      alert_count: 2,
      summary: "maestro-b is unreachable",
    });
    pagedAlertsMock.mockReturnValue(
      buildInstancesResult({
        items: [
          buildInstance({
            id: "src-a",
            device_id: "",
            device_name: "",
            device_mac: "",
            summary: "maestro-a is unreachable",
          }),
          buildInstance({
            id: "src-b",
            device_id: "",
            device_name: "",
            device_mac: "",
            summary: "maestro-b is unreachable",
          }),
        ],
      }),
    );

    renderPill({ groups: [group] });
    await openPopover();
    await userEvent.click(screen.getByText("Curtailment Sources Unreachable"));

    // The drill-in is the only place the sources other than the newest are named.
    expect(screen.getByText("maestro-a is unreachable")).toBeInTheDocument();
    // No device identity to show, so those columns stay out rather than rendering a column of blanks.
    expect(screen.queryByText("Device Name")).not.toBeInTheDocument();
    expect(screen.queryByText("MAC Address")).not.toBeInTheDocument();
  });

  it("surfaces a failed refresh alongside the alerts it could not update", async () => {
    renderPill({ groups: [buildGroup({ alert_name: "Hashrate dropped" })], error: "Failed to load active alerts" });

    // The count is the last good read, so the icon is what says on the closed pill that it stopped being current.
    expect(screen.getByText("1 active alert")).toBeVisible();
    expect(screen.getByTestId("alert-icon")).toHaveClass("text-intent-critical-fill");

    await openPopover();
    expect(screen.getByText("Failed to load active alerts")).toBeInTheDocument();
    expect(screen.getByText("Hashrate dropped")).toBeInTheDocument();
  });

  it("reports a poll that failed before it ever read an alert, rather than a fleet with none", async () => {
    renderPill({ groups: [], error: "Failed to load active alerts" });

    // Nothing else reports this now that the dashboard card is gone, so "0 active alerts" would read as all-clear.
    expect(screen.getByText("Alerts unavailable")).toBeVisible();
    expect(screen.getByTestId("alert-icon")).toHaveClass("text-intent-critical-fill");

    await openPopover();
    expect(screen.getByText("Failed to load active alerts")).toBeInTheDocument();
  });

  it("says the list is truncated when the server has more firing alerts than it returned", async () => {
    renderPill({ groups: [buildGroup({ alert_name: "Hashrate dropped" })], hasMore: true });
    await openPopover();

    expect(screen.getByText(/additional firing alerts are not shown/)).toBeInTheDocument();
  });
});
