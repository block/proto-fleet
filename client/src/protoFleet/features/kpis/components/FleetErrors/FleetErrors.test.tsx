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
    expect(links[0]).toHaveAttribute("href", "/fleet/miners?issues=control-board");
    expect(links[1]).toHaveAttribute("href", "/fleet/miners?issues=fans");
    expect(links[2]).toHaveAttribute("href", "/fleet/miners?issues=hash-boards");
    expect(links[3]).toHaveAttribute("href", "/fleet/miners?issues=psu");
  });

  it("preserves the selected site when building scoped hardware issue links", () => {
    render(
      <BrowserRouter>
        <FleetErrors
          controlBoardErrors={1}
          fanErrors={2}
          hashboardErrors={3}
          psuErrors={4}
          extraFilterParams="building=123"
          activeSite={{ kind: "site", id: "8", slug: "austin" }}
        />
      </BrowserRouter>,
    );

    const links = screen.getAllByRole("link");
    expect(links[0]).toHaveAttribute("href", "/austin/fleet/miners?issues=control-board&building=123");
    expect(links[1]).toHaveAttribute("href", "/austin/fleet/miners?issues=fans&building=123");
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

  it("applies custom gapClassName", () => {
    const { container } = render(
      <BrowserRouter>
        <FleetErrors gapClassName="gap-1" />
      </BrowserRouter>,
    );

    const grid = container.querySelector(".grid");
    expect(grid).toHaveClass("gap-1");
    expect(grid).not.toHaveClass("gap-4");
  });
});
