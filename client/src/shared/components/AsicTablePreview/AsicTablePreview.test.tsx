import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import AsicTablePreview from "./AsicTablePreview";
import type { AsicData } from "./types";

describe("AsicTablePreview", () => {
  const defaultProps = {
    // Optional props will use defaults: warningThreshold=65, dangerThreshold=82, criticalThreshold=90
  };

  it("renders the correct grid dimensions", () => {
    const asics: AsicData[] = [
      { row: 0, col: 0, value: 50 },
      { row: 0, col: 1, value: 60 },
      { row: 1, col: 0, value: 70 },
      { row: 1, col: 1, value: 80 },
    ];

    render(<AsicTablePreview asics={asics} {...defaultProps} />);

    // Should render 4 chips (2x2 grid)
    const chips = screen.getAllByTestId(/^asic-/);
    expect(chips).toHaveLength(4);
  });

  it("handles sparse grids correctly", () => {
    const asics: AsicData[] = [
      { row: 0, col: 0, value: 50 },
      { row: 2, col: 3, value: 60 }, // Creates a 3x4 grid
    ];

    render(<AsicTablePreview asics={asics} {...defaultProps} />);

    // Should render 12 chips (3 rows x 4 cols)
    const chips = screen.getAllByTestId(/^asic-/);
    expect(chips).toHaveLength(12);
  });

  it("applies empty color to null values", () => {
    const asics: AsicData[] = [
      { row: 0, col: 0, value: null },
      { row: 0, col: 1, value: 50 },
    ];

    render(
      <AsicTablePreview
        asics={asics}
        {...defaultProps}
        colors={{
          normal: "#0096D1",
          warning: "#FD8A00",
          critical: "#FA2B37",
          empty: "#CCCCCC",
        }}
      />,
    );

    const nullChip = screen.getByTestId("asic-0-0");
    expect(nullChip).toHaveStyle({ backgroundColor: "#CCCCCC", opacity: "1" });
  });

  it("applies normal color for values below warning", () => {
    const asics: AsicData[] = [{ row: 0, col: 0, value: 40 }];

    const { container } = render(
      <AsicTablePreview
        asics={asics}
        {...defaultProps}
        colors={{
          normal: "#0000FF",
          warning: "#FD8A00",
          critical: "#FA2B37",
          empty: "#F2F2F2",
        }}
      />,
    );

    const chip = container.querySelector('[data-value="40"]');
    expect(chip).toHaveStyle({ backgroundColor: "#0000FF" });
  });

  it("applies critical color for values above danger threshold", () => {
    const asics: AsicData[] = [{ row: 0, col: 0, value: 85 }]; // Above danger (82)

    const { container } = render(
      <AsicTablePreview
        asics={asics}
        {...defaultProps}
        colors={{
          normal: "#0096D1",
          warning: "#FD8A00",
          critical: "#FF0000",
          empty: "#F2F2F2",
        }}
      />,
    );

    const chip = container.querySelector('[data-value="85"]');
    expect(chip).toHaveStyle({ backgroundColor: "#FF0000" });
  });

  it("applies warning color between warning and danger thresholds", () => {
    const asics: AsicData[] = [{ row: 0, col: 0, value: 70 }]; // Between warning (65) and danger (82)

    const { container } = render(
      <AsicTablePreview
        asics={asics}
        {...defaultProps}
        colors={{
          normal: "#0066FF",
          warning: "#FFB800",
          critical: "#FF0000",
          empty: "#F2F2F2",
        }}
      />,
    );

    const chip = container.querySelector('[data-value="70"]');
    expect(chip).toHaveStyle({ backgroundColor: "#FFB800" });
    // Opacity should be mapped between 0.4 and 1.0
    const style = window.getComputedStyle(chip!);
    const opacity = parseFloat(style.opacity);
    expect(opacity).toBeGreaterThan(0.4);
    expect(opacity).toBeLessThanOrEqual(1.0);
  });

  it("varies opacity for normal range values", () => {
    const asics: AsicData[] = [
      { row: 0, col: 0, value: 30 }, // Low temp
      { row: 0, col: 1, value: 50 }, // Middle temp
      { row: 0, col: 2, value: 64 }, // Just below warning
    ];

    const { container } = render(<AsicTablePreview asics={asics} {...defaultProps} />);

    const lowChip = container.querySelector('[data-value="30"]') as HTMLElement;
    const midChip = container.querySelector('[data-value="50"]') as HTMLElement;
    const highChip = container.querySelector('[data-value="64"]') as HTMLElement;

    // Opacity should vary based on map(value, 30, 65, 0.4, 0.05)
    // Lower values should have higher opacity in this inverted mapping
    const lowOpacity = parseFloat(lowChip!.style.opacity);
    const midOpacity = parseFloat(midChip!.style.opacity);
    const highOpacity = parseFloat(highChip!.style.opacity);

    // All should be within the mapped range
    expect(lowOpacity).toBeGreaterThanOrEqual(0.05);
    expect(lowOpacity).toBeLessThanOrEqual(0.4);
    expect(midOpacity).toBeGreaterThanOrEqual(0.05);
    expect(midOpacity).toBeLessThanOrEqual(0.4);
    expect(highOpacity).toBeGreaterThanOrEqual(0.05);
    expect(highOpacity).toBeLessThanOrEqual(0.4);
  });

  it("uses default colors when not provided", () => {
    const asics: AsicData[] = [
      { row: 0, col: 0, value: 50 },
      { row: 0, col: 1, value: null },
    ];

    render(<AsicTablePreview asics={asics} {...defaultProps} />);

    const normalChip = screen.getByTestId("asic-0-0");
    const emptyChip = screen.getByTestId("asic-0-1");

    expect(normalChip).toHaveStyle({ backgroundColor: "var(--color-intent-info-fill)" });
    expect(emptyChip).toHaveStyle({ backgroundColor: "var(--color-surface-5)" });
  });

  it("applies custom className", () => {
    const asics: AsicData[] = [{ row: 0, col: 0, value: 50 }];

    const { container } = render(<AsicTablePreview asics={asics} {...defaultProps} className="custom-class" />);

    const wrapper = container.firstChild;
    expect(wrapper).toHaveClass("custom-class");
  });

  it("handles empty asics array", () => {
    render(<AsicTablePreview asics={[]} {...defaultProps} />);

    // Should render without errors but with no chips
    const chips = screen.queryAllByTestId(/^asic-/);
    expect(chips).toHaveLength(0);
  });

  it("handles negative indices gracefully", () => {
    const asics: AsicData[] = [
      { row: -1, col: 0, value: 50 }, // Should be ignored
      { row: 0, col: -1, value: 60 }, // Should be ignored
      { row: 0, col: 0, value: 70 }, // Should be rendered
    ];

    render(<AsicTablePreview asics={asics} {...defaultProps} />);

    const chips = screen.getAllByTestId(/^asic-/);
    expect(chips).toHaveLength(1);
    expect(chips[0]).toHaveAttribute("data-value", "70");
  });
});
