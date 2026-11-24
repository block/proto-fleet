import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { HomeSeries } from "../types";
import HomeHistoryTooltip from "./HomeHistoryTooltip";

const datetime = new Date("2026-09-15T07:20:00Z").getTime();
const series: HomeSeries[] = ["1–10%", "11–20%", "21–30%"].map((label, index) => ({
  id: `band${index}`,
  label,
  color: "red",
  latest: "—",
  points: [],
}));

describe("Home history tooltip", () => {
  it.each(["°C", "°F", "kW", "J/TH"])("renders %s once per value, in legend order", (units) => {
    render(<HomeHistoryTooltip point={{ datetime, band1: 35.12, band0: 38.14 }} series={series} units={units} />);
    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByText("1–10%")).toBeVisible();
    expect(within(rows[1]).getByText("11–20%")).toBeVisible();
    expect(rows[0]).toHaveTextContent(`38.1 ${units}`);
    expect(rows[1]).toHaveTextContent(`35.1 ${units}`);
    for (const row of rows) expect(row.textContent?.split(units)).toHaveLength(2);
  });

  it("separates the timestamp from readings", () => {
    const { container } = render(<HomeHistoryTooltip point={{ datetime, band0: 38.1 }} series={series} units="°C" />);
    const timestamp = container.querySelector("time");
    expect(timestamp).toHaveAttribute("datetime", "2026-09-15T07:20:00.000Z");
    expect(timestamp).toHaveTextContent("9/15/26 •");
    expect(screen.getByRole("list")).not.toContainElement(timestamp);
  });

  it("preserves zero and negative temperatures while omitting missing or invalid values", () => {
    render(
      <HomeHistoryTooltip point={{ datetime, band0: 0, band1: -2.25, band2: Number.NaN }} series={series} units="°C" />,
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("0.0 °C")).toBeVisible();
    expect(screen.getByText("-2.3 °C")).toBeVisible();
  });

  it.each([undefined, { datetime, band0: null }, { datetime: Number.NaN, band0: 10 }])(
    "hides the tooltip when no valid point is available",
    (point) => {
      const { container } = render(<HomeHistoryTooltip point={point} series={series} units="°C" />);
      expect(container).toBeEmptyDOMElement();
    },
  );
});
