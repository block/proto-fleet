import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import ChartWidget from "./ChartWidget";

describe("ChartWidget", () => {
  it("renders single stat", () => {
    render(
      <ChartWidget stats={{ label: "Hashrate", value: "230.2", text: "TH/s" }}>
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("Hashrate")).toBeInTheDocument();
    expect(screen.getByText("230.2")).toBeInTheDocument();
    expect(screen.getByText("TH/s")).toBeInTheDocument();
  });

  it("renders multiple stats", () => {
    render(
      <ChartWidget
        stats={[
          { label: "Hashrate", value: "230.2", text: "TH/s" },
          { label: "Efficiency", value: "67", text: "J/TH" },
          { label: "Temperature", value: "65°", text: "Average" },
        ]}
        statsGrid="grid-cols-3"
      >
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("Hashrate")).toBeInTheDocument();
    expect(screen.getByText("230.2")).toBeInTheDocument();
    expect(screen.getByText("TH/s")).toBeInTheDocument();
    expect(screen.getByText("Efficiency")).toBeInTheDocument();
    expect(screen.getByText("67.0")).toBeInTheDocument();
    expect(screen.getByText("J/TH")).toBeInTheDocument();
    expect(screen.getByText("Temperature")).toBeInTheDocument();
    expect(screen.getByText("65°")).toBeInTheDocument();
    expect(screen.getByText("Average")).toBeInTheDocument();
  });

  it("renders without stats", () => {
    render(
      <ChartWidget>
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("Chart Content")).toBeInTheDocument();
  });

  it("renders children correctly", () => {
    render(
      <ChartWidget>
        <div data-testid="chart-content">Mock Chart</div>
      </ChartWidget>,
    );

    expect(screen.getByTestId("chart-content")).toBeInTheDocument();
    expect(screen.getByText("Mock Chart")).toBeInTheDocument();
  });

  it("applies custom className", () => {
    const { container } = render(
      <ChartWidget className="custom-class">
        <div>Chart Content</div>
      </ChartWidget>,
    );

    const widget = container.firstChild as HTMLElement;
    expect(widget).toHaveClass("custom-class");
    expect(widget).toHaveClass("rounded-xl");
    expect(widget).toHaveClass("bg-surface-base");
    expect(widget).toHaveClass("p-10");
  });

  it("handles percentage values with special formatting", () => {
    render(
      <ChartWidget stats={{ label: "Efficiency", value: "85%", text: "Current" }}>
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("85%")).toBeInTheDocument();
    expect(screen.getByText("Current")).toBeInTheDocument();
  });

  it("uses custom stats configuration", () => {
    const { container } = render(
      <ChartWidget
        stats={[
          { label: "Stat 1", value: "100" },
          { label: "Stat 2", value: "200" },
        ]}
        statsGrid="grid-cols-2"
        statsGap="gap-x-4"
        statsPadding="pb-8"
        statsSize="small"
      >
        <div>Chart Content</div>
      </ChartWidget>,
    );

    // Check that Stats component is rendered (would have the grid class)
    const statsContainer = container.querySelector(".grid-cols-2");
    expect(statsContainer).toBeInTheDocument();
  });
});
