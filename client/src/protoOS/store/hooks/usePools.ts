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

// =============================================================================
// Action Hooks
// =============================================================================

/**
 * Returns the setPoolsInfo action
 */
export const useSetPoolsInfo = () => {
  return useMinerStore((state) => state.pools.setPoolsInfo);
};
