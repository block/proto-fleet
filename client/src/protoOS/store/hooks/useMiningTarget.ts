// =============================================================================
// Mining Target Hooks
// =============================================================================

import useMinerStore from "../useMinerStore";

// State Selectors
export const useMiningTargetValue = () => useMinerStore((state) => state.miningTarget.value);

export const useMiningTargetDefault = () => useMinerStore((state) => state.miningTarget.default);

export const useMiningTargetPerformanceMode = () => useMinerStore((state) => state.miningTarget.performanceMode);

export const useMiningTargetBounds = () => useMinerStore((state) => state.miningTarget.bounds);

export const useMiningTargetPending = () => useMinerStore((state) => state.miningTarget.pending);

export const useMiningTargetError = () => useMinerStore((state) => state.miningTarget.error);

// Action Selectors
export const useSetMiningTargetValue = () => useMinerStore((state) => state.miningTarget.setValue);

export const useSetMiningTargetDefault = () => useMinerStore((state) => state.miningTarget.setDefault);

export const useSetMiningTargetPerformanceMode = () => useMinerStore((state) => state.miningTarget.setPerformanceMode);

export const useSetMiningTargetBounds = () => useMinerStore((state) => state.miningTarget.setBounds);

export const useSetMiningTargetPending = () => useMinerStore((state) => state.miningTarget.setPending);

export const useSetMiningTargetError = () => useMinerStore((state) => state.miningTarget.setError);

export const useSetMiningTargetFromResponse = () => useMinerStore((state) => state.miningTarget.setFromResponse);

export const useResetMiningTarget = () => useMinerStore((state) => state.miningTarget.reset);
