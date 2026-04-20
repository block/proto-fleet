import { useFleetStore } from "../useFleetStore";

// =============================================================================
// UI State Selectors
// =============================================================================

export const useTheme = () => useFleetStore((state) => state.ui.theme);

export const useDeviceTheme = () => useFleetStore((state) => state.ui.deviceTheme);

export const useTemperatureUnit = () => useFleetStore((state) => state.ui.temperatureUnit);

export const useDuration = () => useFleetStore((state) => state.ui.duration);

export const useBulkRenamePreferences = () => useFleetStore((state) => state.ui.bulkRenamePreferences);
export const useBulkWorkerNamePreferences = () => useFleetStore((state) => state.ui.bulkWorkerNamePreferences);

export const useIsActionBarVisible = () => useFleetStore((state) => state.ui.isActionBarVisible);

// =============================================================================
// UI Action Selectors
// =============================================================================

export const useSetTheme = () => useFleetStore((state) => state.ui.setTheme);

export const useSetDeviceTheme = () => useFleetStore((state) => state.ui.setDeviceTheme);

export const useSetTemperatureUnit = () => useFleetStore((state) => state.ui.setTemperatureUnit);

export const useSetDuration = () => useFleetStore((state) => state.ui.setDuration);

export const useSetBulkRenamePreferences = () => useFleetStore((state) => state.ui.setBulkRenamePreferences);
export const useSetBulkWorkerNamePreferences = () => useFleetStore((state) => state.ui.setBulkWorkerNamePreferences);

export const useSetActionBarVisible = () => useFleetStore((state) => state.ui.setActionBarVisible);
