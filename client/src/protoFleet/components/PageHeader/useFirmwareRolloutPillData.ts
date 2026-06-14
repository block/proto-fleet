import { useCallback, useEffect, useState } from "react";

import { firmwareRolloutClient } from "@/protoFleet/api/clients";
import { FIRMWARE_ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/firmwareRolloutEvents";
import { type FirmwareRollout } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { isActiveRolloutState } from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";
import { useHasPermission } from "@/protoFleet/store";

export interface UseFirmwareRolloutPillDataResult {
  activeRollouts: FirmwareRollout[];
}

const idlePollIntervalMs = 30_000;
const activePollIntervalMs = 5_000;
const pillPageSize = 100;

/**
 * Background poller for the global header pill. Surfaces non-terminal
 * (draft/running/paused) rollouts so operators see an in-flight rollout from any
 * page. Errors are swallowed — the pill is best-effort and never blocks the app.
 */
export function useFirmwareRolloutPillData(): UseFirmwareRolloutPillDataResult {
  const canRead = useHasPermission("firmware:rollout_read");
  const [activeRollouts, setActiveRollouts] = useState<FirmwareRollout[]>([]);
  const pollIntervalMs = activeRollouts.length > 0 ? activePollIntervalMs : idlePollIntervalMs;

  const refresh = useCallback(async (signal: AbortSignal): Promise<void> => {
    try {
      const response = await firmwareRolloutClient.listFirmwareRollouts(
        { pageToken: "", pageSize: pillPageSize },
        { signal },
      );
      if (signal.aborted) return;
      setActiveRollouts(response.rollouts.filter((rollout) => isActiveRolloutState(rollout.state)));
    } catch {
      // Best-effort: ignore aborts, auth, and transient transport errors.
    }
  }, []);

  // Initial fetch (deferred a tick so we never setState during the effect body),
  // plus an immediate refresh whenever a rollout is created/changed in-app so the
  // pill appears without waiting for the next poll.
  useEffect(() => {
    if (!canRead) return undefined;
    const controller = new AbortController();
    const initialId = window.setTimeout(() => void refresh(controller.signal), 0);
    const onChange = (): void => void refresh(controller.signal);
    window.addEventListener(FIRMWARE_ROLLOUT_CHANGED_EVENT, onChange);
    return () => {
      window.clearTimeout(initialId);
      window.removeEventListener(FIRMWARE_ROLLOUT_CHANGED_EVENT, onChange);
      controller.abort();
    };
  }, [canRead, refresh]);

  // Poll faster while a rollout is active, slower when idle.
  useEffect(() => {
    if (!canRead) return undefined;
    const controller = new AbortController();
    const intervalId = window.setInterval(() => void refresh(controller.signal), pollIntervalMs);
    return () => {
      window.clearInterval(intervalId);
      controller.abort();
    };
  }, [canRead, pollIntervalMs, refresh]);

  return { activeRollouts: canRead ? activeRollouts : [] };
}
