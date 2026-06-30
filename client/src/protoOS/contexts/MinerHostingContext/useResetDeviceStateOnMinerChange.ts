import { useRef } from "react";

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
 * The reset runs during render (guarded by a ref so it fires once per key),
 * not in an effect: the embedded children are keyed by miner and remount on a
 * switch, and components that seed local state from the store at mount (e.g.
 * Cooling's coolingMode) would otherwise capture the previous miner's values
 * before a passive effect could clear them. Clearing here — before this
 * provider renders its children — guarantees the new subtree reads empty state.
 *
 * Runs on mount and whenever the per-miner hosting `minerKey` (baseUrl) changes.
 * Direct mode has an empty key and a single miner for the page's lifetime, so it
 * is skipped entirely. UI preferences, auth tokens, and onboarding/identity
 * flags are intentionally left intact.
 */
export const useResetDeviceStateOnMinerChange = (minerKey: string) => {
  const lastKey = useRef<string | null>(null);
  if (minerKey && lastKey.current !== minerKey) {
    lastKey.current = minerKey;
    useMinerStore.getState().resetDeviceData();
  }
};
