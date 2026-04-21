import { useCallback, useEffect, useMemo, useState } from "react";

import { HashboardStatsHashboardstats } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AsicHardwareData, getAsicId } from "@/protoOS/store";
import { useMinerStore } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoll } from "@/shared/hooks/usePoll";
interface UseHashboardStatusProps {
  hashboardSerialNumbers: string[];
  poll?: boolean;
}

// TODO: [STORE_REFACTOR] We only use this hook to fill in gaps that our useHardware doesnt currently provide
// - hashboard.asicIds
// - asic rows and columns
const useHashboardStatus = ({ hashboardSerialNumbers, poll }: UseHashboardStatusProps) => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<Record<string, HashboardStatsHashboardstats>>({});
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const fetchData = useCallback(() => {
    if (!api || hashboardSerialNumbers.length === 0) return;

    setPending(true);
    setError(undefined);

    return authRetry({
      request: (params) =>
        Promise.all(
          hashboardSerialNumbers.map(async (serial) => {
            const res = await api.getHashboardStatus({ hbSn: serial }, params);
            return { serial, data: res?.data["hashboard-stats"] };
          }),
        ),
      onSuccess: (results) => {
        const newData: Record<string, HashboardStatsHashboardstats> = {};
        results.forEach(({ serial, data }) => {
          if (data) {
            newData[serial] = data;
          }
        });
        setData(newData);
      },
      onError: (err) => setError(err?.error?.message ?? "Unknown error occurred"),
    }).finally(() => {
      setPending(false);
    });
  }, [hashboardSerialNumbers, api, authRetry]);

  usePoll({
    fetchData,
    params: hashboardSerialNumbers,
    poll,
  });

  useEffect(() => {
    if (Object.keys(data).length === 0) return;

    // Collect all ASICs to add in a single batch
    const asicsToAdd: Array<AsicHardwareData> = [];

    // Process each hashboard's data
    Object.entries(data).forEach(([hashboardSerialNumber, hashboardData]) => {
      const asics = hashboardData?.asics;
      if (!asics || asics.length === 0) {
        return;
      }

      // Initialize hardware store with hashboard and ASIC structure
      const existingHashboard = useMinerStore.getState().hardware.getHashboard(hashboardSerialNumber);

      const asicIds = asics
        .filter((asic) => asic?.index !== undefined)
        .map((asic) => getAsicId(hashboardSerialNumber, asic.index!));

      if (!existingHashboard) {
        useMinerStore.getState().hardware.addHashboard({
          serial: hashboardSerialNumber,
          asicIds,
        });
      } else {
        // Update existing hashboard with asicIds
        useMinerStore.getState().hardware.addHashboard({
          ...existingHashboard,
          asicIds,
        });
      }

      // Collect ASIC info with positional data for batch processing
      for (const asic of asics) {
        if (asic !== undefined && asic.index !== undefined && asic.row !== undefined && asic.column !== undefined) {
          // Create globally unique ASIC ID using consistent utility
          const asicId = getAsicId(hashboardSerialNumber, asic.index);
          const existingAsic = useMinerStore.getState().hardware.getAsic(asicId);

          if (!existingAsic) {
            const asicInfo = {
              id: asicId,
              hashboardSerial: hashboardSerialNumber,
              row: asic.row,
              column: asic.column,
            };

            asicsToAdd.push(asicInfo);
            useMinerStore.getState().hardware.linkAsicToHashboard(asicId, hashboardSerialNumber);
          }

          // Update telemetry store with voltage and frequency data using Immer-safe setState
          useMinerStore.setState((state) => {
            let asicTelemetry = state.telemetry.asics.get(asicId);

            if (!asicTelemetry) {
              asicTelemetry = { id: asicId };
              state.telemetry.asics.set(asicId, asicTelemetry);
            }

            // Update voltage (voltage_mv from API)
            if (asic.voltage_mv !== undefined && asic.voltage_mv !== null) {
              if (!asicTelemetry.voltage) {
                asicTelemetry.voltage = {};
              }
              asicTelemetry.voltage.latest = {
                value: asic.voltage_mv,
                units: "mV",
              };
            }

            // Update frequency (freq_mhz from API)
            if (asic.freq_mhz !== undefined && asic.freq_mhz !== null) {
              if (!asicTelemetry.frequency) {
                asicTelemetry.frequency = {};
              }
              asicTelemetry.frequency.latest = {
                value: asic.freq_mhz,
                units: "MHz",
              };
            }
          });
        }
      }
    });

    // Batch add all ASICs in a single store mutation
    if (asicsToAdd.length > 0) {
      useMinerStore.getState().hardware.batchAddAsics(asicsToAdd);
    }
  }, [data]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardStatus };
