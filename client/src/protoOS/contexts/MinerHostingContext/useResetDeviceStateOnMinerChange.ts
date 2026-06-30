import { useEffect } from "react";

import useMinerStore from "@/protoOS/store/useMinerStore";

/**
 * Clears device-specific store slices for the embedded miner view.
 *
 * The ProtoOS store is a module-level singleton that outlives the embedded
 * view's mount, so device data from a previously-viewed miner lingers. Without
 * this, opening miner B — whether by switching in place or by closing miner A
 * and reopening into B (which remounts the provider) — renders A's
 * hardware/telemetry/status/pools/system data until B's own fetches land, and
 * indefinitely if B is slow or its proxy request fails. The hardware slice also
 * feeds hashboard serials used for follow-up queries, so stale entries there
 * get queried against the new miner.
 *
 * Runs on mount and whenever the per-miner hosting `minerKey` (baseUrl) changes.
 * Direct mode has an empty key and a single miner for the page's lifetime, so it
 * is skipped entirely. UI preferences, auth tokens, and onboarding/identity
 * flags are intentionally left intact.
 */
export const useResetDeviceStateOnMinerChange = (minerKey: string) => {
  useEffect(() => {
    if (!minerKey) {
      return;
    }

    const { hardware, telemetry, minerStatus, pools, systemInfo, networkInfo, miningTarget } = useMinerStore.getState();
    hardware.reset();
    // clearAllData empties miner/hashboards/asics/psus/fans and coolingMode —
    // the latest/timeSeries strips left fans and cooling mode behind.
    telemetry.clearAllData();
    minerStatus.setErrors([]);
    minerStatus.setMiningStatus(undefined);
    pools.setPoolsInfo(undefined);
    systemInfo.setSystemInfo(undefined);
    networkInfo.setNetworkInfo(undefined);
    miningTarget.reset();
  }, [minerKey]);
};
