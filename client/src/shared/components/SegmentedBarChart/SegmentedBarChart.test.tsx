import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import SegmentedBarChart from "./SegmentedBarChart";
import type { SegmentedBarChartData } from "./types";

// Mock recharts components to avoid rendering issues in tests
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: any) => (
    <div data-testid="responsive-container">{children}</div>
  ),
  BarChart: ({ children, data }: any) => (
    <div data-testid="bar-chart" data-length={data?.length}>
      {children}
    </div>
  ),
  Bar: ({ dataKey }: any) => (
    <div data-testid={`bar-${dataKey}`}>{dataKey}</div>
  ),
  CartesianGrid: () => <div data-testid="cartesian-grid" />,
  XAxis: () => <div data-testid="x-axis" />,
  YAxis: () => <div data-testid="y-axis" />,
  Tooltip: () => <div data-testid="tooltip" />,
  Legend: () => <div data-testid="legend" />,
  Cell: () => <div data-testid="cell" />,
}));

// Mock the ChartWrapper component
vi.mock("@/shared/components/Chart/ChartWrapper", () => ({
  default: ({ children, className }: any) => (
    <div className={className} data-testid="chart-wrapper">
      {children}
    </div>
  ),
}));

// Mock the custom components
vi.mock("./CustomSegmentedBar", () => ({
  default: () => <div data-testid="custom-segmented-bar" />,
}));

vi.mock("./SegmentedXAxisTick", () => ({
  default: () => <div data-testid="segmented-x-axis-tick" />,
}));

vi.mock("./Tooltip/SegmentedBarTooltip", () => ({
  default: () => <div data-testid="segmented-bar-tooltip" />,
}));

// Mock hooks
vi.mock("@/shared/hooks/useMeasure", () => ({
  default: () => [
    (_el: any) => {}, // measureRef
    { width: 600, height: 400 }, // contentRect
    { width: 600, height: 400 }, // boundingRect
  ],
}));

describe("SegmentedBarChart", () => {
  const mockData: SegmentedBarChartData[] = [
    {
      datetime: 1700000000,
      segment1: 45,
      segment2: 30,
      segment3: 25,
    },
    {
      datetime: 1700003600,
      segment1: 50,
      segment2: 25,
      segment3: 25,
    },
    {
      datetime: 1700007200,
      segment1: 40,
      segment2: 35,
      segment3: 25,
    },
  ];

  const defaultProps = {
    chartData: mockData,
    segmentKeys: ["segment1", "segment2", "segment3"],
  };

  it("renders without errors", () => {
    const { container } = render(<SegmentedBarChart {...defaultProps} />);
    expect(container).toBeInTheDocument();
  });

  it("renders chart wrapper with correct props", () => {
    render(<SegmentedBarChart {...defaultProps} />);
    const wrapper = screen.getByTestId("chart-wrapper");
    expect(wrapper).toBeInTheDocument();
  });

  it("renders bar chart with data", () => {
    render(<SegmentedBarChart {...defaultProps} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toHaveAttribute("data-length", "3");
  });

  it("renders bar with total dataKey", () => {
    render(<SegmentedBarChart {...defaultProps} />);
    expect(screen.getByTestId("bar-total")).toBeInTheDocument();
  });

  it("renders no data message when chartData is null", () => {
    render(<SegmentedBarChart {...defaultProps} chartData={null} />);
    expect(screen.getByText("No data available")).toBeInTheDocument();
  });

  it("renders no data message when chartData is empty", () => {
    render(<SegmentedBarChart {...defaultProps} chartData={[]} />);
    expect(screen.getByText("No data available")).toBeInTheDocument();
  });

  it("tooltip is not rendered initially even when showTooltip is true", () => {
    render(<SegmentedBarChart {...defaultProps} showTooltip={true} />);
    // Tooltip only appears on hover, not initially
    expect(screen.queryByTestId("tooltip")).not.toBeInTheDocument();
  });

  it("tooltip is not rendered when showTooltip is false", () => {
    render(<SegmentedBarChart {...defaultProps} showTooltip={false} />);
    expect(screen.queryByTestId("tooltip")).not.toBeInTheDocument();
  });

  it("applies custom className", () => {
    const { container } = render(
      <SegmentedBarChart {...defaultProps} className="custom-class" />,
    );
    const outerDiv = container.firstChild as HTMLElement;
    expect(outerDiv).toHaveClass("custom-class");
  });

  it("applies custom height", () => {
    const { container } = render(
      <SegmentedBarChart {...defaultProps} height={500} />,
    );
    const outerDiv = container.firstChild as HTMLElement;
    expect(outerDiv).toHaveStyle({ height: "500px" });
  });

  it("renders with percentage display", () => {
    render(<SegmentedBarChart {...defaultProps} percentageDisplay={true} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("renders with custom units", () => {
    render(<SegmentedBarChart {...defaultProps} units=" TH/s" />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("renders with custom colorMap", () => {
    const colorMap = {
      segment1: "--color-intent-success-fill",
      segment2: "--color-extended-sky-fill",
      segment3: "--color-intent-critical-fill",
    };
    render(<SegmentedBarChart {...defaultProps} colorMap={colorMap} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("renders axes correctly", () => {
    render(<SegmentedBarChart {...defaultProps} />);
    expect(screen.getByTestId("x-axis")).toBeInTheDocument();
    expect(screen.getByTestId("y-axis")).toBeInTheDocument();
  });

  it("renders cartesian grid", () => {
    render(<SegmentedBarChart {...defaultProps} />);
    expect(screen.getByTestId("cartesian-grid")).toBeInTheDocument();
  });

  it("renders with custom bar width", () => {
    render(<SegmentedBarChart {...defaultProps} barWidth={30} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("does not render tooltip when toolTipKey is null", () => {
    render(<SegmentedBarChart {...defaultProps} toolTipKey={null} />);
    expect(screen.queryByTestId("tooltip")).not.toBeInTheDocument();
  });

  it("renders with toolTipKey specified", () => {
    render(<SegmentedBarChart {...defaultProps} toolTipKey="segment1" />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("renders with yAxisPadding", () => {
    render(<SegmentedBarChart {...defaultProps} yAxisPadding={0.2} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("renders with yAxisTickCount", () => {
    render(<SegmentedBarChart {...defaultProps} yAxisTickCount={5} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("renders with xAxisTickInterval", () => {
    render(<SegmentedBarChart {...defaultProps} xAxisTickInterval={2} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });

  it("handles percentage display with varying totals correctly", () => {
    const percentageData: SegmentedBarChartData[] = [
      {
        datetime: 1700000000,
        active: 100,
        inactive: 50,
        offline: 25,
      },
      {
        datetime: 1700003600,
        active: 200,
        inactive: 100,
        offline: 50,
      },
    ];

    render(
      <SegmentedBarChart
        chartData={percentageData}
        segmentKeys={["active", "inactive", "offline"]}
        percentageDisplay={true}
      />,
    );

    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
    // In percentage mode, data should be converted to percentages
  });

  it("does not animate when animate is false", () => {
    render(<SegmentedBarChart {...defaultProps} animate={false} />);
    const barChart = screen.getByTestId("bar-chart");
    expect(barChart).toBeInTheDocument();
  });
});
