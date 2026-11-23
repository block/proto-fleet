import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import CompositionBar from "./CompositionBar";
import type { CompositionBarProps } from "./types";

describe("CompositionBar", () => {
  const defaultProps: CompositionBarProps = {
    segments: [
      { name: "Healthy", status: "OK", count: 45 },
      { name: "Warning", status: "WARNING", count: 10 },
      { name: "Critical", status: "CRITICAL", count: 5 },
    ],
  };

  describe("Rendering", () => {
    it("renders with custom className", () => {
      const className = "custom-class mt-4";
      render(<CompositionBar {...defaultProps} className={className} />);

      const container = screen.getByRole("group", {
        name: /composition bar chart/i,
      });
      expect(container).toHaveClass("custom-class", "mt-4");
    });

    it("renders with custom height", () => {
      const { container } = render(<CompositionBar {...defaultProps} height={16} />);

      const bar = container.querySelector(".flex.w-full");
      expect(bar).toHaveStyle({ height: "16px" });
    });

    it("renders with custom gap", () => {
      const { container } = render(<CompositionBar {...defaultProps} gap={4} />);

      const bar = container.querySelector(".flex.w-full");
      expect(bar).toHaveClass("gap-4");
    });

    it("renders with no gap when gap is 0", () => {
      const { container } = render(<CompositionBar {...defaultProps} gap={0} />);

      const bar = container.querySelector(".flex.w-full");
      // Should not have any gap class when gap is 0
      expect(bar).toHaveClass("flex", "w-full");
      expect(bar).not.toHaveClass("gap-1", "gap-2", "gap-3", "gap-4");
    });

    it("renders empty state when segments array is empty", () => {
      render(<CompositionBar segments={[]} />);

      const emptyBar = screen.getByRole("progressbar", {
        name: /no data available/i,
      });
      expect(emptyBar).toBeInTheDocument();
      expect(emptyBar).toHaveClass("bg-grayscale-gray-20");
    });

    it("renders empty state when total count is zero", () => {
      render(
        <CompositionBar
          segments={[
            { name: "Test1", status: "OK", count: 0 },
            { name: "Test2", status: "WARNING", count: 0 },
          ]}
        />,
      );

      const emptyBar = screen.getByRole("progressbar", {
        name: /no data available/i,
      });
      expect(emptyBar).toBeInTheDocument();
    });
  });

  describe("Percentage Calculation", () => {
    it("calculates correct percentages for segments", () => {
      render(<CompositionBar {...defaultProps} />);

      const progressBars = screen.getAllByRole("progressbar");

      // Total = 45 + 10 + 5 = 60
      // Healthy: 45/60 = 75%
      expect(progressBars[0]).toHaveAttribute("aria-valuenow", "75");
      expect(progressBars[0]).toHaveAttribute("aria-label", "Healthy: 75.0%");

      // Warning: 10/60 = 16.7%
      expect(progressBars[1]).toHaveAttribute("aria-valuenow", "16.666666666666664");
      expect(progressBars[1]).toHaveAttribute("aria-label", "Warning: 16.7%");

      // Critical: 5/60 = 8.3%
      expect(progressBars[2]).toHaveAttribute("aria-valuenow", "8.333333333333332");
      expect(progressBars[2]).toHaveAttribute("aria-label", "Critical: 8.3%");
    });

    it("filters out segments with zero count", () => {
      render(
        <CompositionBar
          segments={[
            { name: "Active", status: "OK", count: 50 },
            { name: "Warning", status: "WARNING", count: 0 },
            { name: "Critical", status: "CRITICAL", count: 10 },
            { name: "Unknown", status: "NA", count: 0 },
          ]}
        />,
      );

      const progressBars = screen.getAllByRole("progressbar");
      // Should only render 2 segments (Active and Critical)
      expect(progressBars).toHaveLength(2);

      // Check the rendered segments
      expect(progressBars[0]).toHaveAttribute("aria-label", expect.stringContaining("Active"));
      expect(progressBars[1]).toHaveAttribute("aria-label", expect.stringContaining("Critical"));
    });
  });

  describe("Status Colors", () => {
    it("applies correct color classes for each status", () => {
      const { container } = render(
        <CompositionBar
          segments={[
            { name: "OK", status: "OK", count: 1 },
            { name: "Warning", status: "WARNING", count: 1 },
            { name: "Critical", status: "CRITICAL", count: 1 },
            { name: "NA", status: "NA", count: 1 },
          ]}
        />,
      );

      const segments = container.querySelectorAll('[role="progressbar"]');
      expect(segments[0]).toHaveClass("bg-intent-success-fill");
      expect(segments[1]).toHaveClass("bg-intent-warning-fill");
      expect(segments[2]).toHaveClass("bg-intent-critical-fill");
      expect(segments[3]).toHaveClass("bg-grayscale-gray-50");
    });
  });

  describe("Accessibility", () => {
    it("has correct ARIA attributes", () => {
      render(<CompositionBar {...defaultProps} />);

      // Check group label
      expect(screen.getByRole("group", { name: /composition bar chart/i })).toBeInTheDocument();

      // Check progressbar attributes
      const progressBars = screen.getAllByRole("progressbar");
      progressBars.forEach((bar) => {
        expect(bar).toHaveAttribute("aria-valuemin", "0");
        expect(bar).toHaveAttribute("aria-valuemax", "100");
        expect(bar).toHaveAttribute("aria-valuenow");
        expect(bar).toHaveAttribute("aria-label");
      });
    });

    it("handles empty state with appropriate ARIA attributes", () => {
      render(<CompositionBar segments={[]} />);

      const emptyBar = screen.getByRole("progressbar", {
        name: /no data available/i,
      });
      expect(emptyBar).toHaveAttribute("aria-valuenow", "0");
      expect(emptyBar).toHaveAttribute("aria-valuemin", "0");
      expect(emptyBar).toHaveAttribute("aria-valuemax", "100");
    });
  });

  describe("Edge Cases", () => {
    it("renders skeleton bar when all counts are undefined", () => {
      render(
        <CompositionBar
          segments={[
            { name: "Healthy", status: "OK", count: undefined },
            { name: "Warning", status: "WARNING", count: undefined },
            { name: "Critical", status: "CRITICAL", count: undefined },
          ]}
        />,
      );

      const skeleton = screen.getByTestId("composition-bar-skeleton");
      expect(skeleton).toBeInTheDocument();

      // Should not render progress bars
      const progressBars = screen.queryAllByRole("progressbar");
      expect(progressBars).toHaveLength(0);
    });

    it("renders normally when some counts are defined", () => {
      render(
        <CompositionBar
          segments={[
            { name: "Healthy", status: "OK", count: 45 },
            { name: "Warning", status: "WARNING", count: undefined },
            { name: "Critical", status: "CRITICAL", count: 5 },
          ]}
        />,
      );

      // Should not render skeleton
      const skeleton = screen.queryByTestId("composition-bar-skeleton");
      expect(skeleton).not.toBeInTheDocument();

      // Should render progress bars for defined segments
      const progressBars = screen.getAllByRole("progressbar");
      expect(progressBars).toHaveLength(2); // Only Healthy and Critical
    });

    it("handles negative counts by treating them as zero", () => {
      render(
        <CompositionBar
          segments={[
            { name: "Positive", status: "OK", count: 50 },
            { name: "Negative", status: "WARNING", count: -10 },
            { name: "Zero", status: "CRITICAL", count: 0 },
          ]}
        />,
      );

      // Negative and zero counts should be filtered out
      const progressBars = screen.getAllByRole("progressbar");
      expect(progressBars).toHaveLength(1);
      // Only positive value remains, so it should be 100%
      expect(progressBars[0]).toHaveAttribute("aria-label", "Positive: 100.0%");
    });
  });
});
