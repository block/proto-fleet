import { create } from "@bufbuild/protobuf";
import { timestampMs } from "@bufbuild/protobuf/wkt";
import {
  type AccountDashboardReading,
  type MiningSummary,
  MiningSummarySchema,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import {
  AggregationType,
  MeasurementType,
  type Metric,
  MetricSchema,
  TemperatureStatusCountSchema,
  UptimeStatusCountSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

export const reportingCoverage = (count: bigint, summary: MiningSummary) => {
  const { minerCount, moduleCount, hashboardCount } = summary;
  let label = "asset";
  if (minerCount === 0n && hashboardCount === 0n && moduleCount > 0n) {
    label = "module";
  } else if (moduleCount === 0n && hashboardCount === 0n && minerCount > 0n) {
    label = "miner";
  } else if (hashboardCount > 0n && minerCount === 0n && moduleCount === 0n) {
    label = "hashboard";
  }
  return `${count.toLocaleString()} of ${summary.assetCount.toLocaleString()} ${label}${summary.assetCount === 1n ? "" : "s"} reporting`;
};

// Temperature and uptime chart status distributions, so only the averaged metrics become series.
export const accountDashboardData = (readings: AccountDashboardReading[]) => {
  const metrics: Metric[] = [];
  const tooltipNotes: Partial<Record<MeasurementType, Record<number, string>>> = {};
  for (const { time, summary = create(MiningSummarySchema) } of readings) {
    if (!time) continue;
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
    const timestamp = timestampMs(time);
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
      const coverage = reportingCoverage(reporting, summary);
      (tooltipNotes[measurementType] ??= {})[timestamp] =
        summary.moduleCount > 0n ? `Legacy module data · ${coverage}` : coverage;
    }
  }
  const latestSummary = readings[readings.length - 1]?.summary;
  return {
    metrics,
    tooltipNotes,
    hasLegacyModuleReadings: readings.some(({ summary }) => summary && summary.moduleCount > 0n),
    temperatureStatusCounts: readings.map(
      ({ time, temperatureStatus }) => temperatureStatus ?? create(TemperatureStatusCountSchema, { timestamp: time }),
    ),
    uptimeExpectedAssetCount: latestSummary ? Number(latestSummary.assetCount) : undefined,
    uptimeStatusCounts: readings.map(
      ({ time, uptimeStatus }) => uptimeStatus ?? create(UptimeStatusCountSchema, { timestamp: time }),
    ),
  };
};
