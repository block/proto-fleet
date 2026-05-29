import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import BuildingMetricsRow from "./BuildingMetricsRow";

// Minimal helper — only the fields BuildingMetricsRow reads. Cast to the
// proto shape via `unknown` so the test stays narrow without depending
// on every $typeName / $unknown field the generated message carries.
type MetricsLike = {
  totalHashrateThs: number;
  totalPowerKw: number;
  avgEfficiencyJth: number;
  reportingCount: number;
  hashrateReportingCount: number;
  efficiencyReportingCount: number;
  powerReportingCount: number;
  hashingCount: number;
  deviceCount: number;
};

const stats = (overrides: Partial<MetricsLike> = {}) =>
  ({
    totalHashrateThs: 0,
    totalPowerKw: 0,
    avgEfficiencyJth: 0,
    reportingCount: 0,
    hashrateReportingCount: 0,
    efficiencyReportingCount: 0,
    powerReportingCount: 0,
    hashingCount: 0,
    deviceCount: 0,
    ...overrides,
  }) as unknown as Parameters<typeof BuildingMetricsRow>[0]["stats"];

describe("BuildingMetricsRow", () => {
  it("renders skeletons in metric tiles while stats are loading", () => {
    render(<BuildingMetricsRow powerCapacityKw={20_000} stats={undefined} />);
    expect(screen.getByTestId("building-metric-hashrate").querySelector("[data-testid='skeleton-bar']")).not.toBeNull();
    expect(screen.getByTestId("building-metric-online").querySelector("[data-testid='skeleton-bar']")).not.toBeNull();
  });

  it("renders em dashes when no device is reporting", () => {
    render(
      <BuildingMetricsRow
        powerCapacityKw={20_000}
        stats={stats({ reportingCount: 0, hashingCount: 0, deviceCount: 0 })}
      />,
    );
    expect(screen.getByTestId("building-metric-hashrate")).toHaveTextContent("—");
    expect(screen.getByTestId("building-metric-power")).toHaveTextContent("—");
    expect(screen.getByTestId("building-metric-efficiency")).toHaveTextContent("—");
    expect(screen.getByTestId("building-metric-online")).toHaveTextContent("0 / 0");
  });

  it("renders aggregated values once stats resolve", () => {
    render(
      <BuildingMetricsRow
        powerCapacityKw={20_000}
        stats={stats({
          totalHashrateThs: 5_500, // 5.50 PH/s
          totalPowerKw: 12_345, // 12.3 MW
          avgEfficiencyJth: 28.5,
          reportingCount: 1_200,
          hashrateReportingCount: 1_200,
          efficiencyReportingCount: 1_200,
          powerReportingCount: 1_200,
          hashingCount: 1_180,
          deviceCount: 1_200,
        })}
      />,
    );
    expect(screen.getByTestId("building-metric-hashrate")).toHaveTextContent("5.50 PH/s");
    // 20_000 kW = 20 MW capacity; consumed 12_345 kW = 12.3 MW
    expect(screen.getByTestId("building-metric-power")).toHaveTextContent("12.3 / 20.0 MW");
    expect(screen.getByTestId("building-metric-efficiency")).toHaveTextContent("28.5 J/TH");
    expect(screen.getByTestId("building-metric-online")).toHaveTextContent("1,180 / 1,200");
  });

  it("renders the power tile with an em dash on the capacity side when capacity is unset", () => {
    render(
      <BuildingMetricsRow
        powerCapacityKw={0}
        stats={stats({
          totalPowerKw: 5_000,
          reportingCount: 1,
          powerReportingCount: 1,
          hashingCount: 1,
          deviceCount: 1,
        })}
      />,
    );
    expect(screen.getByTestId("building-metric-power")).toHaveTextContent("5.0 / — MW");
  });
});
