import { useEffect, useRef } from "react";

import useMinerStore from "@/protoOS/store/useMinerStore";

/**
 * Clears device-specific store slices when the embedded miner changes.
 *
 * The ProtoOS store is a module-level singleton, so when an operator opens
 * miner B after viewing miner A in the same Fleet session, B would otherwise
 * render A's hardware/telemetry/status/pools/system data until B's own fetches
 * land — and keep showing it indefinitely if B is slow or its proxy request
 * fails (the hardware slice also feeds hashboard serials used for follow-up
 * queries, so stale entries there are queried against the new miner).
 * Keyed on `minerKey` (the per-miner hosting baseUrl), this wipes the live
 * device data on switch. UI preferences, auth tokens, and onboarding/identity
 * flags are intentionally left intact.
 */
export const useResetDeviceStateOnMinerChange = (minerKey: string) => {
  const previousKey = useRef<string | null>(null);

  useEffect(() => {
    // Skip the first mount: the device slices already start empty, and clearing
    // here would race a hook that has just begun populating them. Only clear on
    // an actual change of miner.
    if (previousKey.current === null) {
      previousKey.current = minerKey;
      return;
    }
    if (previousKey.current === minerKey) {
      return;
    }
    previousKey.current = minerKey;

    const { hardware, telemetry, minerStatus, pools, systemInfo, networkInfo, miningTarget } = useMinerStore.getState();
    hardware.reset();
    telemetry.clearLatestData();
    telemetry.clearTimeSeriesData();
    minerStatus.setErrors([]);
    minerStatus.setMiningStatus(undefined);
    pools.setPoolsInfo(undefined);
    systemInfo.setSystemInfo(undefined);
    networkInfo.setNetworkInfo(undefined);
    miningTarget.reset();
  }, [minerKey]);
};
