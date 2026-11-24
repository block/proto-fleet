import { useCallback, useRef, useState } from "react";
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
import { usePoll } from "@/shared/hooks/usePoll";

interface TelemetryMetricsOptions {
  deviceIds?: string[];
  measurementTypes?: MeasurementType[];
  aggregations?: AggregationType[];
  duration: FleetDuration;
  enabled?: boolean;
  pollIntervalMs?: number;
  /** Scope metrics to specific sites (OR). Empty = all sites. */
  siteIds?: bigint[];
  /** Include metrics for devices with no site assignment. */
  includeUnassigned?: boolean;
}

export const useTelemetryMetrics = (options: TelemetryMetricsOptions) => {
  const { handleAuthErrors } = useAuthErrors();
  const [data, setData] = useState<GetCombinedMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const [revision, setRevision] = useState(0);
  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  // Reset when scope changes — invalidate in-flight requests so stale responses can't land
  const siteScopeKey = `${options.siteIds?.map(String).join(",") ?? ""}|${options.includeUnassigned ?? false}`;
  const scopeKey = JSON.stringify([
    options.duration,
    options.deviceIds ?? null,
    siteScopeKey,
    options.measurementTypes ?? [MeasurementType.HASHRATE],
    options.aggregations ?? [AggregationType.AVERAGE],
  ]);
  const prevScopeRef = useRef(scopeKey);
  if (prevScopeRef.current !== scopeKey) {
    prevScopeRef.current = scopeKey;
    ++requestIdRef.current;
    hasLoadedRef.current = false;
    setHasLoaded(false);
    setData(null);
  }

  const fetchMetrics = useCallback(
    async (signal: AbortSignal) => {
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
          siteIds: options.siteIds ?? [],
          includeUnassigned: options.includeUnassigned ?? false,
        });

        const response = await telemetryClient.getCombinedMetrics(request, { signal });

        // Discard stale responses
        if (signal.aborted || thisRequestId !== requestIdRef.current) return;

        setData(response);
        hasLoadedRef.current = true;
        setHasLoaded(true);
      } catch (err) {
        if (signal.aborted || thisRequestId !== requestIdRef.current) return;

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
        if (!signal.aborted && thisRequestId === requestIdRef.current) {
          setIsLoading(false);
        }
      }
    },
    [
      options.deviceIds,
      options.measurementTypes,
      options.aggregations,
      options.duration,
      options.enabled,
      options.siteIds,
      options.includeUnassigned,
      handleAuthErrors,
    ],
  );

  usePoll({
    fetchData: fetchMetrics,
    params: `${scopeKey}:${revision}`,
    enabled: !!options.enabled,
    poll: !!options.pollIntervalMs,
    pollIntervalMs: options.pollIntervalMs,
  });
  const refetch = useCallback(() => setRevision((value) => value + 1), []);
  return { data, isLoading: !!options.enabled && isLoading, hasLoaded, error, refetch };
};
