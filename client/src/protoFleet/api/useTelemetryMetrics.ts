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
import { Duration } from "@/shared/components/DurationSelector";

const DEFAULT_GRANULARITY_SECONDS = 90;
const GRANULARITY_48H_SECONDS = 180; // 3 minutes
const GRANULARITY_5D_SECONDS = 600; // 10 minutes

const HOURS_48_IN_SECONDS = 48 * 3600;
const DAYS_5_IN_SECONDS = 5 * 24 * 3600;

interface TelemetryMetricsOptions {
  deviceIds?: string[];
  measurementTypes?: MeasurementType[];
  aggregations?: AggregationType[];
  duration: Duration;
  enabled?: boolean;
}

/**
 * Convert duration string to seconds
 */
const durationToSeconds = (duration: Duration): number | undefined => {
  const value = parseInt(duration.slice(0, -1));
  const unit = duration.slice(-1);

  switch (unit) {
    case "h":
      return value * 3600;
    case "d":
      return value * 24 * 3600;
    default:
      return undefined;
  }
};

/**
 * Calculate optimal granularity based on duration to stay within backend LIMIT
 * Backend has LIMIT of 1000 buckets, so we adjust granularity for longer durations
 */
const getGranularityForDuration = (duration: Duration): number => {
  const totalSeconds = durationToSeconds(duration);
  if (totalSeconds === undefined) return DEFAULT_GRANULARITY_SECONDS;

  // Round to reasonable intervals to stay within backend bucket limit (1000)
  if (totalSeconds >= DAYS_5_IN_SECONDS) return GRANULARITY_5D_SECONDS; // 5d -> 10 min
  if (totalSeconds >= HOURS_48_IN_SECONDS) return GRANULARITY_48H_SECONDS; // 48h -> 3 min
  return DEFAULT_GRANULARITY_SECONDS; // Default for shorter durations
};

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
        aggregations: options.aggregations || [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX],
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
