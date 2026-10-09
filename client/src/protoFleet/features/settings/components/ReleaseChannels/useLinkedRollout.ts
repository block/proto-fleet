import { useCallback } from "react";

import { useRefreshingRead } from "./useRefreshingRead";
import { rolloutClient } from "@/protoFleet/api/clients";
import { RELEASE_CHANNELS_PATH } from "@/protoFleet/components/PageHeader/RolloutPill";
import { useHasPermission, useIsAuthenticated } from "@/protoFleet/store";

export const LINKED_ROLLOUT_PARAM = "rollout";

export const linkedRolloutPath = (rolloutId: bigint) => `${RELEASE_CHANNELS_PATH}&${LINKED_ROLLOUT_PARAM}=${rolloutId}`;

// History can link to a finished update that isn't in the active polling cache.
export function useLinkedRollout(id: string | null) {
  const canRead = useHasPermission("miner:firmware_update");
  const isAuthenticated = useIsAuthenticated();
  const valid = id !== null && /^[1-9]\d{0,18}$/.test(id) && BigInt(id) <= 9223372036854775807n;
  const enabled = valid && canRead && isAuthenticated;
  const read = useCallback(
    async (signal: AbortSignal) => {
      const response = await rolloutClient.getRollout({ rolloutId: BigInt(id!) }, { signal, timeoutMs: 30_000 });
      if (!response.rollout) throw new Error("This firmware update is no longer available.");
      return response.rollout;
    },
    [id],
  );
  const result = useRefreshingRead({ read, enabled, errorMessage: "Couldn't load this firmware update." });
  return {
    ...result,
    data: enabled ? result.data : null,
    error: id !== null && !valid ? "This firmware update link is invalid." : result.error,
  };
}
