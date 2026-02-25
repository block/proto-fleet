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
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { useAuthErrors } from "@/protoFleet/store";
import { type FleetDuration, getFleetDurationMs } from "@/shared/components/DurationSelector";

interface TelemetryMetricsOptions {
  deviceIds?: string[];
  measurementTypes?: MeasurementType[];
  aggregations?: AggregationType[];
  duration: FleetDuration;
  enabled?: boolean;
}

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
      const durationMs = getFleetDurationMs(options.duration);
      const startTime = new Date(now.getTime() - durationMs);

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
