import { useCallback, useEffect, useRef, useState } from "react";
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
  pollIntervalMs?: number;
}

export const useTelemetryMetrics = (options: TelemetryMetricsOptions) => {
  const { handleAuthErrors } = useAuthErrors();
  const [data, setData] = useState<GetCombinedMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  // Reset when scope changes — invalidate in-flight requests so stale responses can't land
  const scopeKey = `${options.duration}-${options.deviceIds?.join(",") ?? "all"}`;
  const prevScopeRef = useRef(scopeKey);
  if (prevScopeRef.current !== scopeKey) {
    prevScopeRef.current = scopeKey;
    ++requestIdRef.current;
    hasLoadedRef.current = false;
    setHasLoaded(false);
    setData(null);
  }

  const fetchMetrics = useCallback(async () => {
    if (!options.enabled) {
      ++requestIdRef.current;
      setIsLoading(false);
      return;
    }

    const thisRequestId = ++requestIdRef.current;

    // Only show loading spinner on first fetch, not poll refreshes
    if (!hasLoadedRef.current) {
      setIsLoading(true);
    }
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

      // Discard stale responses
      if (thisRequestId !== requestIdRef.current) return;

      setData(response);
      hasLoadedRef.current = true;
      setHasLoaded(true);
    } catch (err) {
      if (thisRequestId !== requestIdRef.current) return;

      handleAuthErrors({
        error: err,
        onError: () => {
          const errorObj = err instanceof Error ? err : new Error(String(err));
          setError(errorObj);
          // Only clear data on first-load failure; preserve last snapshot during poll errors
          if (!hasLoadedRef.current) {
            setData(null);
          }
          console.error("Error fetching combined metrics:", errorObj);
        },
      });
    } finally {
      if (thisRequestId === requestIdRef.current) {
        setIsLoading(false);
      }
    }
  }, [
    options.deviceIds,
    options.measurementTypes,
    options.aggregations,
    options.duration,
    options.enabled,
    handleAuthErrors,
  ]);

  // Initial fetch + refetch on dependency change
  useEffect(() => {
    fetchMetrics();
  }, [fetchMetrics]);

  // Polling
  useEffect(() => {
    if (!options.pollIntervalMs || !options.enabled) return;

    const intervalId = setInterval(() => {
      void fetchMetrics();
    }, options.pollIntervalMs);

    return () => clearInterval(intervalId);
  }, [options.pollIntervalMs, options.enabled, fetchMetrics]);

  return { data, isLoading, hasLoaded, error, refetch: fetchMetrics };
};
