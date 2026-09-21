import { useEffect, useMemo, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { rolloutClient } from "@/protoFleet/api/clients";
import { type Rollout, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  useAuthErrors,
  useFleetStore,
  useHasPermission,
  useIsAuthenticated,
  useSessionGeneration,
  useUsername,
} from "@/protoFleet/store";

export interface UseRolloutPillDataResult {
  activeRollouts: Rollout[];
  hasVisiblePill: boolean;
}

const POLL_INTERVAL_MS = 15_000;
const POLL_RPC_TIMEOUT_MS = 30_000;
const PAGE_SIZE = 1000;
const transientCodes = new Set([
  Code.Canceled,
  Code.Unknown,
  Code.DeadlineExceeded,
  Code.ResourceExhausted,
  Code.Aborted,
  Code.Internal,
  Code.Unavailable,
]);

// Polls for active firmware updates to feed the app-wide header pill.
// Gated on miner:firmware_update, the permission the rollout RPCs (and the
// Settings > Firmware page the pill links to) require.
export const useRolloutPillData = ({ enabled = true }: { enabled?: boolean } = {}): UseRolloutPillDataResult => {
  const { handleAuthErrors } = useAuthErrors();
  const canManageFirmware = useHasPermission("miner:firmware_update");
  const isAuthenticated = useIsAuthenticated();
  const username = useUsername();
  const sessionGeneration = useSessionGeneration();
  const active = enabled && canManageFirmware && isAuthenticated;
  // Each visibility/permission/login transition needs a fresh snapshot; old
  // data must not briefly reappear when polling is enabled again.
  const scope = useMemo(() => ({ active, username, sessionGeneration }), [active, username, sessionGeneration]);
  const [snapshot, setSnapshot] = useState({ scope, rollouts: [] as Rollout[] });

  useEffect(() => {
    if (!scope.active) return;
    const controller = new AbortController();
    let disposed = false;
    let inFlight = false;
    const isCurrent = () => {
      const auth = useFleetStore.getState().auth;
      return (
        !disposed &&
        auth.isAuthenticated &&
        auth.username === scope.username &&
        auth.sessionGeneration === scope.sessionGeneration &&
        auth.permissions.includes("miner:firmware_update")
      );
    };
    const checkCurrent = () => {
      if (!isCurrent()) controller.abort();
      controller.signal.throwIfAborted();
    };
    const refresh = async () => {
      // Slow scans skip timer ticks instead of queuing competing snapshots.
      if (inFlight || !isCurrent()) return;
      inFlight = true;
      try {
        const rollouts: Rollout[] = [];
        let cursor = "";
        do {
          checkCurrent();
          const response = await rolloutClient.listRollouts(
            { status: RolloutStatus.ACTIVE, pageSize: PAGE_SIZE, cursor },
            { signal: controller.signal, timeoutMs: POLL_RPC_TIMEOUT_MS },
          );
          checkCurrent();
          rollouts.push(...response.rollouts);
          cursor = response.cursor;
        } while (cursor);
        setSnapshot({ scope, rollouts });
      } catch (error) {
        if (!isCurrent() || controller.signal.aborted) return;
        // Authorization and other permanent failures invalidate cached details.
        // Transient failures retain only the last complete scan, never a page.
        if (error instanceof ConnectError && !transientCodes.has(error.code)) {
          setSnapshot({ scope, rollouts: [] });
        }
        if (error instanceof ConnectError && error.code === Code.Unauthenticated) handleAuthErrors({ error });
      } finally {
        inFlight = false;
      }
    };

    void refresh();
    const intervalId = window.setInterval(() => void refresh(), POLL_INTERVAL_MS);
    return () => {
      disposed = true;
      controller.abort();
      window.clearInterval(intervalId);
    };
  }, [scope, handleAuthErrors]);

  return useMemo(
    () => ({
      activeRollouts: active && snapshot.scope === scope ? snapshot.rollouts : [],
      hasVisiblePill: active && snapshot.scope === scope && snapshot.rollouts.length > 0,
    }),
    [active, scope, snapshot],
  );
};
