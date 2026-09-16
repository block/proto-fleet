import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { HashboardStatus } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AsicHardwareData, HashboardHardwareData } from "@/protoOS/store";
import { useMinerStore } from "@/protoOS/store";
import { asAsicId } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { getHashboardId } from "@/protoOS/store/utils/entityIds";
import { usePoll } from "@/shared/hooks/usePoll";
interface UseHashboardStatusProps {
  hashboardSerialNumbers: string[];
  poll?: boolean;
}

// TODO: [STORE_REFACTOR] We only use this hook to fill in gaps that our useHardware doesnt currently provide
// - hashboard.asicIds
// - asic rows and columns
const useHashboardStatus = ({ hashboardSerialNumbers, poll }: UseHashboardStatusProps) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<HashboardStatus[]>([]);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  // Staleness guard: if hashboardSerialNumbers changes mid-fetch, an older
  // Promise.all resolving after a newer one must not overwrite fresher data.
  const fetchIdRef = useRef(0);
  const fetchData = useCallback(
    (signal: AbortSignal) => {
      if (!adapter || hashboardSerialNumbers.length === 0) return;

      setPending(true);
      setError(undefined);
      const fetchId = ++fetchIdRef.current;

      return authRetry({
        request: (params) => adapter.getHashboardStatus(hashboardSerialNumbers, params),
        signal,
        onSuccess: (results) => {
          if (fetchId !== fetchIdRef.current) return;
          setData(results);
        },
        onError: (err) => setError(err?.error?.message ?? "Unknown error occurred"),
      }).finally(() => {
        setPending(false);
      });
    },
    [hashboardSerialNumbers, adapter, authRetry],
  );

  usePoll({
    fetchData,
    // Hydrating ASIC hardware rebuilds Diagnostics' serial array. Restart only
    // when its contents change, or every response triggers another request.
    params: JSON.stringify(hashboardSerialNumbers),
    poll,
  });

  useEffect(() => {
    if (data.length === 0) return;

    // Collect all ASICs to add in a single batch
    const asicsToAdd: Array<AsicHardwareData> = [];
    const hashboardsToUpdate: Array<HashboardHardwareData> = [];

    data.forEach((hashboard) => {
      // No ASIC identities reported for this hashboard yet — nothing to record.
      if (hashboard.asicIds.length === 0) {
        return;
      }

      // Initialize hardware store with hashboard and ASIC structure
      const existingHashboard = useMinerStore.getState().hardware.getHashboard(hashboard.serial);
      hashboardsToUpdate.push(
        existingHashboard
          ? { ...existingHashboard, asicIds: hashboard.asicIds }
          : { serial: hashboard.serial, asicIds: hashboard.asicIds },
      );

      // Collect ASIC info with positional data for batch processing. Always merge
      // onto any existing entry: useTelemetry may have created one without
      // positional data, and skipping the update leaves the ASIC table unable to
      // render rows.
      for (const asic of hashboard.asics) {
        const existingAsic = useMinerStore.getState().hardware.getAsic(asic.id);
        asicsToAdd.push({ ...existingAsic, ...asic });
      }
    });

    const hardware = useMinerStore.getState().hardware;
    if (hashboardsToUpdate.length > 0) hardware.batchAddHashboards(hashboardsToUpdate);
    if (asicsToAdd.length > 0) hardware.batchAddAsics(asicsToAdd);

    // Apply every ASIC telemetry sample in one Immer transaction. A full CT1
    // can report tens of thousands of ASICs, so per-sample store writes make
    // initial hydration prohibitively expensive.
    useMinerStore.setState((state) => {
      data.forEach((hashboard) => {
        const temperatures = hashboard.asicTelemetry.flatMap((sample) =>
          sample.temperature &&
          typeof sample.temperature.value === "number" &&
          Number.isFinite(sample.temperature.value)
            ? [sample.temperature.value]
            : [],
        );
        const id = getHashboardId(hashboard.serial);
        if (temperatures.length) {
          const board = state.telemetry.hashboards.get(id) ?? { hashboardId: id };
          board.maximumAsicTemperature = {
            ...board.maximumAsicTemperature,
            latest: { value: Math.max(...temperatures), units: "°C" },
          };
          state.telemetry.hashboards.set(id, board);
        }
        for (const sample of hashboard.asicTelemetry) {
          const normalizedAsicId = asAsicId(sample.asicId);
          let asicTelemetry = state.telemetry.asics.get(normalizedAsicId);

          if (!asicTelemetry) {
            asicTelemetry = { asicId: normalizedAsicId };
            state.telemetry.asics.set(normalizedAsicId, asicTelemetry);
          }

          if (sample.voltage) {
            asicTelemetry.voltage = { ...asicTelemetry.voltage, latest: sample.voltage };
          }
          if (sample.frequency) {
            asicTelemetry.frequency = { ...asicTelemetry.frequency, latest: sample.frequency };
          }
          if (sample.hashrate) {
            asicTelemetry.hashrate = { ...asicTelemetry.hashrate, latest: sample.hashrate };
          }
          if (sample.temperature) {
            asicTelemetry.temperature = { ...asicTelemetry.temperature, latest: sample.temperature };
          }
          if (sample.passRate) {
            asicTelemetry.passRate = { ...asicTelemetry.passRate, latest: sample.passRate };
          }
        }
      });
    });
  }, [data]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardStatus };
