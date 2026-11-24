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
  /**
   * Follow live readings instead of polling when exactly one board is
   * requested and the adapter can stream it. The stream closes on unmount or
   * when the board changes.
   */
  stream?: boolean;
}

// Reconnect delays after a stream fails or closes. A frame resets the delay.
export const HASHBOARD_STREAM_RETRY_MS = 2_000;
export const HASHBOARD_STREAM_MAX_RETRY_MS = 30_000;
// A stream with no new sample for this long is reported stale and reopened.
// The controller sends one sample per second.
export const HASHBOARD_STREAM_STALL_MS = 10_000;
const STALLED_MESSAGE = "The controller stopped sending new ASIC readings.";

// TODO: [STORE_REFACTOR] We only use this hook to fill in gaps that our useHardware doesnt currently provide
// - hashboard.asicIds
// - asic rows and columns
const useHashboardStatus = ({ hashboardSerialNumbers, poll, stream }: UseHashboardStatusProps) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<HashboardStatus[]>([]);
  // A failure belongs to the boards it was reported for, so it never shows
  // for the next selection.
  const serialsKey = JSON.stringify(hashboardSerialNumbers);
  const [failure, setFailure] = useState<{ serials: string; message: string }>();
  const error = failure?.serials === serialsKey ? failure.message : undefined;
  const [pending, setPending] = useState<boolean>(false);
  // Staleness guard: if hashboardSerialNumbers changes mid-fetch, an older
  // Promise.all resolving after a newer one must not overwrite fresher data.
  const fetchIdRef = useRef(0);
  const fetchData = useCallback(
    (signal: AbortSignal) => {
      if (!adapter || hashboardSerialNumbers.length === 0) return;

      setPending(true);
      const fetchId = ++fetchIdRef.current;

      return authRetry({
        request: (params) => adapter.getHashboardStatus(hashboardSerialNumbers, params),
        signal,
        onSuccess: (results) => {
          if (fetchId !== fetchIdRef.current) return;
          setData(results);
          // Cleared here, not before the request, so a failing poll keeps its
          // error until a refresh succeeds.
          setFailure(undefined);
        },
        onError: (err) => setFailure({ serials: serialsKey, message: err?.error?.message ?? "Unknown error occurred" }),
      }).finally(() => {
        setPending(false);
      });
    },
    [hashboardSerialNumbers, serialsKey, adapter, authRetry],
  );

  const streamSerial =
    stream && adapter?.streamHashboardStatus && hashboardSerialNumbers.length === 1
      ? hashboardSerialNumbers[0]
      : undefined;

  usePoll({
    fetchData,
    // Hydrating ASIC hardware rebuilds Diagnostics' serial array. Restart only
    // when its contents change, or every response triggers another request.
    params: serialsKey,
    poll,
    enabled: streamSerial === undefined,
  });

  // A new authRetry identity must not reopen a healthy stream.
  const authRetryRef = useRef(authRetry);
  useEffect(() => {
    authRetryRef.current = authRetry;
  }, [authRetry]);

  useEffect(() => {
    const follow = adapter?.streamHashboardStatus?.bind(adapter);
    if (streamSerial === undefined || !follow) return;
    const controller = new AbortController();
    let retryMs = HASHBOARD_STREAM_RETRY_MS;
    const fail = (message: string) => setFailure({ serials: serialsKey, message });
    let timer: ReturnType<typeof setTimeout> | undefined;
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    let endStalledAttempt: (() => void) | undefined;
    // Both are kept across attempts, so a reopened stream that resends the old
    // sample does not read as current, however it ends.
    let lastObservedAt: string | undefined;
    // Covers the wait for the first sample and every gap between new ones.
    // Only a new sample pushes the deadline back.
    const watch = () => {
      clearTimeout(watchdog);
      watchdog = setTimeout(() => {
        watchdog = undefined;
        // A failure already reported says more than the stall that follows it.
        setFailure((current) =>
          current?.serials === serialsKey ? current : { serials: serialsKey, message: STALLED_MESSAGE },
        );
        endStalledAttempt?.();
      }, HASHBOARD_STREAM_STALL_MS);
    };
    const open = () => {
      setPending(true);
      let failed = false;
      let stalled = false;
      let frames = 0;
      // Each attempt has its own signal so a stall can end it and reconnect.
      const attempt = new AbortController();
      const abortAttempt = () => attempt.abort();
      controller.signal.addEventListener("abort", abortAttempt);
      endStalledAttempt = () => {
        stalled = true;
        attempt.abort();
      };
      // After a stall, each new attempt gets a fresh deadline of its own.
      if (watchdog === undefined) watch();
      void authRetryRef
        .current({
          request: async (params) => {
            for await (const status of follow(streamSerial, params)) {
              if (attempt.signal.aborted) return;
              frames += 1;
              setPending(false);
              setData([status]);
              // A resent sample is not a new reading: it neither clears the
              // error nor holds off the watchdog.
              if (status.observedAt !== undefined && status.observedAt === lastObservedAt) continue;
              lastObservedAt = status.observedAt;
              retryMs = HASHBOARD_STREAM_RETRY_MS;
              setFailure(undefined);
              watch();
            }
          },
          signal: attempt.signal,
          onError: (err) => {
            failed = true;
            fail(err?.error?.message ?? "Unknown error occurred");
          },
        })
        .finally(() => {
          endStalledAttempt = undefined;
          controller.signal.removeEventListener("abort", abortAttempt);
          if (controller.signal.aborted) return;
          setPending(false);
          // A stream that closes before its first frame has shown nothing current.
          if (!failed && !stalled && frames === 0) {
            fail("The controller closed the ASIC stream before sending readings.");
          }
          timer = setTimeout(open, retryMs);
          retryMs = Math.min(retryMs * 2, HASHBOARD_STREAM_MAX_RETRY_MS);
        });
    };
    watch();
    open();
    return () => {
      controller.abort();
      clearTimeout(timer);
      clearTimeout(watchdog);
    };
  }, [adapter, streamSerial, serialsKey]);

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
