import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { HashboardStatus } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { AsicHardwareData } from "@/protoOS/store";
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
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<HashboardStatus[]>([]);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  // Staleness guard: if hashboardSerialNumbers changes mid-fetch, an older
  // Promise.all resolving after a newer one must not overwrite fresher data.
  const fetchIdRef = useRef(0);
  const fetchData = useCallback(() => {
    if (!adapter || hashboardSerialNumbers.length === 0) return;

    setPending(true);
    setError(undefined);
    const fetchId = ++fetchIdRef.current;

    return authRetry({
      request: (params) => adapter.getHashboardStatus(hashboardSerialNumbers, params),
      onSuccess: (results) => {
        if (fetchId !== fetchIdRef.current) return;
        setData(results);
      },
      onError: (err) => setError(err?.error?.message ?? "Unknown error occurred"),
    }).finally(() => {
      setPending(false);
    });
  }, [hashboardSerialNumbers, adapter, authRetry]);

  usePoll({
    fetchData,
    params: hashboardSerialNumbers,
    poll,
  });

  useEffect(() => {
    if (data.length === 0) return;

    // Collect all ASICs to add in a single batch
    const asicsToAdd: Array<AsicHardwareData> = [];

    data.forEach((hashboard) => {
      // No ASIC identities reported for this hashboard yet — nothing to record.
      if (hashboard.asicIds.length === 0) {
        return;
      }

      // Initialize hardware store with hashboard and ASIC structure
      const existingHashboard = useMinerStore.getState().hardware.getHashboard(hashboard.serial);
      useMinerStore
        .getState()
        .hardware.addHashboard(
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

        if (!existingAsic) {
          useMinerStore.getState().hardware.linkAsicToHashboard(asic.id, hashboard.serial);
        }
      }

      // Update telemetry store with the adapter's canonical voltage/frequency
      // samples using Immer-safe setState.
      for (const sample of hashboard.asicTelemetry) {
        useMinerStore.setState((state) => {
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
        });
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
