import { cloneElement, type ReactElement } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import KpiLineChart from "./KpiLineChart";
import { useChartDataForMetric, useMinerStore } from "@/protoOS/store";
import { type MetricTimeSeries, ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";

// Keep the real Recharts renderer; jsdom only needs a fixed layout size.
vi.mock("@/shared/components/Chart", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/components/Chart")>()),
  ChartWrapper: ({ children }: { children: ReactElement<{ width: number; height: number }> }) =>
    cloneElement(children, { width: 800, height: 400 }),
}));

const Chart = () => <KpiLineChart {...useChartDataForMetric("hashrate")} />;

const seedHistories = (miner: MetricTimeSeries, board: MetricTimeSeries) => {
  useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
  const telemetry = createEmptyTelemetryState();
  telemetry.rollupsByEntity.set(ROOT_MODULE_KEY, { hashrate: { timeSeries: miner } });
  const hashboardId = getHashboardId("HB-1");
  telemetry.hashboards.set(hashboardId, { hashboardId, hashrate: { timeSeries: board } });
  useMinerStore.getState().telemetry.mergeTelemetry(telemetry);
};

const history = (timestamps: number[], values = timestamps.map((_, index) => 100 + index * 10)) => ({
  points: timestamps.map((timestampMs, index) => ({ timestampMs, value: values[index] })),
  bucketSizeMs: 60_000,
});

const curvePaths = (container: HTMLElement) =>
  [...container.querySelectorAll(".recharts-line-curve")].map((curve) => curve.getAttribute("d") ?? "");

describe("KPI history rendering", () => {
  beforeEach(() => {
    useMinerStore.setState(useMinerStore.getInitialState(), true);
  });

  it.each([60_000, undefined])("draws offset histories continuously with bucket size %s", async (bucketSizeMs) => {
    seedHistories(
      { ...history([0, 60_000, 120_000]), bucketSizeMs },
      { ...history([1_000, 61_000, 121_000], [40, 45, 50]), bucketSizeMs },
    );

    const { container } = render(<Chart />);

    await waitFor(() => expect(curvePaths(container)).toHaveLength(2));
    for (const path of curvePaths(container)) {
      expect(path.match(/M/g)).toHaveLength(1);
      expect(path).toMatch(/[LC]/);
    }
  });

  it("keeps shared tooltips aligned to the hovered timestamp with independent lines", async () => {
    seedHistories(history([0, 60_000, 120_000]), history([1_000, 61_000, 121_000], [40, 45, 50]));
    const { container } = render(<Chart />);
    await waitFor(() => expect(curvePaths(container)).toHaveLength(2));

    const chart = screen.getByRole("application");
    fireEvent.focus(chart);
    fireEvent.keyDown(chart, { key: "ArrowRight" });

    await waitFor(() => {
      const tooltip = container.querySelector(".recharts-tooltip-wrapper");
      expect(tooltip).toHaveTextContent("40");
      expect(tooltip).not.toHaveTextContent("Summary");
      expect(tooltip).not.toHaveTextContent("100");
    });
  });

  it("does not infer gaps without bucket metadata or for small sampling jitter", async () => {
    seedHistories(
      { ...history([0, 60_000, 180_000, 240_000]), bucketSizeMs: undefined },
      history([1_000, 62_000, 121_000, 182_000, 241_000]),
    );
    const { container } = render(<Chart />);
    await waitFor(() => expect(curvePaths(container)).toHaveLength(2));
    for (const path of curvePaths(container)) {
      expect(path.match(/M/g)).toHaveLength(1);
      expect(path).toMatch(/[LC]/);
    }

    fireEvent.click(screen.getByTestId("chart-filter-summary"));
    await waitFor(() => expect(curvePaths(container)).toHaveLength(1));
    expect(curvePaths(container)[0].match(/M/g)).toHaveLength(1);
  });

  it("redraws safely when a refresh replaces history with fewer samples", async () => {
    seedHistories(history([0, 60_000, 120_000]), history([1_000, 61_000, 121_000]));
    const { container } = render(<Chart />);
    await waitFor(() => expect(curvePaths(container)).toHaveLength(2));

    act(() => seedHistories(history([180_000, 240_000]), history([181_000, 241_000])));

    await waitFor(() => expect(curvePaths(container)).toHaveLength(2));
    for (const path of curvePaths(container)) {
      expect(path.match(/M/g)).toHaveLength(1);
      expect(path).toMatch(/[LC]/);
    }
  });

  it.each(["explicit null", "skipped bucket"])("preserves an actual %s gap in one series", async (gap) => {
    const miner = history([0, 60_000, 120_000, 180_000, 240_000]);
    const points =
      gap === "explicit null"
        ? miner.points.map((point) => (point.timestampMs === 120_000 ? { ...point, value: null } : point))
        : miner.points.filter((point) => point.timestampMs !== 120_000);
    seedHistories({ ...miner, points }, history([1_000, 61_000, 121_000, 181_000, 241_000]));

    const { container } = render(<Chart />);

    await waitFor(() => expect(curvePaths(container)).toHaveLength(2));
    const [minerPath, boardPath] = curvePaths(container);
    expect(minerPath.match(/M/g)).toHaveLength(2);
    expect(minerPath.match(/[LC]/g)).toHaveLength(2);
    expect(boardPath.match(/M/g)).toHaveLength(1);
    expect(boardPath).toMatch(/[LC]/);
  });
});
