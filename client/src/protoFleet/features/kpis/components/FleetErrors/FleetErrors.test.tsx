import { BrowserRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import FleetErrors from "./FleetErrors";

describe("FleetErrors", () => {
  it("renders all four hardware error sections", () => {
    render(
      <BrowserRouter>
        <FleetErrors />
      </BrowserRouter>,
    );

    expect(screen.getByText("Control Boards")).toBeInTheDocument();
    expect(screen.getByText("Fans")).toBeInTheDocument();
    expect(screen.getByText("Hashboards")).toBeInTheDocument();
    expect(screen.getByText("Power supplies")).toBeInTheDocument();
  });

  it("displays correct error counts", () => {
    render(
      <BrowserRouter>
        <FleetErrors controlBoardErrors={0} fanErrors={0} hashboardErrors={42} psuErrors={58} />
      </BrowserRouter>,
    );

    const noIssues = screen.getAllByText("No issues");
    expect(noIssues).toHaveLength(2);
    expect(screen.getByText("42 miners need attention")).toBeInTheDocument();
    expect(screen.getByText("58 miners need attention")).toBeInTheDocument();
  });

  it("renders all components as links with correct filters when errors exist", () => {
    render(
      <BrowserRouter>
        <FleetErrors controlBoardErrors={1} fanErrors={2} hashboardErrors={3} psuErrors={4} />
      </BrowserRouter>,
    );

    const links = screen.getAllByRole("link");
    expect(links).toHaveLength(4);
    expect(links[0]).toHaveAttribute("href", "/miners?issues=control-board");
    expect(links[1]).toHaveAttribute("href", "/miners?issues=fans");
    expect(links[2]).toHaveAttribute("href", "/miners?issues=hash-boards");
    expect(links[3]).toHaveAttribute("href", "/miners?issues=psu");
  });

  it("does not render as links when error counts are zero", () => {
    render(
      <BrowserRouter>
        <FleetErrors controlBoardErrors={0} fanErrors={0} hashboardErrors={0} psuErrors={0} />
      </BrowserRouter>,
    );

    expect(screen.queryAllByRole("link")).toHaveLength(0);
  });

  it("applies custom className", () => {
    const { container } = render(
      <BrowserRouter>
        <FleetErrors className="custom-class" />
      </BrowserRouter>,
    );

    const fleetErrors = container.firstChild as HTMLElement;
    expect(fleetErrors).toHaveClass("custom-class");
  });
});
