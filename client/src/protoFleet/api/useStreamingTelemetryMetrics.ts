import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { telemetryClient } from "@/protoFleet/api/clients";
import {
  AggregationType,
  DeviceListSchema,
  DeviceSelectorSchema,
  MeasurementType,
  StreamCombinedMetricUpdatesRequestSchema,
  StreamCombinedMetricUpdatesResponse,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

interface StreamingOptions {
  deviceIds: string[];
  measurementTypes?: MeasurementType[];
  aggregations?: AggregationType[];
  enabled?: boolean;
}

export const useStreamingTelemetryMetrics = (options: StreamingOptions) => {
  const { authTokens } = useAuthContext();
  const [latestData, setLatestData] =
    useState<StreamCombinedMetricUpdatesResponse | null>(null);
  const [isStreaming, setIsStreaming] = useState(false);
  const abortController = useRef<AbortController | null>(null);

  // Create stable options using useMemo with proper dependencies
  const stableOptions = useMemo(() => {
    const deviceIds = options.deviceIds || [];
    const measurementTypes = options.measurementTypes || [];
    const aggregations = options.aggregations || [];

    return {
      enabled: options.enabled,
      deviceIds,
      measurementTypes,
      aggregations,
    };
  }, [
    options.enabled,
    options.deviceIds,
    options.measurementTypes,
    options.aggregations,
  ]);

  const stopStreaming = useCallback(() => {
    if (abortController.current) {
      abortController.current.abort();
      abortController.current = null;
    }
    setIsStreaming(false);
  }, []);

  const startStreaming = useCallback(async () => {
    if (!stableOptions.enabled) return;

    abortController.current = new AbortController();
    setIsStreaming(true);

    let deviceSelector = create(DeviceSelectorSchema);
    if (stableOptions.deviceIds.length > 0) {
      deviceSelector.selectorValue.case = "deviceList";
      deviceSelector.selectorValue.value = create(DeviceListSchema, {
        deviceIds: stableOptions.deviceIds,
      });
    } else {
      deviceSelector.selectorValue.case = "allDevices";
      deviceSelector.selectorValue.value = true;
    }

    (async () => {
      try {
        const request = create(StreamCombinedMetricUpdatesRequestSchema, {
          deviceSelector,
          metrics: stableOptions.measurementTypes || [MeasurementType.HASHRATE],
          aggregations: stableOptions.aggregations || [AggregationType.AVERAGE],
          granularity: { seconds: BigInt(90), nanos: 0 }, // 1.5 minute
        });

        for await (const response of telemetryClient.streamCombinedMetricUpdates(
          request,
          {
            ...getAuthHeader(authTokens),
            signal: abortController.current?.signal,
          },
        )) {
          setLatestData(response);
        }
      } catch (error) {
        if (!abortController.current?.signal.aborted) {
          console.error("Error starting telemetry stream:", error);
        }
      } finally {
        setIsStreaming(false);
      }
    })();
  }, [stableOptions, authTokens]);

  // Start/stop streaming when options change
  useEffect(() => {
    if (stableOptions.enabled) {
      startStreaming();
    } else {
      stopStreaming();
    }

    return stopStreaming;
  }, [stableOptions, startStreaming, stopStreaming]);

  return { latestData, isStreaming };
};
