import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import DashboardPerformance from "./DashboardPerformance";

vi.mock("@/protoFleet/store", () => ({ useTemperatureUnit: () => "C" }));
vi.mock("@/shared/hooks/useStickyState", () => ({
  useStickyState: () => ({ refs: { vertical: { start: { current: null }, end: { current: null } } } }),
}));
vi.mock("./HashratePanel", () => ({ HashratePanel: () => null }));
vi.mock("./UptimePanel", () => ({ UptimePanel: () => null }));
vi.mock("./TemperaturePanel", () => ({ TemperaturePanel: () => null }));
vi.mock("./ReadingPanel", () => ({ default: () => null }));
vi.mock("./PowerPanel", () => ({ PowerPanel: () => null }));
vi.mock("./EfficiencyPanel", () => ({ EfficiencyPanel: () => null }));

describe("dashboard history ranges", () => {
  it("offers retained raw history ranges for accounts and requests the selected range", async () => {
    const onDurationChange = vi.fn();
    render(
      <DashboardPerformance
        duration="1h"
        onDurationChange={onDurationChange}
        metrics={[]}
        totalAssets={0}
        accountReadings
      />,
    );
    const ranges = within(screen.getByRole("group", { name: "Time range" }));
    expect(ranges.getAllByRole("button").map((button) => button.textContent)).toEqual(["1h", "24h"]);
    await userEvent.click(ranges.getByRole("button", { name: "24h" }));
    expect(onDurationChange).toHaveBeenCalledWith("24h");
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
