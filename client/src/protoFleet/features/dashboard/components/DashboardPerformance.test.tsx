import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import DashboardPerformance from "./DashboardPerformance";

const panels = vi.hoisted(() => ({ hashrate: vi.fn(), power: vi.fn(), efficiency: vi.fn() }));

vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C" }));
vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({ refs: { vertical: { start: { current: null }, end: { current: null } } } }),
}));
vi.mock("./HashratePanel", () => ({ HashratePanel: panels.hashrate }));
vi.mock("./UptimePanel", () => ({ UptimePanel: () => null }));
vi.mock("./TemperaturePanel", () => ({ TemperaturePanel: () => null }));
vi.mock("./PowerPanel", () => ({ PowerPanel: panels.power }));
vi.mock("./EfficiencyPanel", () => ({ EfficiencyPanel: panels.efficiency }));

describe("dashboard history ranges", () => {
  it.each([false, true])("offers every Fleet range (account readings=%s)", async (accountReadings) => {
    const onDurationChange = vi.fn();
    render(
      <DashboardPerformance
        duration="1h"
        onDurationChange={onDurationChange}
        metrics={[]}
        totalAssets={0}
        accountReadings={accountReadings}
      />,
    );
    const ranges = within(screen.getByRole("group", { name: "Time range" }));
    const durations = ["1h", "24h", "7d", "30d", "90d", "1y"];
    expect(ranges.getAllByRole("button").map((button) => button.textContent)).toEqual(durations);
    for (const duration of durations) {
      await userEvent.click(ranges.getByRole("button", { name: duration }));
      expect(onDurationChange).toHaveBeenLastCalledWith(duration);
    }
  });

  it("keeps account metric gaps and measured equipment power for longer ranges", () => {
    render(
      <DashboardPerformance duration="90d" onDurationChange={vi.fn()} metrics={[]} totalAssets={4} accountReadings />,
    );
    expect(panels.hashrate).toHaveBeenLastCalledWith(
      expect.objectContaining({ duration: "90d", connectNulls: false, padMissingBuckets: false }),
      undefined,
    );
    expect(panels.power).toHaveBeenLastCalledWith(
      expect.objectContaining({
        duration: "90d",
        connectNulls: false,
        padMissingBuckets: false,
        metricsInKW: true,
        reportingSubtitle: "Measured equipment power",
      }),
      undefined,
    );
    expect(panels.efficiency).toHaveBeenLastCalledWith(
      expect.objectContaining({
        duration: "90d",
        connectNulls: false,
        padMissingBuckets: false,
        reportingSubtitle: "Complete readings only",
      }),
      undefined,
    );
  });

  it("preserves longer aggregate-backed ranges for the host dashboard", () => {
    render(<DashboardPerformance duration="30d" onDurationChange={vi.fn()} metrics={[]} totalAssets={0} />);
    const ranges = within(screen.getByRole("group", { name: "Time range" }));
    expect(ranges.getAllByRole("button").map((button) => button.textContent)).toEqual([
      "1h",
      "24h",
      "7d",
      "30d",
      "90d",
      "1y",
    ]);
    expect(ranges.getByRole("button", { name: "30d" })).toHaveAttribute("aria-pressed", "true");
  });
});
