import { useSyncExternalStore } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";

// The batch of outlet writes in progress, and the result of the last one. It is
// kept outside the component because the Power panel unmounts when another tab
// is selected while the batch keeps sending: the switches must stay locked when
// the panel comes back, and the result must still be shown.
type Batch = { pending: boolean; error?: string; generation?: number };
let batch: Batch = { pending: false };
const listeners = new Set<() => void>();
const setBatch = (next: Batch) => {
  batch = next;
  listeners.forEach((listener) => listener());
};
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => void listeners.delete(listener);
};

export const resetPowerFeedBatch = () => setBatch({ pending: false });

export const usePowerFeeds = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const state = useSyncExternalStore(subscribe, () => batch);
  const current = useMinerStore((store) => store.connectionGeneration);

  const setFeeds = async (powerFeedIds: string[], enabled: boolean) => {
    if (!adapter?.setPowerFeedState || !powerFeedIds.length || batch.pending) return;
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrent = () => useMinerStore.getState().connectionGeneration === generation;
    setBatch({ pending: true, generation });
    let error: string | undefined;
    let switched = 0;
    let failed = 0;
    try {
      // The controller switches outlets itself during a recovery cycle, and it
      // restores them afterward. A write is sent only when the read confirms
      // that no cycle is active.
      try {
        if (await adapter.getActivePowerRecoveryCycle?.()) {
          error = "The controller is power-cycling outlets to recover a module. Try again when it finishes.";
          return;
        }
      } catch {
        error = "Could not confirm that the controller is not power-cycling outlets. No outlets were switched.";
        return;
      }
      // One outlet at a time: the write is an idempotent target state, so each
      // can be retried after a token refresh, and a rejected outlet does not
      // stop the rest of the PDU.
      for (const powerFeedId of powerFeedIds) {
        if (!isCurrent()) return;
        await authRetry({
          shouldRetry: isCurrent,
          request: (params) => adapter.setPowerFeedState!(powerFeedId, enabled, params),
          onSuccess: () => {
            switched++;
          },
          onError: () => {
            failed++;
          },
        });
      }
      if (!isCurrent()) return;
      let refreshed = switched === 0;
      if (switched > 0) {
        try {
          const accept = beginStoreWrite("telemetry:current:miner");
          const telemetry = await adapter.getCurrentTelemetry(["miner"]);
          if (accept()) useMinerStore.getState().telemetry.mergeTelemetry(telemetry, accept.requestId);
          refreshed = true;
        } catch {
          // Reported below; the next poll will catch up.
        }
      }
      if (failed > 0) {
        error = `Could not switch ${failed} of ${powerFeedIds.length} ${powerFeedIds.length === 1 ? "outlet" : "outlets"}. Check PDU status before trying again.`;
      } else if (!refreshed) {
        error = "Command sent. Could not refresh PDU status; waiting for the next update.";
      }
    } finally {
      setBatch({ pending: false, error, generation });
    }
  };

  return {
    pending: state.pending,
    // A result from another device's batch does not belong to this view.
    error: state.generation === current ? state.error : undefined,
    setFeeds,
    supported: Boolean(adapter?.setPowerFeedState),
  };
};
