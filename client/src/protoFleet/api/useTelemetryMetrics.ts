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
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";
import { Duration } from "@/shared/components/DurationSelector";

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

export const useTelemetryMetrics = (options: TelemetryMetricsOptions) => {
  const { authTokens } = useAuthContext();
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
        measurementTypes: options.measurementTypes || [
          MeasurementType.HASHRATE,
        ],
        aggregations: options.aggregations || [
          AggregationType.AVERAGE,
          AggregationType.MIN,
          AggregationType.MAX,
        ],
        granularity: { seconds: BigInt(60), nanos: 0 }, // 1 minute granularity
        startTime: {
          seconds: BigInt(Math.floor(startTime.getTime() / 1000)),
          nanos: 0,
        },
        endTime: {
          seconds: BigInt(Math.floor(now.getTime() / 1000)),
          nanos: 0,
        },
        pageSize: 1000,
        pageToken: "",
      });

      const response = await telemetryClient.getCombinedMetrics(
        request,
        getAuthHeader(authTokens),
      );

      setData(response);
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      setError(error);
      console.error("Error fetching combined metrics:", error);
    } finally {
      setIsLoading(false);
    }
  }, [
    options.deviceIds,
    options.measurementTypes,
    options.aggregations,
    options.duration,
    options.enabled,
    authTokens,
  ]);

  useEffect(() => {
    fetchMetrics();
  }, [fetchMetrics]);

  return { data, isLoading, error, refetch: fetchMetrics };
};
