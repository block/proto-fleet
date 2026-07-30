import { useCallback, useState } from "react";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { useHasPermission } from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

// The server refreshes release discovery roughly hourly; poll this status far
// slower than the telemetry-cadence POLL_INTERVAL_MS used elsewhere.
const UPDATE_STATUS_POLL_INTERVAL_MS = 15 * 60 * 1000;

export interface UseUpdateStatusResult {
  status: GetUpdateStatusResponse | null;
  hasUpdatePermission: boolean;
}

const statusUnchanged = (prev: GetUpdateStatusResponse, next: GetUpdateStatusResponse) =>
  prev.currentVersion === next.currentVersion &&
  prev.statusAvailable === next.statusAvailable &&
  prev.updateAvailable === next.updateAvailable &&
  prev.channel === next.channel &&
  prev.installCommand === next.installCommand &&
  prev.latestEligible?.version === next.latestEligible?.version;

// Polls GetUpdateStatus for permission holders only: the RPC is gated by
// instance:update, so non-holders never fire it. Errors are swallowed by
// design — the nav-footer callout has no error surface and simply stays hidden
// until a successful response arrives.
export function useUpdateStatus(): UseUpdateStatusResult {
  const hasUpdatePermission = useHasPermission("instance:update");
  const [status, setStatus] = useState<GetUpdateStatusResponse | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const next = await instanceUpdateClient.getUpdateStatus({});
      // Most polls return identical data; keep the previous reference so
      // consumers don't re-render for a no-op tick.
      setStatus((prev) => (prev && statusUnchanged(prev, next) ? prev : next));
    } catch {
      // Silent: keep the last known status (if any) and retry on the next poll.
    }
  }, []);

  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: UPDATE_STATUS_POLL_INTERVAL_MS,
    enabled: hasUpdatePermission,
  });

  return { status, hasUpdatePermission };
}
