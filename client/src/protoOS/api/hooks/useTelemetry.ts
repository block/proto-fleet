import { useCallback, useMemo, useState } from "react";

import { type GetCurrentTelemetryParams, type TelemetryData } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { type AsicHardwareData, getAsicId, useMinerStore } from "@/protoOS/store";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { usePoll } from "@/shared/hooks/usePoll";

interface UseTelemetryProps {
  level?: GetCurrentTelemetryParams["level"];
  poll?: boolean;
  pollIntervalMs?: number;
  enabled?: boolean;
}

const useTelemetry = ({
  level = ["miner", "hashboard"],
  poll = true,
  pollIntervalMs = 15 * 1000,
  enabled = true,
}: UseTelemetryProps = {}) => {
  const { api } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();
  const [data, setData] = useState<TelemetryData>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(async () => {
    if (!api) {
      return;
    }

    setPending(true);
    setError(undefined);

    try {
      // TODO: Need to have type in MDK_API updated because level expects comma separated string
      // not ("miner" | "hashboard" | "psu" | "asic")[], casting as any to bypass for now
      const levelParam = level.join(",") as any;
      const response = await api.getCurrentTelemetry({ level: levelParam });
      setData(response.data);

      // Process successful response
      processHashboards(response.data);

      // Update the telemetry store with latest values
      useMinerStore.getState().telemetry.updateLatestTelemetry(response.data);
    } catch (err) {
      handleAuthErrors({
        error: err as any,
        onError: (e) => setError(e?.error?.message ?? (err instanceof Error ? err.message : "Unknown error occurred")),
      });
    } finally {
      setPending(false);
    }
  }, [api, level, handleAuthErrors]);

  // Helper function to process hashboard data
  const processHashboards = (data: TelemetryData) => {
    // Update hardware store with ASIC data from telemetry API
    // TODO: [STORE_REFACTOR] We shouldnt need to populate hardware data from telemetry api
    // ideally the useHardware hook would fetch and populate this data
    if (data?.hashboards) {
      // Collect all ASIC updates in a single array for batch processing
      const asicsToUpdate: Array<AsicHardwareData> = [];

      data.hashboards.forEach((hashboardData) => {
        if (hashboardData.asics && hashboardData.serial_number && hashboardData.index !== undefined) {
          const asicTelemetry = hashboardData.asics;

          // ASICs are returned as arrays of values, we need to iterate by index
          const numAsics = asicTelemetry.hashrate?.values?.length || asicTelemetry.temperature?.values?.length || 0;

          for (let asicIndex = 0; asicIndex < numAsics; asicIndex++) {
            const asicId = getAsicId(hashboardData.serial_number, asicIndex.toString());

            const existingAsic = useMinerStore.getState().hardware.getAsic(asicId);

            // Prepare ASIC data for batch update
            if (!existingAsic) {
              asicsToUpdate.push({
                id: asicId,
                hashboardSerial: hashboardData.serial_number,
                index: asicIndex,
                hashboardIndex: hashboardData.index,
                // row/column will be populated by useHashboardStatus
              });
            } else {
              // Update existing ASIC with index/hashboardIndex data
              asicsToUpdate.push({
                ...existingAsic,
                index: asicIndex,
                hashboardIndex: hashboardData.index,
              });
            }
          }
        }
      });

      // Batch update all ASICs in a single store mutation
      if (asicsToUpdate.length > 0) {
        useMinerStore.getState().hardware.batchAddAsics(asicsToUpdate);
      }
    }
  };

  usePoll({
    fetchData,
    enabled,
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

export { useTelemetry };
