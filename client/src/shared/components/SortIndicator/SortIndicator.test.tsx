import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import SortIndicator from "./SortIndicator";

describe("SortIndicator", () => {
  describe("not sorted", () => {
    it("renders invisible placeholder when not hovering", () => {
      // Act
      const { container } = render(<SortIndicator direction={undefined} />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveClass("invisible");
    });

    it("renders grey arrow showing defaultDirection when hovering (defaults to DESC)", () => {
      // Act
      const { container } = render(<SortIndicator direction={undefined} isHovering />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveClass("text-text-primary-50");
      // Default is DESC, so should show down arrow
      expect(container.querySelector("svg")).toBeTruthy();
    });

    it("renders grey up arrow when hovering with defaultDirection=asc", () => {
      // Act
      const { container } = render(<SortIndicator direction={undefined} defaultDirection="asc" isHovering />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveClass("text-text-primary-50");
    });
  });

  describe("sorted ascending", () => {
    it("renders up arrow in primary color when not hovering", () => {
      // Act
      const { container } = render(<SortIndicator direction="asc" />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).not.toHaveClass("text-text-primary-50");
    });

    it("renders grey down arrow when hovering (DESC preview)", () => {
      // Act
      const { container } = render(<SortIndicator direction="asc" isHovering />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveClass("text-text-primary-50");
    });
  });

  describe("sorted descending", () => {
    it("renders down arrow in primary color when not hovering", () => {
      // Act
      const { container } = render(<SortIndicator direction="desc" />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).not.toHaveClass("text-text-primary-50");
    });

    it("renders grey up arrow when hovering (ASC preview)", () => {
      // Act
      const { container } = render(<SortIndicator direction="desc" isHovering />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveClass("text-text-primary-50");
    });
  });

  describe("accessibility", () => {
    it("has aria-hidden attribute", () => {
      // Act
      const { container } = render(<SortIndicator direction="asc" />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveAttribute("aria-hidden", "true");
    });
  });

  describe("styling", () => {
    it("applies custom className", () => {
      // Act
      const { container } = render(<SortIndicator direction="asc" className="custom-class" />);

      // Assert
      const indicator = container.querySelector("div");
      expect(indicator).toHaveClass("custom-class");
    });
  });
});
