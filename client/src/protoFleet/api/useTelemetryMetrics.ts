import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { telemetryClient } from "@/protoFleet/api/clients";
import {
  AggregationType,
  DeviceListSchema,
  DeviceSelectorSchema,
  GetCombinedMetricsRequestSchema,
  GetCombinedMetricsResponse,
  MeasurementType,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useAuthErrors } from "@/protoFleet/store";
import { FleetDuration } from "@/shared/components/DurationSelector";

const DEFAULT_GRANULARITY_SECONDS = 90;
const GRANULARITY_48H_SECONDS = 180; // 3 minutes
const GRANULARITY_5D_SECONDS = 600; // 10 minutes
const GRANULARITY_14D_SECONDS = 1260; // 21 minutes (~960 buckets for 14d)
const GRANULARITY_30D_SECONDS = 2700; // 45 minutes (~960 buckets for 30d)
const GRANULARITY_90D_SECONDS = 8100; // 2.25 hours (~960 buckets for 90d)
const GRANULARITY_1Y_SECONDS = 32850; // ~9 hours (~960 buckets for 1y)

const HOURS_48_IN_SECONDS = 48 * 3600;
const DAYS_5_IN_SECONDS = 5 * 24 * 3600;
const DAYS_14_IN_SECONDS = 14 * 24 * 3600;
const DAYS_30_IN_SECONDS = 30 * 24 * 3600;
const DAYS_90_IN_SECONDS = 90 * 24 * 3600;
const DAYS_365_IN_SECONDS = 365 * 24 * 3600;

interface TelemetryMetricsOptions {
  deviceIds?: string[];
  measurementTypes?: MeasurementType[];
  aggregations?: AggregationType[];
  duration: FleetDuration;
  enabled?: boolean;
}

/**
 * Convert duration string to seconds
 */
const durationToSeconds = (duration: FleetDuration): number | undefined => {
  const value = parseInt(duration.slice(0, -1));
  const unit = duration.slice(-1);

  switch (unit) {
    case "h":
      return value * 3600;
    case "d":
      return value * 24 * 3600;
    case "y":
      return value * 365 * 24 * 3600;
    default:
      return undefined;
  }
};

/**
 * Calculate optimal granularity based on duration to stay within backend LIMIT.
 * Backend has LIMIT of 1000 buckets, so we adjust granularity for longer durations.
 *
 * Note: These thresholds are intentionally different from backend data source selection
 * (raw ≤24h, hourly 24h-10d, daily >10d). The backend data source determines WHICH table
 * to query for performance, while this granularity controls HOW MANY buckets to return.
 * The backend aggregates its chosen data source to match this requested granularity.
 */
const getGranularityForDuration = (duration: FleetDuration): number => {
  const totalSeconds = durationToSeconds(duration);
  if (totalSeconds === undefined) return DEFAULT_GRANULARITY_SECONDS;

  // Granularity thresholds ensure ~960 buckets max for chart rendering performance
  if (totalSeconds >= DAYS_365_IN_SECONDS) return GRANULARITY_1Y_SECONDS; // 1y -> ~9 hours
  if (totalSeconds >= DAYS_90_IN_SECONDS) return GRANULARITY_90D_SECONDS; // 90d -> 2.25 hours
  if (totalSeconds >= DAYS_30_IN_SECONDS) return GRANULARITY_30D_SECONDS; // 30d -> 45 min
  // Note: No "14d" duration option exists, but this threshold affects 10d queries (10d < 14d, so uses 10min)
  if (totalSeconds >= DAYS_14_IN_SECONDS) return GRANULARITY_14D_SECONDS; // 14d+ -> 21 min
  if (totalSeconds >= DAYS_5_IN_SECONDS) return GRANULARITY_5D_SECONDS; // 5d -> 10 min
  if (totalSeconds >= HOURS_48_IN_SECONDS) return GRANULARITY_48H_SECONDS; // 48h -> 3 min
  return DEFAULT_GRANULARITY_SECONDS; // Default for shorter durations
};

// Export for use by chartDataPadding
export { getGranularityForDuration, durationToSeconds };

export const useTelemetryMetrics = (options: TelemetryMetricsOptions) => {
  const { handleAuthErrors } = useAuthErrors();
  const [data, setData] = useState<GetCombinedMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const fetchMetrics = useCallback(async () => {
    if (!options.enabled) return;

    setIsLoading(true);
    setError(null);

    try {
      const now = new Date();
      let durationSeconds = durationToSeconds(options.duration);
      if (durationSeconds === undefined) {
        durationSeconds = 3600; // Default to 1 hour if duration is invalid
      }
      const startTime = new Date(now.getTime() - durationSeconds * 1000);

      const request = create(GetCombinedMetricsRequestSchema, {
        deviceSelector: options.deviceIds?.length
          ? create(DeviceSelectorSchema, {
              selectorValue: {
                case: "deviceList",
                value: create(DeviceListSchema, {
                  deviceIds: options.deviceIds,
                }),
              },
            })
          : create(DeviceSelectorSchema, {
              selectorValue: { case: "allDevices", value: true },
            }),
        measurementTypes: options.measurementTypes || [MeasurementType.HASHRATE],
        aggregations: options.aggregations || [AggregationType.AVERAGE],
        granularity: { seconds: BigInt(getGranularityForDuration(options.duration)), nanos: 0 },
        startTime: {
          seconds: BigInt(Math.floor(startTime.getTime() / 1000)),
          nanos: 0,
        },
        endTime: {
          seconds: BigInt(Math.floor(now.getTime() / 1000)),
          nanos: 0,
        },
        pageSize: 10000,
        pageToken: "",
      });

      const response = await telemetryClient.getCombinedMetrics(request);

      setData(response);
    } catch (err) {
      handleAuthErrors({
        error: err,
        onError: () => {
          const errorObj = err instanceof Error ? err : new Error(String(err));
          setError(errorObj);
          setData(null); // Clear old data when error occurs
          console.error("Error fetching combined metrics:", errorObj);
        },
      });
    } finally {
      setIsLoading(false);
    }
  }, [
    options.deviceIds,
    options.measurementTypes,
    options.aggregations,
    options.duration,
    options.enabled,
    handleAuthErrors,
  ]);

  useEffect(() => {
    fetchMetrics();
  }, [fetchMetrics]);

  return { data, isLoading, error, refetch: fetchMetrics };
};
