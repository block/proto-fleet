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
  useAuthErrors,
  useAuthHeader,
  useFleetStore,
  useSetTemperatureStatusCounts,
} from "@/protoFleet/store";

interface StreamingOptions {
  deviceIds: string[];
  measurementTypes?: MeasurementType[];
  aggregations?: AggregationType[];
  enabled?: boolean;
}

export const useStreamingTelemetryMetrics = (options: StreamingOptions) => {
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();
  const setTemperatureStatusCounts = useSetTemperatureStatusCounts();
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
          granularity: { seconds: BigInt(20), nanos: 0 }, // 20 seconds
        });

        for await (const response of telemetryClient.streamCombinedMetricUpdates(
          request,
          {
            ...authHeader,
            signal: abortController.current?.signal,
          },
        )) {
          setLatestData(response);

          // Update temperature status counts if present in the response
          if (
            response.temperatureStatusCounts &&
            response.temperatureStatusCounts.length > 0
          ) {
            // Get current temperature status counts from store
            const existingTemperatureStatusCounts =
              useFleetStore.getState().fleet.temperatureStatusCounts;

            // Merge new temperature status counts with existing ones
            const updatedCounts = [...existingTemperatureStatusCounts];

            // Add new temperature status counts from the streaming response
            for (const newCount of response.temperatureStatusCounts) {
              // Find if we already have a count for this timestamp
              const existingIndex = updatedCounts.findIndex(
                (count) =>
                  count.timestamp?.seconds === newCount.timestamp?.seconds,
              );

              if (existingIndex >= 0) {
                // Update existing count
                updatedCounts[existingIndex] = newCount;
              } else {
                // Add new count
                updatedCounts.push(newCount);
              }
            }

            // Sort by timestamp and keep a reasonable number of entries
            updatedCounts.sort((a, b) => {
              const timeA = a.timestamp?.seconds || 0n;
              const timeB = b.timestamp?.seconds || 0n;
              return timeA < timeB ? -1 : timeA > timeB ? 1 : 0;
            });

            // Keep only the last 1000 entries to prevent memory issues
            if (updatedCounts.length > 1000) {
              updatedCounts.splice(0, updatedCounts.length - 1000);
            }

            setTemperatureStatusCounts(updatedCounts);
          }
        }
      } catch (error) {
        if (!abortController.current?.signal.aborted) {
          handleAuthErrors({
            error: error,
            onError: (err) => {
              console.error("Error starting telemetry stream:", err);
            },
          });
        }
      } finally {
        setIsStreaming(false);
      }
    })();
  }, [stableOptions, authHeader, handleAuthErrors, setTemperatureStatusCounts]);

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
