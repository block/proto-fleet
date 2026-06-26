import { BrowserRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FleetHealthSection from "./FleetHealthSection";
import { type ActiveSite } from "@/protoFleet/store/types/activeSite";

// useSiteStats hits the network; stub it so we can drive the performance
// subheading deterministically.
const mockStats = vi.hoisted(() => ({ current: undefined as unknown }));
vi.mock("@/protoFleet/api/useSiteStats", () => ({
  useSiteStats: () => ({ stats: mockStats.current }),
}));

// The bottom-half panel pulls in buildings/racks/component data hooks of its
// own; it has dedicated coverage in SiteResourcePanel.test. Stub it here so
// these tests stay focused on the header + health summary.
vi.mock("./SiteResourcePanel", () => ({
  default: () => <div data-testid="site-resource-panel" />,
}));

const ACTIVE_SITE: ActiveSite = { kind: "site", id: "8", slug: "austin" };

const renderSection = (props?: Partial<Parameters<typeof FleetHealthSection>[0]>) =>
  render(
    <BrowserRouter>
      <FleetHealthSection
        activeSite={ACTIVE_SITE}
        siteId={8n}
        powerCapacityMw={12}
        fleetSize={100}
        healthyMiners={80}
        needsAttentionMiners={10}
        offlineMiners={6}
        sleepingMiners={4}
        {...props}
      />
    </BrowserRouter>,
  );

describe("FleetHealthSection", () => {
  beforeEach(() => {
    mockStats.current = undefined;
  });

  it("scopes the View sites / View miners links to the active site", () => {
    renderSection();
    expect(screen.getByTestId("dashboard-fleet-health-view-sites")).toHaveAttribute("href", "/austin/fleet/sites");
    expect(screen.getByTestId("dashboard-fleet-health-view-miners")).toHaveAttribute("href", "/austin/fleet/miners");
  });

  it("builds the performance subheading from the site roll-up", () => {
    mockStats.current = {
      totalHashrateThs: 922_600,
      hashrateReportingCount: 100,
      totalPowerKw: 3_500,
      powerReportingCount: 100,
      avgEfficiencyJth: 3.8,
      efficiencyReportingCount: 100,
    };
    renderSection();

    expect(screen.getByTestId("dashboard-fleet-health-subheading")).toHaveTextContent(
      "922.6 PH/s, 3.5 MW (29% of 12 MW), 3.8 J/TH",
    );
  });

  it("drops non-reporting metrics from the subheading", () => {
    mockStats.current = {
      totalHashrateThs: 922_600,
      hashrateReportingCount: 100,
      totalPowerKw: 0,
      powerReportingCount: 0,
      avgEfficiencyJth: 0,
      efficiencyReportingCount: 0,
    };
    renderSection();

    const subheading = screen.getByTestId("dashboard-fleet-health-subheading");
    expect(subheading).toHaveTextContent("922.6 PH/s");
    expect(subheading).not.toHaveTextContent("MW");
    expect(subheading).not.toHaveTextContent("J/TH");
  });

  it("renders the health bar and the resource panel inside the module", () => {
    renderSection();
    expect(screen.getByTestId("dashboard-fleet-health-bar")).toBeInTheDocument();
    expect(screen.getByTestId("site-resource-panel")).toBeInTheDocument();
  });

  it("falls back to a skeleton bar while counts are loading", () => {
    renderSection({ healthyMiners: undefined });
    expect(screen.queryByTestId("dashboard-fleet-health-bar")).not.toBeInTheDocument();
  });
});
