import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import ChartTooltip from "./Tooltip";

const TestTooltipIcon = ({ itemKey }: { itemKey: string }) => <span data-testid={`tooltip-icon-${itemKey}`} />;

describe("ChartTooltip", () => {
  it("disables pointer events and flips left when it would overflow the chart width", () => {
    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total", "seriesA"]}
        chartWidth={320}
        coordinate={{ x: 280, y: 120 }}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_000_000,
              total: 54,
              seriesA: 12,
            },
          },
        ]}
        segmentsLabel="Hashboards"
        tooltipWidth={100}
        tooltipXOffset={24}
        tooltipYOffset={24}
        units="W"
      />,
    );

    expect(screen.getByText("Summary")).toBeInTheDocument();

    const tooltip = container.firstChild as HTMLElement;

    expect(tooltip).toHaveClass("pointer-events-none");
    expect(tooltip.style.transform).toBe("translate(-124px, -96px)");
  });

  it("stays to the right of the cursor when there is enough space", () => {
    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartWidth={400}
        coordinate={{ x: 80, y: 40 }}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_000_000,
              total: 54,
            },
          },
        ]}
        tooltipWidth={100}
        tooltipXOffset={24}
        tooltipYOffset={24}
      />,
    );

    const tooltip = container.firstChild as HTMLElement;

    expect(tooltip.style.transform).toBe("translate(24px, -16px)");
  });

  it("clamps the flipped tooltip within the chart bounds on narrow charts", () => {
    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        activeKeys={["total"]}
        chartWidth={200}
        coordinate={{ x: 150, y: 120 }}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_000_000,
              total: 54,
            },
          },
        ]}
        tooltipWidth={190}
        tooltipXOffset={24}
        tooltipYOffset={24}
      />,
    );

    const tooltip = container.firstChild as HTMLElement;

    expect(tooltip.style.transform).toBe("translate(-150px, -96px)");
  });

  it("omits null-valued segment rows from the rendered tooltip", () => {
    render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total", "seriesA", "seriesB"]}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_000_000,
              total: 54,
              seriesA: 12,
              seriesB: null,
            },
          },
        ]}
        segmentsLabel="Hashboards"
        toolTipItemIcon={TestTooltipIcon}
      />,
    );

    expect(screen.getByText("Summary")).toBeInTheDocument();
    expect(screen.getByTestId("tooltip-icon-seriesA")).toBeInTheDocument();
    expect(screen.queryByTestId("tooltip-icon-seriesB")).not.toBeInTheDocument();
  });

  it("does not render tooltip content when the payload has no displayable values", () => {
    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_000_000,
              total: null,
            },
          },
        ]}
      />,
    );

    expect(container.firstChild).toBeNull();
  });

  it("falls back to nearest non-null data point when connectNulls is true and payload has null values", () => {
    const chartData = [
      { datetime: 1_700_000_000_000, total: 10 },
      { datetime: 1_700_000_300_000, total: null },
      { datetime: 1_700_000_600_000, total: 20 },
    ];

    render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartData={chartData}
        connectNulls={true}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_300_000,
              total: null,
            },
          },
        ]}
        segmentsLabel="Hashboards"
      />,
    );

    expect(screen.getByText("Summary")).toBeInTheDocument();
    expect(screen.getByText("10.0")).toBeInTheDocument();
  });

  it("falls back via label when connectNulls is true and Recharts strips null lines from payload", () => {
    const chartData = [
      { datetime: 1_700_000_000_000, total: 10 },
      { datetime: 1_700_000_300_000, total: null },
      { datetime: 1_700_000_600_000, total: 20 },
    ];

    render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartData={chartData}
        connectNulls={true}
        label={1_700_000_300_000}
        payload={[]}
        segmentsLabel="Hashboards"
      />,
    );

    expect(screen.getByText("Summary")).toBeInTheDocument();
    expect(screen.getByText("10.0")).toBeInTheDocument();
  });

  it("picks the closer neighbor when distances are unequal", () => {
    const chartData = [
      { datetime: 1_700_000_000_000, total: 10 },
      { datetime: 1_700_000_100_000, total: null },
      { datetime: 1_700_000_200_000, total: null },
      { datetime: 1_700_000_300_000, total: 30 },
    ];

    render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartData={chartData}
        connectNulls={true}
        label={1_700_000_250_000}
        payload={[]}
        segmentsLabel="Hashboards"
      />,
    );

    expect(screen.getByText("Summary")).toBeInTheDocument();
    expect(screen.getByText("30.0")).toBeInTheDocument();
  });

  it("does not fall back when hovering before the first non-null data point", () => {
    const chartData = [
      { datetime: 1_700_000_000_000, total: null },
      { datetime: 1_700_000_300_000, total: null },
      { datetime: 1_700_000_600_000, total: 10 },
      { datetime: 1_700_000_900_000, total: 20 },
    ];

    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartData={chartData}
        connectNulls={true}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_000_000,
              total: null,
            },
          },
        ]}
        segmentsLabel="Hashboards"
      />,
    );

    expect(container.firstChild).toBeNull();
  });

  it("does not fall back when hovering after the last non-null data point", () => {
    const chartData = [
      { datetime: 1_700_000_000_000, total: 10 },
      { datetime: 1_700_000_300_000, total: 20 },
      { datetime: 1_700_000_600_000, total: null },
      { datetime: 1_700_000_900_000, total: null },
    ];

    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartData={chartData}
        connectNulls={true}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_900_000,
              total: null,
            },
          },
        ]}
        segmentsLabel="Hashboards"
      />,
    );

    expect(container.firstChild).toBeNull();
  });

  it("does not fall back to nearest point when connectNulls is false", () => {
    const chartData = [
      { datetime: 1_700_000_000_000, total: 10 },
      { datetime: 1_700_000_300_000, total: null },
      { datetime: 1_700_000_600_000, total: 20 },
    ];

    const { container } = render(
      <ChartTooltip
        aggregateKey="total"
        aggregateLabel="Summary"
        activeKeys={["total"]}
        chartData={chartData}
        payload={[
          {
            name: "total",
            payload: {
              datetime: 1_700_000_300_000,
              total: null,
            },
          },
        ]}
      />,
    );

    expect(container.firstChild).toBeNull();
  });
});
