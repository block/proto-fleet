import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import ChartWidget from "./ChartWidget";

describe("ChartWidget", () => {
  it("renders label and value", () => {
    render(
      <ChartWidget label="Hashrate" value="230.2" units="TH/s">
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("Hashrate")).toBeInTheDocument();
    expect(screen.getByText("230.2")).toBeInTheDocument();
    expect(screen.getByText("TH/s")).toBeInTheDocument();
  });

  it("renders without units", () => {
    render(
      <ChartWidget label="Efficiency" value="67">
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("Efficiency")).toBeInTheDocument();
    expect(screen.getByText("67")).toBeInTheDocument();
  });

  it("renders children correctly", () => {
    render(
      <ChartWidget label="Test" value="100">
        <div data-testid="chart-content">Mock Chart</div>
      </ChartWidget>,
    );

    expect(screen.getByTestId("chart-content")).toBeInTheDocument();
    expect(screen.getByText("Mock Chart")).toBeInTheDocument();
  });

  it("applies custom className", () => {
    const { container } = render(
      <ChartWidget label="Test" value="100" className="custom-class">
        <div>Chart Content</div>
      </ChartWidget>,
    );

    const widget = container.firstChild as HTMLElement;
    expect(widget).toHaveClass("custom-class");
  });

  it("handles numeric values", () => {
    render(
      <ChartWidget label="Count" value={42} units="items">
        <div>Chart Content</div>
      </ChartWidget>,
    );

    expect(screen.getByText("42")).toBeInTheDocument();
    expect(screen.getByText("items")).toBeInTheDocument();
  });
});
