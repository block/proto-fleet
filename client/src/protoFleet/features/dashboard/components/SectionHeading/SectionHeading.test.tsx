import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import SectionHeading from "./SectionHeading";

describe("SectionHeading", () => {
  it("renders heading text", () => {
    render(<SectionHeading heading="Performance" />);

    expect(screen.getByText("Performance")).toBeInTheDocument();
  });

  it("renders without children", () => {
    render(<SectionHeading heading="Overview" />);

    expect(screen.getByText("Overview")).toBeInTheDocument();
  });

  it("renders with children controls", () => {
    render(
      <SectionHeading heading="Performance">
        <button>1h</button>
        <button>24h</button>
      </SectionHeading>,
    );

    expect(screen.getByText("Performance")).toBeInTheDocument();
    expect(screen.getByText("1h")).toBeInTheDocument();
    expect(screen.getByText("24h")).toBeInTheDocument();
  });

  it("applies custom className", () => {
    const { container } = render(<SectionHeading heading="Test" className="custom-class" />);

    const sectionHeading = container.firstChild as HTMLElement;
    expect(sectionHeading).toHaveClass("custom-class");
  });
});
