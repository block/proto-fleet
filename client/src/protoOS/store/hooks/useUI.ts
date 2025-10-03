import useMinerStore from "../useMinerStore";

// =============================================================================
// UI State Hooks
// =============================================================================

export const useDuration = () => useMinerStore((state) => state.ui.duration);

export const useSetDuration = () =>
  useMinerStore((state) => state.ui.setDuration);

export const useActiveChartLines = () =>
  useMinerStore((state) => state.ui.activeChartLines);

export const useSetActiveChartLines = () =>
  useMinerStore((state) => state.ui.setActiveChartLines);

export const useToggleActiveChartLine = () =>
  useMinerStore((state) => state.ui.toggleActiveChartLine);
