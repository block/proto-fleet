import { ROOT_MODULE_KEY } from "../contract";
import useMinerStore from "../useMinerStore";

// =============================================================================
// State Hooks
// =============================================================================

/**
 * Returns the pools info from the store
 */
export const usePoolsInfo = () => {
  return useMinerStore((state) => state.pools.byOwner.get(ROOT_MODULE_KEY));
};

export const usePoolsInfoResource = () => useMinerStore((state) => state.pools.resourceByOwner.get(ROOT_MODULE_KEY));
export const useSetPoolsInfoPending = () => useMinerStore((state) => state.pools.setPending);
export const useSetPoolsInfoError = () => useMinerStore((state) => state.pools.setError);

// =============================================================================
// Action Hooks
// =============================================================================

/**
 * Returns the setPoolsInfo action
 */
export const useSetPoolsInfo = () => {
  return useMinerStore((state) => state.pools.setPoolsInfo);
};
