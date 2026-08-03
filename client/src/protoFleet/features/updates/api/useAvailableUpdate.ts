import { useCallback, useRef, useState } from "react";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { useFleetStore, useHasPermission, useIsAuthenticated } from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

// Release discovery refreshes roughly hourly. A 15-minute client poll keeps
// the passive indicator reasonably fresh without joining telemetry cadence.
const UPDATE_STATUS_POLL_INTERVAL_MS = 15 * 60 * 1000;
const INSTANCE_UPDATE_PERMISSION = "instance:update";

interface UseAvailableUpdateOptions {
  enabled?: boolean;
}

const currentlyCanPoll = () => {
  const { isAuthenticated, permissions } = useFleetStore.getState().auth;
  return isAuthenticated && permissions.includes(INSTANCE_UPDATE_PERMISSION);
};

/**
 * Polls only for the version needed by the passive shell indicator. The
 * command-bearing status response is intentionally not retained here;
 * Settings fetches authoritative status before exposing update actions.
 * Separate tabs converge through this poll rather than cross-tab messaging
 * because stale indicator text cannot perform an update.
 */
export function useAvailableUpdate({ enabled = true }: UseAvailableUpdateOptions = {}): string | null {
  const hasUpdatePermission = useHasPermission(INSTANCE_UPDATE_PERMISSION);
  const isAuthenticated = useIsAuthenticated();
  const [availableVersion, setAvailableVersion] = useState<string | null>(null);
  const latestRequestId = useRef(0);
  const pollEnabled = enabled && hasUpdatePermission && isAuthenticated;

  const fetchData = useCallback(async () => {
    const requestId = ++latestRequestId.current;
    try {
      const status = await instanceUpdateClient.getUpdateStatus({});
      // A late response may populate the internal cache after a route hides
      // the indicator, but the render gate below still keeps it invisible.
      // Settings always revalidates before exposing an actionable command.
      if (requestId !== latestRequestId.current || !currentlyCanPoll()) {
        return;
      }

      const nextVersion =
        status.statusAvailable && status.updateAvailable ? (status.latestEligible?.version ?? null) : null;
      setAvailableVersion((current) => (current === nextVersion ? current : nextVersion));
    } catch {
      if (requestId !== latestRequestId.current) {
        return;
      }
      // The indicator has no error surface. Hide stale text and let usePoll
      // retry on its normal cadence. This best-effort background owner never
      // mutates global auth: Settings handles authoritative permission errors.
      setAvailableVersion(null);
    }
  }, []);

  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: UPDATE_STATUS_POLL_INTERVAL_MS,
    enabled: pollEnabled,
  });

  return pollEnabled ? availableVersion : null;
}
