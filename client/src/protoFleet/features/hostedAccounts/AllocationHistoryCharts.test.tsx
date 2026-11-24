import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { accountDashboardData } from "./accountDashboardData";
import { AccountDashboardReadingSchema } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { EfficiencyPanel } from "@/protoFleet/features/dashboard/components/EfficiencyPanel";
import { HashratePanel } from "@/protoFleet/features/dashboard/components/HashratePanel";
import { PowerPanel } from "@/protoFleet/features/dashboard/components/PowerPanel";

vi.mock("@/protoFleet/components/LineChart", () => ({
  default: ({ chartData }: { chartData: unknown }) => <pre data-testid="chart-data">{JSON.stringify(chartData)}</pre>,
}));

describe("allocation history chart boundaries", () => {
  it("retains separate equipment periods and missing readings inside one regular bucket", () => {
    const history = accountDashboardData([
      create(AccountDashboardReadingSchema, {
        time: { seconds: 180n },
        summary: {
          assetCount: 1n,
          moduleCount: 1n,
          hashrateHs: 300e12,
          hashrateReportingCount: 1n,
          powerW: 6000,
          powerReportingCount: 1n,
          efficiencyJTh: 20,
        },
      }),
      create(AccountDashboardReadingSchema, {
        time: { seconds: 181n, nanos: 125000000 },
        summary: {
          assetCount: 3n,
          hashboardCount: 3n,
          hashrateHs: 300e12,
          hashrateReportingCount: 3n,
          powerW: 6000,
          powerReportingCount: 3n,
          efficiencyJTh: 20,
        },
      }),
      create(AccountDashboardReadingSchema, {
        time: { seconds: 182n },
        summary: { assetCount: 3n, hashboardCount: 3n },
      }),
    ]);
    const metrics = (type: MeasurementType) => history.metrics.filter((metric) => metric.measurementType === type);
    render(
      <>
        <HashratePanel duration="1h" metrics={metrics(MeasurementType.HASHRATE)} padMissingBuckets={false} />
        <PowerPanel
          duration="1h"
          metrics={metrics(MeasurementType.POWER)}
          metricsInKW
          totalMiners={3}
          padMissingBuckets={false}
        />
        <EfficiencyPanel
          duration="1h"
          metrics={metrics(MeasurementType.EFFICIENCY)}
          totalMiners={3}
          padMissingBuckets={false}
        />
      </>,
    );
    const charts = screen.getAllByTestId("chart-data").map((element) => JSON.parse(element.textContent!));
    for (const points of charts)
      expect(points.map((point: { datetime: number }) => point.datetime)).toEqual([180000, 181125, 182000]);
    expect(charts[0].map((point: { hashrate: number | null }) => point.hashrate)).toEqual([300, 300, null]);
    expect(charts[1].map((point: { power: number | null }) => point.power)).toEqual([6, 6, null]);
    expect(history.hasLegacyModuleReadings).toBe(true);
    expect(history.tooltipNotes[MeasurementType.HASHRATE]?.[180000]).toBe(
      "Legacy module data · 1 of 1 module reporting",
    );
    expect(history.tooltipNotes[MeasurementType.HASHRATE]?.[181125]).toBe("3 of 3 hashboards reporting");
  });
});
