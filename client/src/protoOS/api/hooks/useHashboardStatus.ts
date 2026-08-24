import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { HashboardStatsHashboardstats } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AsicHardwareData, getAsicId } from "@/protoOS/store";
import { useMinerStore } from "@/protoOS/store";
import { asAsicId } from "@/protoOS/store/contract";
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
  // Staleness guard: if hashboardSerialNumbers changes mid-fetch, an older
  // Promise.all resolving after a newer one must not overwrite fresher data.
  const fetchIdRef = useRef(0);
  const fetchData = useCallback(() => {
    if (!api || hashboardSerialNumbers.length === 0) return;

    setPending(true);
    setError(undefined);
    const fetchId = ++fetchIdRef.current;

    return authRetry({
      request: (params) =>
        Promise.all(
          hashboardSerialNumbers.map(async (serial) => {
            const res = await api.getHashboardStatus({ hbSn: serial }, params);
            return { serial, data: res?.data["hashboard-stats"] };
          }),
        ),
      onSuccess: (results) => {
        if (fetchId !== fetchIdRef.current) return;
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

          // Always merge row/column onto the entry: useTelemetry may have created an
          // entry without positional data, and skipping the update here leaves the
          // ASIC table unable to render rows.
          asicsToAdd.push({
            ...existingAsic,
            id: asicId,
            hashboardSerial: hashboardSerialNumber,
            index: asic.index,
            row: asic.row,
            column: asic.column,
          });

          if (!existingAsic) {
            useMinerStore.getState().hardware.linkAsicToHashboard(asicId, hashboardSerialNumber);
          }

          // Update telemetry store with voltage and frequency data using Immer-safe setState
          useMinerStore.setState((state) => {
            const normalizedAsicId = asAsicId(asicId);
            let asicTelemetry = state.telemetry.asics.get(normalizedAsicId);

            if (!asicTelemetry) {
              asicTelemetry = { asicId: normalizedAsicId };
              state.telemetry.asics.set(normalizedAsicId, asicTelemetry);
            }

            // Update voltage (voltage_mv from API)
            if (asic.voltage_mv !== undefined && asic.voltage_mv !== null) {
              if (!asicTelemetry.voltage) {
                asicTelemetry.voltage = {};
              }
              asicTelemetry.voltage.latest = {
                value: asic.voltage_mv / 1000,
                units: "V",
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
