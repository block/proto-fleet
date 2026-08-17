import useMinerStore from "../useMinerStore";
import { ROOT_MODULE_KEY } from "../contract";

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
