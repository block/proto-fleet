import { useCallback, useEffect, useMemo, useState } from "react";

import { type TimeSeriesRequest, type TimeSeriesResponse } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { getAsicId, useMinerStore } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { type Duration, getDurationMs } from "@/shared/components/DurationSelector";
import { usePoll } from "@/shared/hooks/usePoll";

/**
 * Get appropriate data interval based on duration
 */
function getIntervalMinutes(duration: Duration): number {
  switch (duration) {
    case "1h":
      return 1;
    case "12h":
      return 5;
    case "24h":
      return 15;
    case "48h":
      return 30;
    case "5d":
      return 60;
    default:
      return 15;
  }
}

interface UseTimeSeriesProps {
  duration: Duration;
  levels: TimeSeriesRequest["levels"];
  poll?: boolean;
  pollIntervalMs?: number;
}

const useTimeSeries = ({ duration, levels, poll = true, pollIntervalMs = 30 * 1000 }: UseTimeSeriesProps) => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<TimeSeriesResponse>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  // Get hashboard count from the hardware slice to trigger telemetry fetch
  // Using count instead of keys array to prevent infinite re-renders from array recreation
  const hashboardCount = useMinerStore((state) => state.hardware.hashboards.size);

  const fetchData = useCallback(async () => {
    if (!api) {
      return;
    }

    const currentHashboards = Array.from(useMinerStore.getState().hardware.hashboards.keys());

    if (currentHashboards.length === 0) {
      return;
    }

    setPending(true);
    setError(undefined);

    // Capture the current duration at the start of the request
    const requestDuration = duration;

    // Calculate time window on every fetch
    const timeRangeMs = getDurationMs(duration);
    const intervalMinutes = getIntervalMinutes(duration);
    const startTime = new Date(Date.now() - timeRangeMs);

    const request: TimeSeriesRequest = {
      start_time: startTime.toISOString(),
      duration: `PT${Math.floor(timeRangeMs / (60 * 1000))}M`,
      interval: `PT${intervalMinutes}M`,
      levels,
      aggregation: "mean",
    };

    await authRetry({
      request: (params) => api.getTimeSeries(request, params),
      onSuccess: (response) => {
        // Only update if the duration hasn't changed since we started the request
        const currentDuration = useMinerStore.getState().ui.duration;
        if (requestDuration !== currentDuration) {
          return;
        }

        setData(response.data);

        // Update hardware store with ASIC index/hashboardIndex data from time series API
        // TODO: [STORE_REFACTOR] We shouldnt need to populate hardware data from timeseries api
        // ideally the useHardware hook would fetch and populate this data
        if (response.data?.data?.asics) {
          response.data.data.asics.forEach((asicData) => {
            if (asicData.index !== undefined && asicData.hashboard_index !== undefined) {
              const hashboardSerialNumber = response.data?.data?.hashboards?.find(
                (hb) => hb.index === asicData.hashboard_index,
              )?.serial_number;

              if (hashboardSerialNumber) {
                const asicId = getAsicId(hashboardSerialNumber, asicData.index.toString());

                // Update existing ASIC with index/hashboardIndex data
                const existingAsic = useMinerStore.getState().hardware.getAsic(asicId);

                if (existingAsic) {
                  useMinerStore.getState().hardware.addAsic({
                    ...existingAsic,
                    index: asicData.index,
                    hashboardIndex: asicData.hashboard_index,
                  });
                }
              }
            }
          });
        }

        // Update the telemetry store with the new data
        useMinerStore.getState().telemetry.updateTimeSeriesTelemetry(response.data);
      },
      onError: (err) => {
        setError(err?.error?.message ?? "Unknown error occurred");
      },
    }).finally(() => {
      setPending(false);
    });
  }, [duration, levels, api, authRetry]);

  // Trigger fetch when hashboards change or when fetchData dependencies change (duration, levels, api)
  useEffect(() => {
    if (hashboardCount > 0) {
      fetchData();
    }
  }, [hashboardCount, fetchData]);

  // Memoize params to prevent recreating object on every render
  const pollParams = useMemo(() => ({ duration, levels }), [duration, levels]);

  usePoll({
    fetchData,
    params: pollParams,
    poll,
    pollIntervalMs,
  });

  return useMemo(
    () => ({
      pending,
      error,
      data,
    }),
    [pending, error, data],
  );
};

export { useTimeSeries };
