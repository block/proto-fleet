import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { statusColors } from "./constants";
import Stat from "./Stat";

describe("Stat", () => {
  it("renders basic stat with label and value", () => {
    render(<Stat label="Hashrate" value={"value"} size="small" />);

    expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent("Hashrate");
    expect(screen.getByText("value")).toBeInTheDocument();
  });

  it("renders loading state when value is undefined", () => {
    render(<Stat label="Hashrate" value={undefined} size="small" />);

    expect(screen.getByTestId("skeleton-bar")).toBeInTheDocument();
  });

  it("renders units when provided", () => {
    render(<Stat label="Hashrate" value={"value"} units="TH/s" size="small" />);

    expect(screen.getByText(/TH\/s/)).toBeInTheDocument();
  });

  it("renders descriptive text when provided", () => {
    const text = "2.1% below expected";
    render(<Stat label="Hashrate" value={"value"} text={text} size="small" />);

    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it("renders icon when provided", () => {
    const TestIcon = () => <div data-testid="test-icon">Icon</div>;
    render(<Stat label="Hashrate" value={"value"} icon={<TestIcon />} size="small" />);

    expect(screen.getByTestId("test-icon")).toBeInTheDocument();
  });

  it("applies correct size classes", () => {
    const { rerender } = render(<Stat label="Hashrate" value={"value"} size="large" />);
    expect(screen.getByText("value").parentElement).toHaveClass("text-heading-300");

    rerender(<Stat label="Hashrate" value={"value"} size="medium" />);
    expect(screen.getByText("value").parentElement).toHaveClass("text-heading-200");

    rerender(<Stat label="Hashrate" value={"value"} size="small" />);
    expect(screen.getByText("value").parentElement).toHaveClass("text-heading-100");
  });

  it("renders chart with correct status color", async () => {
    const { container } = render(
      <Stat label="Hashrate" value={"value"} chartPercentage={74.2} chartStatus="warning" size="small" />,
    );

    // Since getAllByClassName doesn't exist, let's use a more appropriate query
    const chartBars = container.getElementsByClassName(statusColors.warning);
    expect(chartBars).toHaveLength(2); // Background and foreground bars
    await waitFor(() => {
      expect(chartBars[1]).toHaveStyle({ transform: "scaleX(0.742)" });
    });
  });

  it("uses custom heading level when provided", () => {
    render(<Stat label="Hashrate" value={"value"} headingLevel={2} size="small" />);

    expect(screen.getByRole("heading", { level: 2 })).toBeInTheDocument();
  });
});
