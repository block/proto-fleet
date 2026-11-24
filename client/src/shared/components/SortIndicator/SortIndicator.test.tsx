import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import SortIndicator from "./SortIndicator";
import type { SortDirection } from "@/shared/components/List/types";

vi.mock("@/shared/assets/icons", () => ({
  ArrowUp: () => <span data-testid="arrow-up" />,
  ArrowDown: () => <span data-testid="arrow-down" />,
}));

type Row = {
  name: string;
  direction?: SortDirection;
  defaultDirection?: SortDirection;
  isHovering?: boolean;
  icon: "arrow-up" | "arrow-down";
  grey: boolean;
};

describe("SortIndicator", () => {
  it("renders invisible placeholder when not sorted and not hovering", () => {
    const { container } = render(<SortIndicator direction={undefined} />);

    expect(container.querySelector("div")).toHaveClass("invisible");
  });

  it.each<Row>([
    { name: "not sorted, hover, no defaultDirection", isHovering: true, icon: "arrow-up", grey: true },
    {
      name: "not sorted, hover, defaultDirection asc",
      defaultDirection: "asc",
      isHovering: true,
      icon: "arrow-up",
      grey: true,
    },
    {
      name: "not sorted, hover, defaultDirection desc",
      defaultDirection: "desc",
      isHovering: true,
      icon: "arrow-down",
      grey: true,
    },
    { name: "sorted asc, no hover", direction: "asc", icon: "arrow-up", grey: false },
    { name: "sorted asc, hover (desc preview)", direction: "asc", isHovering: true, icon: "arrow-down", grey: true },
    { name: "sorted desc, no hover", direction: "desc", icon: "arrow-down", grey: false },
    { name: "sorted desc, hover (asc preview)", direction: "desc", isHovering: true, icon: "arrow-up", grey: true },
  ])("$name: shows $icon, grey=$grey", ({ direction, defaultDirection, isHovering, icon, grey }) => {
    const { container } = render(
      <SortIndicator direction={direction} defaultDirection={defaultDirection} isHovering={isHovering} />,
    );

    const indicator = container.querySelector("div");
    expect(indicator).not.toHaveClass("invisible");
    expect(screen.getByTestId(icon)).toBeInTheDocument();
    expect(screen.queryByTestId(icon === "arrow-up" ? "arrow-down" : "arrow-up")).not.toBeInTheDocument();
    if (grey) {
      expect(indicator).toHaveClass("text-text-primary-50");
    } else {
      expect(indicator).not.toHaveClass("text-text-primary-50");
    }
  });

  describe("accessibility", () => {
    it("has aria-hidden attribute", () => {
      const { container } = render(<SortIndicator direction="asc" />);

      expect(container.querySelector("div")).toHaveAttribute("aria-hidden", "true");
    });
  });

  describe("styling", () => {
    it("applies custom className", () => {
      const { container } = render(<SortIndicator direction="asc" className="custom-class" />);

      expect(container.querySelector("div")).toHaveClass("custom-class");
    });
  });
});
