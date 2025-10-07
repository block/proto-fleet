import { useCallback, useMemo, useState } from "react";

import { usePoll } from "./usePoll";
import { type TelemetryData } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { getAsicId, useMinerStore } from "@/protoOS/store";

interface UseTelemetryProps {
  level?: "miner" | "hashboard" | "asic";
  poll?: boolean;
  pollIntervalMs?: number;
}

const useTelemetry = ({
  level = "hashboard",
  poll = true,
  pollIntervalMs = 15 * 1000,
}: UseTelemetryProps = {}) => {
  const { api } = useMinerHosting();
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
      const response = await api.getCurrentTelemetry({ level });
      setData(response.data);

      // Process successful response
      processHashboards(response.data);

      // Update the telemetry store with latest values
      useMinerStore.getState().telemetry.updateLatestTelemetry(response.data);
    } catch (err) {
      const errorMessage =
        err instanceof Error ? err.message : "Unknown error occurred";
      setError(errorMessage);
    } finally {
      setPending(false);
    }
  }, [api, level]);

  // Helper function to process hashboard data
  const processHashboards = (data: TelemetryData) => {
    // Update hardware store with ASIC data from telemetry API
    // TODO: [STORE_REFACTOR] We shouldnt need to populate hardware data from telemetry api
    // ideally the useHardware hook would fetch and populate this data
    if (data?.hashboards) {
      data.hashboards.forEach((hashboardData) => {
        if (
          hashboardData.asics &&
          hashboardData.serial_number &&
          hashboardData.index !== undefined
        ) {
          const asicTelemetry = hashboardData.asics;

          // ASICs are returned as arrays of values, we need to iterate by index
          const numAsics =
            asicTelemetry.hashrate?.values?.length ||
            asicTelemetry.temperature?.values?.length ||
            0;

          for (let asicIndex = 0; asicIndex < numAsics; asicIndex++) {
            const asicId = getAsicId(
              hashboardData.serial_number,
              asicIndex.toString(),
            );

            const existingAsic = useMinerStore
              .getState()
              .hardware.getAsic(asicId);

            // Only add if it doesn't exist yet
            if (!existingAsic) {
              useMinerStore.getState().hardware.addAsic({
                id: asicId,
                hashboardSerial: hashboardData.serial_number,
                index: asicIndex,
                hashboardIndex: hashboardData.index,
                // row/column will be populated by useHashboardStatus
              });
            } else {
              // Update existing ASIC with index/hashboardIndex data
              useMinerStore.getState().hardware.addAsic({
                ...existingAsic,
                index: asicIndex,
                hashboardIndex: hashboardData.index,
              });
            }
          }
        }
      });
    }
  };

  usePoll({
    fetchData,
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
