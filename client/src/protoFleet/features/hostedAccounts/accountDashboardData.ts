import { create } from "@bufbuild/protobuf";
import type { AccountDashboardReading, MiningSummary } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import {
  AggregationType,
  MeasurementType,
  type Metric,
  MetricSchema,
  TemperatureStatusCountSchema,
  UptimeStatusCountSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import type { FleetDuration } from "@/shared/components/DurationSelector";

export const reportingCoverage = (count: bigint, summary: MiningSummary) => {
  const label =
    summary.minerCount === 0n && summary.moduleCount > 0n
      ? "module"
      : summary.moduleCount === 0n && summary.minerCount > 0n
        ? "miner"
        : "asset";
  return `${count.toLocaleString()} of ${summary.assetCount.toLocaleString()} ${label}${summary.assetCount === 1n ? "" : "s"} reporting`;
};

// Temperature and uptime chart status distributions, so only the averaged metrics become series.
export const accountDashboardData = (readings: AccountDashboardReading[], duration: FleetDuration) => {
  const metrics: Metric[] = [];
  const tooltipNotes: Partial<Record<MeasurementType, Record<number, string>>> = {};
  const granularityMs = getGranularityForDuration(duration) * 1000;
  for (const { time, summary } of readings) {
    if (!time || !summary) continue;
    const values: [MeasurementType, number | undefined, bigint][] = [
      [
        MeasurementType.HASHRATE,
        summary.hashrateHs === undefined ? undefined : summary.hashrateHs / 1e12,
        summary.hashrateReportingCount,
      ],
      [
        MeasurementType.POWER,
        summary.powerW === undefined ? undefined : summary.powerW / 1000,
        summary.powerReportingCount,
      ],
      [MeasurementType.EFFICIENCY, summary.efficiencyJTh, summary.pairedReportingCount],
    ];
    const timestamp = Math.floor((Number(time.seconds) * 1000) / granularityMs) * granularityMs;
    for (const [measurementType, value, reporting] of values) {
      metrics.push(
        create(MetricSchema, {
          measurementType,
          openTime: time,
          deviceCount: Number(reporting),
          aggregatedValues:
            value !== undefined && Number.isFinite(value) ? [{ aggregationType: AggregationType.AVERAGE, value }] : [],
        }),
      );
      (tooltipNotes[measurementType] ??= {})[timestamp] = reportingCoverage(reporting, summary);
    }
  }
  const latestSummary = readings[readings.length - 1]?.summary;
  return {
    metrics,
    tooltipNotes,
    temperatureStatusCounts: readings.map(
      ({ time, temperatureStatus }) => temperatureStatus ?? create(TemperatureStatusCountSchema, { timestamp: time }),
    ),
    uptimeExpectedAssetCount: latestSummary ? Number(latestSummary.assetCount) : undefined,
    uptimeStatusCounts: readings.map(
      ({ time, uptimeStatus }) => uptimeStatus ?? create(UptimeStatusCountSchema, { timestamp: time }),
    ),
  };
};
