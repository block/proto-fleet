import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import KpiChart from "./KpiLineChart";

// Mock the recharts components
vi.mock("recharts", () => {
  const OriginalModule = vi.importActual("recharts");
  return {
    ...OriginalModule,
    ResponsiveContainer: ({ children }: any) => (
      <div data-testid="responsive-container">{children}</div>
    ),
    Tooltip: ({ content, isAnimationActive }: any) => (
      <div data-testid="tooltip">
        {content}
        <span data-testid="tooltip-animation">{String(isAnimationActive)}</span>
      </div>
    ),
    LineChart: ({ children }: any) => (
      <div data-testid="line-chart">{children}</div>
    ),
    Line: ({ dataKey, activeDot, isAnimationActive }: any) => (
      <div data-testid={`line-${dataKey}`}>
        <span data-testid={`animation-${dataKey}`}>
          {String(isAnimationActive)}
        </span>
        {activeDot && <div data-testid={`dot-${dataKey}`}>{activeDot}</div>}
      </div>
    ),
    CartesianGrid: () => <div data-testid="cartesian-grid" />,
    XAxis: ({ tick }: any) => <div data-testid="x-axis">{tick}</div>,
    YAxis: () => <div data-testid="y-axis" />,
  };
});

// Mock window dimensions hook
vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({
    isDesktop: true,
    isTablet: false,
    isPhone: false,
  }),
}));

// Mock KpiTooltip component
vi.mock("./KpiTooltip", () => ({
  default: ({ tooltipData, units, onHover }: any) => (
    <div data-testid="kpi-tooltip">
      <button
        data-testid="tooltip-hover-button"
        onClick={() =>
          onHover({
            payload: [
              {
                name: "total",
                payload: {
                  datetime: 1234567890,
                  aggregateName: "total",
                  total: 54,
                  series1: 42,
                  series2: 12,
                },
              },
            ],
            x: 100,
            y: 200,
          })
        }
      >
        Simulate Hover
      </button>
      <button
        data-testid="tooltip-unhover-button"
        onClick={() => onHover({ payload: [], x: 0, y: 0 })}
      >
        Simulate Unhover
      </button>
      <div data-testid="tooltip-payload-count">
        {tooltipData.payload.length}
      </div>
      <div data-testid="tooltip-units">{units}</div>
    </div>
  ),
}));

const mockSeries = [
  {
    name: "series1",
    data: [
      { datetime: 1234567890, value: 42 },
      { datetime: 1234567891, value: 43 },
    ],
  },
  {
    name: "series2",
    data: [
      { datetime: 1234567890, value: 12 },
      { datetime: 1234567891, value: 13 },
    ],
  },
];

const mockAggregateSeries = {
  name: "total",
  data: [
    { datetime: 1234567890, value: 54 },
    { datetime: 1234567891, value: 56 },
  ],
};

describe("KpiLineChart", () => {
  it("renders the component with necessary chart elements", () => {
    render(
      <KpiChart
        duration="12h"
        series={mockSeries}
        aggregateSeries={mockAggregateSeries}
        units="W"
      />,
    );

    expect(screen.getByTestId("responsive-container")).toBeInTheDocument();
    expect(screen.getByTestId("line-chart")).toBeInTheDocument();
    expect(screen.getByTestId("cartesian-grid")).toBeInTheDocument();
    expect(screen.getByTestId("x-axis")).toBeInTheDocument();
    expect(screen.getByTestId("y-axis")).toBeInTheDocument();
    expect(screen.getByTestId("tooltip")).toBeInTheDocument();
    expect(screen.getByTestId("kpi-tooltip")).toBeInTheDocument();
    expect(screen.getByTestId("line-total")).toBeInTheDocument();
  });

  it("does not show series lines when tooltip has no data (not hovered)", () => {
    render(
      <KpiChart
        duration="12h"
        series={mockSeries}
        aggregateSeries={mockAggregateSeries}
        units="W"
      />,
    );

    // Initially, the tooltip has no payload
    expect(screen.getByTestId("tooltip-payload-count").textContent).toBe("0");

    // Series lines should not be in the document
    expect(screen.queryByTestId("line-series1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("line-series2")).not.toBeInTheDocument();

    // But aggregate line should be present
    expect(screen.getByTestId("line-total")).toBeInTheDocument();
  });

  it("shows series lines when tooltip has data (chart is hovered)", () => {
    render(
      <KpiChart
        duration="12h"
        series={mockSeries}
        aggregateSeries={mockAggregateSeries}
        units="W"
      />,
    );

    // Initially, the tooltip has no payload
    expect(screen.getByTestId("tooltip-payload-count").textContent).toBe("0");

    // Simulate a hover by calling the onHover function
    fireEvent.click(screen.getByTestId("tooltip-hover-button"));

    // Now the tooltip should have payload data
    expect(screen.getByTestId("tooltip-payload-count").textContent).toBe("1");

    // Series lines should now be visible
    expect(screen.getByTestId("line-series1")).toBeInTheDocument();
    expect(screen.getByTestId("line-series2")).toBeInTheDocument();

    // And the aggregate line should still be present
    expect(screen.getByTestId("line-total")).toBeInTheDocument();

    // The aggregate line should also have an active dot when hovered
    expect(screen.getByTestId("dot-total")).toBeInTheDocument();
  });

  it("hides series lines when unhovered", () => {
    render(
      <KpiChart
        duration="12h"
        series={mockSeries}
        aggregateSeries={mockAggregateSeries}
        units="W"
      />,
    );

    // Simulate a hover
    fireEvent.click(screen.getByTestId("tooltip-hover-button"));

    // Verify series lines are shown
    expect(screen.getByTestId("line-series1")).toBeInTheDocument();
    expect(screen.getByTestId("line-series2")).toBeInTheDocument();

    // Now simulate an unhover
    fireEvent.click(screen.getByTestId("tooltip-unhover-button"));

    // Series lines should be hidden again
    expect(screen.queryByTestId("line-series1")).not.toBeInTheDocument();
    expect(screen.queryByTestId("line-series2")).not.toBeInTheDocument();

    // The tooltip should have no payload
    expect(screen.getByTestId("tooltip-payload-count").textContent).toBe("0");
  });

  it("passes units to the tooltip", () => {
    render(
      <KpiChart
        duration="12h"
        series={mockSeries}
        aggregateSeries={mockAggregateSeries}
        units="W"
      />,
    );

    expect(screen.getByTestId("tooltip-units").textContent).toBe("W");
  });
});
