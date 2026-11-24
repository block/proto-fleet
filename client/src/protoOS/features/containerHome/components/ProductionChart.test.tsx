import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { HomeHistory } from "../types";
import ProductionChart from "./ProductionChart";

const chart = vi.hoisted(() => vi.fn((_props: Record<string, unknown>) => null));
vi.mock("@/shared/components/LineChart", () => ({ default: chart }));

describe("ProductionChart", () => {
  it("uses named peer temperature series with blue inlet, red outlet, and no legend", () => {
    const names = [
      ["outsideTemperature", "Ambient outside"],
      ["inletTemperature", "Fluid inlet"],
      ["outletTemperature", "Fluid outlet"],
    ];
    const history: HomeHistory = {
      units: "°C",
      window: [0, 60_000],
      series: names.map(([id, label]) => ({
        id,
        label,
        color: "",
        latest: "0.0 °C",
        points: [{ datetime: 0, value: 0 }],
      })),
    };
    render(<ProductionChart history={history} />);
    expect(chart.mock.calls[chart.mock.calls.length - 1]?.[0]).toMatchObject({
      tooltipAggregateKey: null,
      tooltipSeriesLabels: Object.fromEntries(names),
      segmentsLabel: "",
      colorMap: { inletTemperature: "--color-intent-info-fill", outletTemperature: "--color-intent-critical-fill" },
    });
    expect(screen.queryByRole("list", { name: "Latest series values" })).not.toBeInTheDocument();
  });
});
