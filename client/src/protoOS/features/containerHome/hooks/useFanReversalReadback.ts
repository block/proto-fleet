import { useEffect } from "react";
import {
  dryCoolerHoldReason,
  type FanReversalReadback,
  recordFanReversalReadback,
  useFanReversalOutcome,
} from "./useFanReversalOutcome";
import type { AdapterRequestOptions, ContainerSettings, MinerAdapter } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { withDeadline } from "@/shared/utils/withDeadline";

const READBACK_POLL_MS = 3_000;
const READBACK_TIMEOUT_MS = 10_000;

type AuthRetry = ReturnType<typeof useAuthRetry>;

/** One controller read for readback. Never sends a command. */
export const readFanReversalSettings = async (
  adapter: MinerAdapter,
  authRetry: AuthRetry,
  isCurrent: () => boolean,
) => {
  const controller = new AbortController();
  let settings: ContainerSettings | undefined;
  let failed: unknown;
  await withDeadline(
    () =>
      authRetry({
        signal: controller.signal,
        shouldRetry: () => isCurrent() && !controller.signal.aborted,
        request: (params: AdapterRequestOptions) => adapter.getContainerSettings!(params),
        onSuccess: (value) => {
          settings = value;
        },
        onError: (error) => {
          failed = error;
        },
      }),
    controller,
    READBACK_TIMEOUT_MS,
  );
  if (failed !== undefined) throw failed;
  return settings;
};

// Stamped with when the read was requested, never when it arrived: an answer already on its way
// when a run settled says nothing about what happened after.
export const toFanReversalReadback = (
  settings: ContainerSettings | undefined,
  readAtMs: number,
): FanReversalReadback => ({ status: settings?.dryCoolerFanReversal?.status, readAtMs });

/**
 * Follows the procedure while the dry cooler is held (a run is open here, the controller reports
 * one, or an uncertain run awaits a newer reading), and returns why it is held. `alsoWhile` keeps
 * reading for a host that still needs a settled state. Reads only; a poll never sends the command.
 */
export function useDryCoolerHold(alsoWhile = false) {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const { generation, outcome, readback } = useFanReversalOutcome();
  const reason = dryCoolerHoldReason(outcome, readback);
  const shouldPoll = Boolean(adapter?.getContainerSettings) && (reason !== undefined || alsoWhile);
  useEffect(() => {
    if (!shouldPoll || !adapter?.getContainerSettings) return;
    const isCurrent = () => useMinerStore.getState().connectionGeneration === generation;
    let stopped = false;
    let inFlight = false;
    const poll = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const requestedAtMs = Date.now();
        const current = await readFanReversalSettings(adapter, authRetry, () => isCurrent() && !stopped);
        if (!stopped && isCurrent())
          recordFanReversalReadback(generation, toFanReversalReadback(current, requestedAtMs));
      } catch {
        // The last reading stays on screen; the next poll tries again.
      } finally {
        inFlight = false;
      }
    };
    const id = setInterval(() => void poll(), READBACK_POLL_MS);
    return () => {
      stopped = true;
      clearInterval(id);
    };
  }, [adapter, authRetry, generation, shouldPoll]);
  return reason;
}
