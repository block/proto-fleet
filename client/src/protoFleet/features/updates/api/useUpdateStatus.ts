import { useCallback, useState } from "react";

import { updatesClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { useHasPermission } from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

// Releases land at most daily, so poll far slower than the telemetry-cadence
// POLL_INTERVAL_MS used elsewhere.
const UPDATE_STATUS_POLL_INTERVAL_MS = 15 * 60 * 1000;

export interface UseUpdateStatusResult {
  status: GetUpdateStatusResponse | null;
  loading: boolean;
  hasUpdatePermission: boolean;
}

// Polls GetUpdateStatus for permission holders only: the RPC is gated by
// instance:update, so non-holders never fire it. Errors are swallowed by
// design — the nav-footer callout has no error surface and simply stays hidden
// until a successful response arrives.
export function useUpdateStatus(): UseUpdateStatusResult {
  const hasUpdatePermission = useHasPermission("instance:update");
  const [status, setStatus] = useState<GetUpdateStatusResponse | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      setStatus(await updatesClient.getUpdateStatus({}));
    } catch {
      // Silent: keep the last known status (if any) and retry on the next poll.
    } finally {
      setLoading(false);
    }
  }, []);

  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: UPDATE_STATUS_POLL_INTERVAL_MS,
    enabled: hasUpdatePermission,
  });

  return { status, loading, hasUpdatePermission };
}
