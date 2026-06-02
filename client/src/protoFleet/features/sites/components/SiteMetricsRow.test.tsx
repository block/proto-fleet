import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import SiteMetricsRow from "./SiteMetricsRow";

describe("SiteMetricsRow", () => {
  it("shows location, capacity and buildings count without waiting on metrics", () => {
    render(
      <SiteMetricsRow
        locationCity="Austin"
        locationState="TX"
        powerCapacityMw={20}
        buildingCount={3}
        metrics={undefined}
      />,
    );
    expect(screen.getByTestId("site-metric-location")).toHaveTextContent("Austin, TX");
    expect(screen.getByTestId("site-metric-buildings")).toHaveTextContent("3");
    expect(screen.getByTestId("site-metric-hashrate").querySelector("[data-testid='skeleton-bar']")).not.toBeNull();
  });

  it("renders aggregated values once stats resolve", () => {
    render(
      <SiteMetricsRow
        locationCity="Austin"
        locationState="TX"
        powerCapacityMw={20}
        buildingCount={2}
        metrics={{
          totalHashrateThs: 2_500_000,
          totalPowerKw: 12_345,
          avgEfficiencyJth: 28.5,
          reportingCount: 1200,
          hashrateReportingCount: 1200,
          efficiencyReportingCount: 1200,
          powerReportingCount: 1200,
        }}
      />,
    );
    expect(screen.getByTestId("site-metric-hashrate")).toHaveTextContent("2.50 EH/s");
    expect(screen.getByTestId("site-metric-power")).toHaveTextContent("12.3 / 20.0 MW");
    expect(screen.getByTestId("site-metric-efficiency")).toHaveTextContent("28.5 J/TH");
  });

  it("renders em dashes when no devices are reporting", () => {
    render(
      <SiteMetricsRow
        locationCity=""
        locationState=""
        powerCapacityMw={0}
        buildingCount={0}
        metrics={{
          totalHashrateThs: 0,
          totalPowerKw: 0,
          avgEfficiencyJth: 0,
          reportingCount: 0,
          hashrateReportingCount: 0,
          efficiencyReportingCount: 0,
          powerReportingCount: 0,
        }}
      />,
    );
    expect(screen.getByTestId("site-metric-location")).toHaveTextContent("—");
    expect(screen.getByTestId("site-metric-hashrate")).toHaveTextContent("—");
    expect(screen.getByTestId("site-metric-power")).toHaveTextContent("—");
    expect(screen.getByTestId("site-metric-efficiency")).toHaveTextContent("—");
  });
});
