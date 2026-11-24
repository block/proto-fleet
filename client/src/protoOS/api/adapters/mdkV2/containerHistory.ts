import {
  containerHistoryKey,
  type ContainerHistoryMetric,
  type ContainerHistoryRequest,
  type ContainerHistoryResult,
} from "../containerHistory";
import type { AdapterRequestOptions } from "../types";
import { mapWithConcurrency } from "../utils/async";
import type { TelemetryServiceQueryTimeSeriesParams, V2QueryTimeSeriesResponse } from "./generated/generatedApi";
import type { MdkV2Client } from "./MdkV2Adapter";
import type { MetricTimeSeries, MetricUnit } from "@/protoOS/store/contract";
import { isAbortError } from "@/shared/utils/abort";

const metrics: Record<
  ContainerHistoryMetric,
  { wire: TelemetryServiceQueryTimeSeriesParams["metric"]; units: MetricUnit }
> = {
  hashrate: { wire: "TIME_SERIES_METRIC_HASHRATE", units: "TH/s" },
  power: { wire: "TIME_SERIES_METRIC_POWER", units: "W" },
  temperature: { wire: "TIME_SERIES_METRIC_TEMPERATURE", units: "°C" },
  plantPower: { wire: "TIME_SERIES_METRIC_PLANT_POWER", units: "W" },
  facilityPower: { wire: "TIME_SERIES_METRIC_PLANT_FACILITY_POWER", units: "W" },
  heatRejectionPower: { wire: "TIME_SERIES_METRIC_PLANT_HEAT_REJECTION_POWER", units: "W" },
  insideTemperature: { wire: "TIME_SERIES_METRIC_PLANT_INSIDE_AMBIENT_TEMPERATURE", units: "°C" },
  outsideTemperature: { wire: "TIME_SERIES_METRIC_PLANT_OUTSIDE_AMBIENT_TEMPERATURE", units: "°C" },
  inletTemperature: { wire: "TIME_SERIES_METRIC_PLANT_INLET_TEMPERATURE", units: "°C" },
  outletTemperature: { wire: "TIME_SERIES_METRIC_PLANT_OUTLET_TEMPERATURE", units: "°C" },
  outletATemperature: { wire: "TIME_SERIES_METRIC_PLANT_OUTLET_A_TEMPERATURE", units: "°C" },
  outletBTemperature: { wire: "TIME_SERIES_METRIC_PLANT_OUTLET_B_TEMPERATURE", units: "°C" },
  distributionPower: { wire: "TIME_SERIES_METRIC_PLANT_DISTRIBUTION_POWER", units: "W" },
  fanPower: { wire: "TIME_SERIES_METRIC_PLANT_FAN_POWER", units: "W" },
};

const bucketSizes: Record<number, TelemetryServiceQueryTimeSeriesParams["bucketSize"]> = {
  60_000: "BUCKET_SIZE_1M",
  300_000: "BUCKET_SIZE_5M",
  900_000: "BUCKET_SIZE_15M",
  3_600_000: "BUCKET_SIZE_1H",
};

/** Canonical units and explicit missing buckets, retaining firmware min/max/count. */
export function normalizeContainerHistory(
  wire: V2QueryTimeSeriesResponse,
  metric: ContainerHistoryMetric,
  intervalMs: number,
): MetricTimeSeries {
  const units = metrics[metric].units;
  const inputUnit = wire.unit?.trim().toLowerCase().replace(/\s/g, "");
  const conversions: Record<string, number> =
    units === "W"
      ? { w: 1, kw: 1000 }
      : units === "TH/s"
        ? { "th/s": 1, "gh/s": 0.001, "h/s": 1e-12 }
        : { c: 1, "°c": 1, celsius: 1 };
  const factor = inputUnit ? conversions[inputUnit] : 1;
  if (factor === undefined) throw new Error("Unsupported history units");
  const finite = (value: number | undefined) => (value !== undefined && Number.isFinite(value) ? value * factor : null);
  const points = (wire.buckets ?? [])
    .flatMap((bucket) => {
      const timestampMs = Date.parse(bucket.time ?? "");
      if (!Number.isFinite(timestampMs)) return [];
      const count = bucket.count === undefined ? undefined : Number(bucket.count);
      return [
        {
          timestampMs,
          value: count === 0 ? null : finite(bucket.avg),
          min: count === 0 ? null : finite(bucket.min),
          max: count === 0 ? null : finite(bucket.max),
          sampleCount: count !== undefined && Number.isFinite(count) && count >= 0 ? count : undefined,
        },
      ];
    })
    .sort((a, b) => a.timestampMs - b.timestampMs);
  const values = points.filter((point) => point.value !== null);
  const minimums = points.flatMap((point) => (point.min === null ? [] : [point.min]));
  const maximums = points.flatMap((point) => (point.max === null ? [] : [point.max]));
  // Count-weighted means only when counts cover every nonempty bucket. Equal
  // duration buckets are the fallback; never mix weighted and unweighted terms.
  const hasCounts =
    values.length > 0 && values.every((point) => point.sampleCount !== undefined && point.sampleCount > 0);
  const denominator = values.reduce((total, point) => total + (hasCounts ? point.sampleCount! : 1), 0);
  const average = denominator
    ? values.reduce((total, point) => total + point.value! * (hasCounts ? point.sampleCount! : 1), 0) / denominator
    : null;
  return {
    units,
    bucketSizeMs: intervalMs,
    points,
    aggregates: {
      min: { value: minimums.length ? Math.min(...minimums) : null, units },
      max: { value: maximums.length ? Math.max(...maximums) : null, units },
      avg: { value: average, units },
    },
  };
}

export async function queryContainerHistory(
  client: MdkV2Client,
  request: ContainerHistoryRequest,
  params: AdapterRequestOptions,
): Promise<ContainerHistoryResult> {
  const start = Date.parse(request.startTime),
    end = Date.parse(request.endTime);
  const bucketSize = bucketSizes[request.intervalMs];
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || !bucketSize)
    throw new Error("Invalid container history range or interval");
  const selections = [
    ...new Map(request.selections.map((selection) => [containerHistoryKey(selection), selection])).values(),
  ];
  for (const selection of selections) {
    if (
      !metrics[selection.metric] ||
      ((selection.metric === "distributionPower" || selection.metric === "fanPower") && !selection.identifier?.trim())
    )
      throw new Error("Container component history requires a metric and identifier");
  }
  const results = await mapWithConcurrency(
    selections,
    6,
    async (selection) => {
      const key = containerHistoryKey(selection);
      try {
        const response = await client.telemetryServiceQueryTimeSeries(
          {
            metric: metrics[selection.metric].wire,
            identifier: selection.identifier,
            "range.start": request.startTime,
            "range.end": request.endTime,
            bucketSize,
          },
          params,
        );
        return { key, series: normalizeContainerHistory(response.data, selection.metric, request.intervalMs) };
      } catch (error) {
        const status = (error as { status?: number })?.status;
        if (params.signal?.aborted || isAbortError(error) || status === 401 || status === 403) throw error;
        return { key, series: undefined };
      }
    },
    params.signal ?? undefined,
  );
  return {
    series: Object.fromEntries(results.flatMap(({ key, series }) => (series ? [[key, series]] : []))),
    unavailable: results.filter((result) => !result.series).map((result) => result.key),
  };
}
