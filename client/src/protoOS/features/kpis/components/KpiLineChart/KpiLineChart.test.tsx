import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import KpiLineChartWrapper from "./KpiLineChart";

// Mock SVG elements for tests
vi.mock("@/shared/components/Chart/AxisTick", () => ({
  default: ({ payload }: any) => (
    <div data-testid="axis-tick" className="test-axis-tick">
      <span data-testid="axis-value">{payload.value}</span>
    </div>
  ),
}));

vi.mock("@/shared/components/Chart/TimeXAxisTick", () => ({
  default: ({ payload }: any) => (
    <div data-testid="time-x-axis-tick">
      <span data-testid="time-value">{payload?.value}</span>
    </div>
  ),
}));

// Mock the recharts components
vi.mock("recharts", async () => {
  const OriginalModule = await vi.importActual("recharts");
  return {
    ...OriginalModule,
    ResponsiveContainer: ({ children }: any) => <div data-testid="responsive-container">{children}</div>,
    Tooltip: ({ content, isAnimationActive }: any) => (
      <div data-testid="tooltip">
        {content}
        <span data-testid="tooltip-animation">{String(isAnimationActive)}</span>
      </div>
    ),
    LineChart: ({ children }: any) => <div data-testid="line-chart">{children}</div>,
    Line: ({ dataKey, activeDot, isAnimationActive }: any) => (
      <div data-testid={`line-${dataKey}`}>
        <span data-testid={`animation-${dataKey}`}>{String(isAnimationActive)}</span>
        {activeDot ? <div data-testid={`dot-${dataKey}`}>{activeDot}</div> : null}
      </div>
    ),
    XAxis: ({ tick }: any) => <div data-testid="x-axis">{tick}</div>,
    YAxis: () => <div data-testid="y-axis" />,
    Rectangle: () => <div data-testid="rectangle" />,
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

// Mock Tooltip component
vi.mock("@/shared/components/LineChart/Tooltip", () => ({
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
      <button data-testid="tooltip-unhover-button" onClick={() => onHover({ payload: [], x: 0, y: 0 })}>
        Simulate Unhover
      </button>
      <div data-testid="tooltip-payload-count">{tooltipData.payload.length}</div>
      <div data-testid="tooltip-units">{units}</div>
    </div>
  ),
}));

describe("KpiLineChartWrapper", () => {
  it("renders the component without errors", () => {
    const { container } = render(<KpiLineChartWrapper chartData={[]} chartLines={[]} />);

    expect(container).toBeInTheDocument();
  });

  it("renders hashboard selector", () => {
    render(<KpiLineChartWrapper chartData={[]} chartLines={[]} />);

    // The wrapper should render the HashboardSelector with a Summary button
    expect(screen.getByText("Summary")).toBeInTheDocument();
  });

  it("renders responsive container for chart", () => {
    render(<KpiLineChartWrapper chartData={[]} chartLines={[]} />);

    expect(screen.getByTestId("responsive-container")).toBeInTheDocument();
  });
});
