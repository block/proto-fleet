import { BrowserRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SitesSection from "./SitesSection";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";

// SiteCard polls GetSiteStats per card; stub it so the section's own
// rendering (cards, pagination) is what's under test here.
vi.mock("@/protoFleet/api/useSiteStats", () => ({
  useSiteStats: () => ({ stats: undefined }),
}));

// Drive the responsive visible-count deterministically. Defaults to desktop
// (3 visible); individual tests flip the breakpoint as needed.
const mockWin = vi.hoisted(() => ({
  isDesktop: true,
  isLaptop: false,
  isTablet: false,
  isPhone: false,
}));
vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => mockWin,
}));

const setBreakpoint = (bp: "desktop" | "laptop" | "tablet" | "phone") => {
  mockWin.isDesktop = bp === "desktop";
  mockWin.isLaptop = bp === "laptop";
  mockWin.isTablet = bp === "tablet";
  mockWin.isPhone = bp === "phone";
};

const makeSite = (id: number, name: string) => ({ site: { id: BigInt(id), name } }) as unknown as SiteWithCounts;

const renderWithRouter = (sites: SiteWithCounts[] | undefined) =>
  render(
    <BrowserRouter>
      <SitesSection sites={sites} />
    </BrowserRouter>,
  );

describe("SitesSection", () => {
  beforeEach(() => setBreakpoint("desktop"));

  it("links the View sites button to /fleet/sites", () => {
    renderWithRouter([makeSite(1, "Alpha")]);
    expect(screen.getByTestId("dashboard-sites-view-all")).toHaveAttribute("href", "/fleet/sites");
  });

  it("renders a card per site and no pagination when sites fit one page", () => {
    renderWithRouter([makeSite(1, "Alpha"), makeSite(2, "Bravo"), makeSite(3, "Charlie")]);

    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Charlie")).toBeInTheDocument();
    expect(screen.queryByTestId("dashboard-sites-next")).not.toBeInTheDocument();
  });

  it("sizes cards to the visible count per breakpoint", () => {
    setBreakpoint("desktop");
    const { rerender } = renderWithRouter([makeSite(1, "Alpha")]);
    expect(screen.getByTestId("dashboard-sites-track").style.getPropertyValue("--site-card-w")).toBe(
      "calc((100% - 32px) / 3)",
    );

    const withWin = (sites: SiteWithCounts[]) => (
      <BrowserRouter>
        <SitesSection sites={sites} />
      </BrowserRouter>
    );

    setBreakpoint("laptop");
    rerender(withWin([makeSite(1, "Alpha")]));
    expect(screen.getByTestId("dashboard-sites-track").style.getPropertyValue("--site-card-w")).toBe(
      "calc((100% - 16px) / 2)",
    );

    setBreakpoint("phone");
    rerender(withWin([makeSite(1, "Alpha")]));
    expect(screen.getByTestId("dashboard-sites-track").style.getPropertyValue("--site-card-w")).toBe(
      "calc((100% - 0px) / 1)",
    );
  });

  it("slides the track one card at a time and clamps at the right-aligned end", () => {
    renderWithRouter([makeSite(1, "Alpha"), makeSite(2, "Bravo"), makeSite(3, "Charlie"), makeSite(4, "Delta")]);

    // All cards live in the overflowing track — sliding moves the track, it
    // doesn't add/remove cards. With 4 sites and 3 visible, maxIndex is 1.
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Delta")).toBeInTheDocument();

    const prev = screen.getByTestId("dashboard-sites-prev");
    const next = screen.getByTestId("dashboard-sites-next");
    const track = screen.getByTestId("dashboard-sites-track");

    // Start: pinned left.
    expect(prev).toBeDisabled();
    expect(next).toBeEnabled();
    expect(track.style.transform).toContain("-0 *");

    // One step advances by a single card and clamps (only one card overflows).
    fireEvent.click(next);
    expect(track.style.transform).toContain("-1 *");
    expect(next).toBeDisabled();
    expect(prev).toBeEnabled();

    // Back to the start.
    fireEvent.click(prev);
    expect(track.style.transform).toContain("-0 *");
    expect(prev).toBeDisabled();
  });

  it("renders skeleton placeholders while sites load", () => {
    renderWithRouter(undefined);
    expect(screen.getAllByTestId("skeleton-bar")).toHaveLength(3);
  });
});
