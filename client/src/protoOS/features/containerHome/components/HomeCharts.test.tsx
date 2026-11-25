import { cloneElement, type ReactElement } from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { HomeHistory } from "../types";
import { HomeHistoryChart } from "./HomeCharts";

// Give the real Recharts plots a size without relying on jsdom layout measurements.
vi.mock("recharts", async (importOriginal) => ({
  ...(await importOriginal<typeof import("recharts")>()),
  ResponsiveContainer: ({ children }: { children: ReactElement<{ width: number; height: number }> }) =>
    cloneElement(children, { width: 600, height: 300 }),
}));

function historyWith(values: (number | null)[], stacked = false): HomeHistory {
  return {
    units: "kW",
    window: [0, Math.max(1, values.length - 1)],
    stacked,
    series: [
      {
        id: "power",
        label: "Power",
        color: "#ff6600",
        latest: "10 kW",
        points: values.map((value, datetime) => ({ datetime, value })),
      },
    ],
  };
}

describe.each([false, true])("Home history isolated samples (stacked=%s)", (stacked) => {
  it.each([
    { name: "a sample between gaps", values: [null, 10, null], markers: 1 },
    { name: "a zero between gaps", values: [null, 0, null], markers: 1 },
    { name: "a single sample", values: [10], markers: 1 },
    { name: "isolated samples at both edges", values: [10, null, 20], markers: 2 },
    { name: "an uninterrupted line", values: [10, 20, 30], markers: 0 },
    { name: "an isolated sample after a segment", values: [10, 20, null, 30], markers: 1 },
  ])("renders markers only for $name", ({ values, markers }) => {
    const { container } = render(<HomeHistoryChart history={historyWith(values, stacked)} />);
    const dots = container.querySelectorAll(".recharts-line-dots circle, .recharts-area-dots circle");
    expect(dots).toHaveLength(markers);
    for (const dot of dots) {
      expect(dot).toHaveAttribute("r", "3");
      expect(dot).toHaveAttribute("fill", "#ff6600");
      expect(Number.isFinite(Number(dot.getAttribute("cx")))).toBe(true);
      expect(Number.isFinite(Number(dot.getAttribute("cy")))).toBe(true);
    }
  });

  it("keeps the unavailable state when every sample is missing", () => {
    render(<HomeHistoryChart history={historyWith([null, null, null], stacked)} />);
    expect(screen.getByRole("status")).toHaveTextContent("History unavailable");
  });
});

it("preserves line gaps around an isolated sample", () => {
  const { container } = render(<HomeHistoryChart history={historyWith([10, 20, null, 30, null, 40, 50])} />);
  const path = container.querySelector(".recharts-line-curve");
  expect(path).toBeInTheDocument();
  expect(path?.getAttribute("d")?.match(/M/g)).toHaveLength(3);
  expect(container.querySelectorAll(".recharts-line-dots circle")).toHaveLength(1);
});

it("shows a marker when clipping leaves only one visible sample in the compact chart", () => {
  const history = historyWith([10, 20, 30]);
  history.window = [0.5, 1.5];
  const { container } = render(<HomeHistoryChart history={history} compact />);
  expect(container.querySelectorAll(".recharts-line-dots circle")).toHaveLength(1);
});

it("treats a timestamp present only in another series as a gap", () => {
  const history = historyWith([10, null, 20]);
  history.series[0].points.splice(1, 1);
  history.series.push({
    id: "fans",
    label: "Fans",
    color: "#0000ff",
    latest: "5 kW",
    points: [{ datetime: 1, value: 5 }],
  });
  const { container } = render(<HomeHistoryChart history={history} />);
  expect(container.querySelectorAll('.recharts-line-dots circle[fill="#ff6600"]')).toHaveLength(2);
  expect(container.querySelectorAll('.recharts-line-dots circle[fill="#0000ff"]')).toHaveLength(1);
});
