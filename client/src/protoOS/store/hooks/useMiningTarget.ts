// =============================================================================
// Mining Target Hooks
// =============================================================================

import { ROOT_MODULE_ID } from "../contract";
import useMinerStore from "../useMinerStore";

const selectTarget = (state: ReturnType<typeof useMinerStore.getState>) =>
  state.miningTarget.byModule.get(ROOT_MODULE_ID);

// State Selectors
export const useMiningTargetValue = () => useMinerStore((state) => selectTarget(state)?.valueWatts);

export const useMiningTargetDefault = () => useMinerStore((state) => selectTarget(state)?.defaultWatts);

export const useMiningTargetPerformanceMode = () => useMinerStore((state) => selectTarget(state)?.performanceMode);

export const useMiningTargetBounds = () => useMinerStore((state) => selectTarget(state)?.boundsWatts);

export const useMiningTargetPending = () =>
  useMinerStore((state) => selectTarget(state)?.resource.status === "loading");

export const useMiningTargetError = () =>
  useMinerStore((state) => {
    const resource = selectTarget(state)?.resource;
    return resource?.status === "error" ? resource.error.message : null;
  });

// Action Selectors
export const useSetMiningTargetValue = () => useMinerStore((state) => state.miningTarget.setValue);

export const useSetMiningTargetDefault = () => useMinerStore((state) => state.miningTarget.setDefault);

export const useSetMiningTargetPerformanceMode = () => useMinerStore((state) => state.miningTarget.setPerformanceMode);

export const useSetMiningTargetBounds = () => useMinerStore((state) => state.miningTarget.setBounds);

export const useSetMiningTargetPending = () => useMinerStore((state) => state.miningTarget.setPending);

export const useSetMiningTargetError = () => useMinerStore((state) => state.miningTarget.setError);

export const useSetMiningTargetFromResponse = () => useMinerStore((state) => state.miningTarget.setFromResponse);

export const useResetMiningTarget = () => useMinerStore((state) => state.miningTarget.reset);
