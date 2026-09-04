import { useEffect, useMemo, useState } from "react";

import { rolloutClient } from "@/protoFleet/api/clients";
import { type Rollout, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useHasPermission } from "@/protoFleet/store";

export interface UseRolloutPillDataResult {
  activeRollouts: Rollout[];
  hasVisiblePill: boolean;
}

const POLL_INTERVAL_MS = 15_000;

// Polls for active firmware updates to feed the app-wide header pill.
// Gated on miner:firmware_update, the permission the rollout RPCs (and the
// Settings > Firmware page the pill links to) require.
export const useRolloutPillData = ({ enabled = true }: { enabled?: boolean } = {}): UseRolloutPillDataResult => {
  const [activeRollouts, setActiveRollouts] = useState<Rollout[]>([]);
  const canManageFirmware = useHasPermission("miner:firmware_update");
  const active = enabled && canManageFirmware;

  useEffect(() => {
    if (!active) {
      return;
    }
    const refresh = () => {
      rolloutClient
        .listRollouts({})
        .then((resp) => setActiveRollouts(resp.rollouts.filter((r) => r.status === RolloutStatus.ACTIVE)))
        // Header polling is best-effort; the release channels page surfaces errors.
        .catch(() => {});
    };

    refresh();
    const intervalId = window.setInterval(refresh, POLL_INTERVAL_MS);
    return () => window.clearInterval(intervalId);
  }, [active]);

  return useMemo(
    () => ({
      activeRollouts: active ? activeRollouts : [],
      hasVisiblePill: active && activeRollouts.length > 0,
    }),
    [active, activeRollouts],
  );
};
