import type { ReactNode } from "react";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import LineChart from "./LineChart";

type AnimationFrameHandler = (timestamp: number) => void;

type TooltipInteractionState = {
  activeTooltipIndex?: number | string | null;
  isTooltipActive?: boolean;
};

const frameCallbacks: Array<AnimationFrameHandler | undefined> = [];
let requestAnimationFrameMock: ReturnType<typeof vi.fn>;
let cancelAnimationFrameMock: ReturnType<typeof vi.fn>;
let latestOnMouseMove: ((state: TooltipInteractionState) => void) | undefined;
let latestOnMouseLeave: (() => void) | undefined;
let latestOnTouchMove: ((state: TooltipInteractionState) => void) | undefined;
let latestOnTouchEnd: (() => void) | undefined;

const chartData = [
  { datetime: 1_700_000_000_000, total: 10 },
  { datetime: 1_700_000_300_000, total: 12 },
  { datetime: 1_700_000_600_000, total: 14 },
];

const flushAnimationFrame = () => {
  const pendingCallbacks = [...frameCallbacks];
  frameCallbacks.length = 0;
  pendingCallbacks.forEach((callback) => callback?.(0));
};

vi.mock("recharts", () => ({
  CartesianGrid: () => <div data-testid="cartesian-grid" />,
  Line: ({ dataKey }: { dataKey: string }) => <div data-testid={`line-${dataKey}`} />,
  LineChart: ({
    children,
    onMouseLeave,
    onMouseMove,
    onTouchEnd,
    onTouchMove,
  }: {
    children: ReactNode;
    onMouseLeave?: () => void;
    onMouseMove?: (state: TooltipInteractionState) => void;
    onTouchEnd?: () => void;
    onTouchMove?: (state: TooltipInteractionState) => void;
  }) => {
    latestOnMouseMove = onMouseMove;
    latestOnMouseLeave = onMouseLeave;
    latestOnTouchMove = onTouchMove;
    latestOnTouchEnd = onTouchEnd;

    return <div data-testid="recharts-line-chart">{children}</div>;
  },
  Rectangle: () => <div data-testid="line-cursor-rectangle" />,
  ReferenceLine: () => <div data-testid="reference-line" />,
  Tooltip: ({
    content,
    filterNull,
    offset,
    position,
    wrapperStyle,
  }: {
    content: ReactNode;
    filterNull?: boolean;
    offset?: number;
    position?: unknown;
    wrapperStyle?: { pointerEvents?: string };
  }) => (
    <div data-testid="tooltip" data-filter-null={filterNull === undefined ? "" : String(filterNull)}>
      <span data-testid="tooltip-pointer-events">{wrapperStyle?.pointerEvents ?? ""}</span>
      <span data-testid="tooltip-offset">{String(offset ?? "")}</span>
      <span data-testid="tooltip-position">{position ? "set" : "none"}</span>
      {content}
    </div>
  ),
  XAxis: ({ tick }: { tick: ReactNode }) => <div data-testid="x-axis">{tick}</div>,
  YAxis: () => <div data-testid="y-axis" />,
}));

vi.mock("@/shared/components/Chart", () => ({
  ChartWrapper: ({ children }: { children: ReactNode }) => <div data-testid="chart-wrapper">{children}</div>,
  LineCursor: () => <div data-testid="line-cursor" />,
  TimeXAxisTick: ({ tooltipDatetime, tooltipTickValue }: { tooltipDatetime?: number; tooltipTickValue?: number }) => (
    <div
      data-testid="time-x-axis-tick"
      data-tooltip-datetime={tooltipDatetime === undefined ? "" : String(tooltipDatetime)}
      data-tooltip-tick-value={tooltipTickValue === undefined ? "" : String(tooltipTickValue)}
    />
  ),
  xAxisProps: {},
}));

vi.mock("@/shared/hooks/useCssVariable", () => ({
  default: () => "#123456",
}));

vi.mock("@/shared/hooks/useMeasure", () => ({
  default: () => [
    () => {},
    { x: 0, y: 0, width: 320, height: 240, top: 0, left: 0, bottom: 240, right: 320 },
    { x: 0, y: 0, width: 320, height: 240, top: 0, left: 0, bottom: 240, right: 320 },
  ],
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({
    width: 1440,
    height: 900,
    isDesktop: true,
    isLaptop: false,
    isTablet: false,
    isPhone: false,
  }),
}));

vi.mock("./Tooltip", () => ({
  default: ({
    chartWidth,
    tooltipWidth,
    tooltipXOffset,
  }: {
    chartWidth?: number;
    tooltipWidth?: number;
    tooltipXOffset?: number;
  }) => (
    <div
      data-testid="chart-tooltip"
      data-chart-width={chartWidth === undefined ? "" : String(chartWidth)}
      data-tooltip-width={tooltipWidth === undefined ? "" : String(tooltipWidth)}
      data-tooltip-x-offset={tooltipXOffset === undefined ? "" : String(tooltipXOffset)}
    />
  ),
}));

describe("LineChart", () => {
  beforeEach(() => {
    latestOnMouseMove = undefined;
    latestOnMouseLeave = undefined;
    latestOnTouchMove = undefined;
    latestOnTouchEnd = undefined;
    frameCallbacks.length = 0;

    requestAnimationFrameMock = vi.fn((callback: AnimationFrameHandler) => {
      frameCallbacks.push(callback);
      return frameCallbacks.length;
    });
    cancelAnimationFrameMock = vi.fn((id: number) => {
      frameCallbacks[id - 1] = undefined;
    });

    vi.stubGlobal("requestAnimationFrame", requestAnimationFrameMock);
    vi.stubGlobal("cancelAnimationFrame", cancelAnimationFrameMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("disables tooltip pointer events and lets recharts drive tooltip positioning", () => {
    render(<LineChart chartData={chartData} aggregateKey="total" activeKeys={["total"]} />);

    expect(screen.getByTestId("tooltip-pointer-events")).toHaveTextContent("none");
    expect(screen.getByTestId("tooltip-offset")).toHaveTextContent("0");
    expect(screen.getByTestId("tooltip-position")).toHaveTextContent("none");
    expect(screen.getByTestId("chart-tooltip")).toHaveAttribute("data-chart-width", "320");
  });

  it("batches hovered timestamp updates to one animation frame and clears on mouse leave", () => {
    render(<LineChart chartData={chartData} aggregateKey="total" activeKeys={["total"]} />);

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute("data-tooltip-datetime", "");

    act(() => {
      latestOnMouseMove?.({ activeTooltipIndex: 0, isTooltipActive: true });
      latestOnMouseMove?.({ activeTooltipIndex: 1, isTooltipActive: true });
    });

    expect(requestAnimationFrameMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute("data-tooltip-datetime", "");

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute(
      "data-tooltip-datetime",
      String(chartData[1].datetime),
    );

    act(() => {
      latestOnMouseMove?.({ activeTooltipIndex: 1, isTooltipActive: true });
    });

    expect(requestAnimationFrameMock).toHaveBeenCalledTimes(1);

    act(() => {
      latestOnMouseLeave?.();
    });

    expect(requestAnimationFrameMock).toHaveBeenCalledTimes(2);

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute("data-tooltip-datetime", "");
  });

  it("updates hovered timestamp on touch move and clears it on touch end", () => {
    render(<LineChart chartData={chartData} aggregateKey="total" activeKeys={["total"]} />);

    act(() => {
      latestOnTouchMove?.({ activeTooltipIndex: 1, isTooltipActive: true });
    });

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute(
      "data-tooltip-datetime",
      String(chartData[1].datetime),
    );

    act(() => {
      latestOnTouchEnd?.();
    });

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute("data-tooltip-datetime", "");
  });

  it("does not set hovered timestamp for null-only tooltip buckets", () => {
    const sparseChartData = [
      { datetime: 1_700_000_000_000, total: 10, seriesA: 4 },
      { datetime: 1_700_000_300_000, total: null, seriesA: null },
      { datetime: 1_700_000_600_000, total: 14, seriesA: 8 },
    ];

    render(<LineChart chartData={sparseChartData} aggregateKey="total" activeKeys={["total", "seriesA"]} />);

    act(() => {
      latestOnMouseMove?.({ activeTooltipIndex: 1, isTooltipActive: true });
    });

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute("data-tooltip-datetime", "");
  });

  it("sets hovered timestamp for null-only tooltip buckets when connectNulls is true", () => {
    const sparseChartData = [
      { datetime: 1_700_000_000_000, total: 10, seriesA: 4 },
      { datetime: 1_700_000_300_000, total: null, seriesA: null },
      { datetime: 1_700_000_600_000, total: 14, seriesA: 8 },
    ];

    render(
      <LineChart
        chartData={sparseChartData}
        aggregateKey="total"
        activeKeys={["total", "seriesA"]}
        connectNulls={true}
      />,
    );

    act(() => {
      latestOnMouseMove?.({ activeTooltipIndex: 1, isTooltipActive: true });
    });

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute(
      "data-tooltip-datetime",
      String(sparseChartData[1].datetime),
    );
  });

  it("does not set hovered timestamp for leading null buckets outside displayable range when connectNulls is true", () => {
    const paddedChartData = [
      { datetime: 1_700_000_000_000, total: null, seriesA: null },
      { datetime: 1_700_000_300_000, total: null, seriesA: null },
      { datetime: 1_700_000_600_000, total: 10, seriesA: 4 },
      { datetime: 1_700_000_900_000, total: 14, seriesA: 8 },
    ];

    render(
      <LineChart
        chartData={paddedChartData}
        aggregateKey="total"
        activeKeys={["total", "seriesA"]}
        connectNulls={true}
      />,
    );

    act(() => {
      latestOnMouseMove?.({ activeTooltipIndex: 0, isTooltipActive: true });
    });

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute("data-tooltip-datetime", "");
  });

  it("disables filterNull on Recharts Tooltip when connectNulls is true", () => {
    render(<LineChart chartData={chartData} aggregateKey="total" activeKeys={["total"]} connectNulls={true} />);

    expect(screen.getByTestId("tooltip")).toHaveAttribute("data-filter-null", "false");
  });

  it("preserves default filterNull when connectNulls is false", () => {
    render(<LineChart chartData={chartData} aggregateKey="total" activeKeys={["total"]} />);

    expect(screen.getByTestId("tooltip")).toHaveAttribute("data-filter-null", "");
  });

  it("keeps hovered timestamp for partially populated tooltip buckets", () => {
    const partiallyPopulatedChartData = [
      { datetime: 1_700_000_000_000, total: 10, seriesA: null, seriesB: 6 },
      { datetime: 1_700_000_300_000, total: 12, seriesA: 7, seriesB: 8 },
    ];

    render(
      <LineChart chartData={partiallyPopulatedChartData} aggregateKey="total" activeKeys={["seriesA", "seriesB"]} />,
    );

    act(() => {
      latestOnMouseMove?.({ activeTooltipIndex: 0, isTooltipActive: true });
    });

    act(() => {
      flushAnimationFrame();
    });

    expect(screen.getByTestId("time-x-axis-tick")).toHaveAttribute(
      "data-tooltip-datetime",
      String(partiallyPopulatedChartData[0].datetime),
    );
  });
});
