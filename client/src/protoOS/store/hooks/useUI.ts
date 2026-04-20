import useMinerStore from "../useMinerStore";

// =============================================================================
// Chart State Hooks
// =============================================================================

export const useDuration = () => useMinerStore((state) => state.ui.duration);

export const useSetDuration = () => useMinerStore((state) => state.ui.setDuration);

export const useActiveChartLines = () => useMinerStore((state) => state.ui.activeChartLines);

export const useSetActiveChartLines = () => useMinerStore((state) => state.ui.setActiveChartLines);

export const useToggleActiveChartLine = () => useMinerStore((state) => state.ui.toggleActiveChartLine);

// =============================================================================
// Preference Hooks
// =============================================================================

export const useTheme = () => useMinerStore((state) => state.ui.theme);

export const useDeviceTheme = () => useMinerStore((state) => state.ui.deviceTheme);

export const useSetTheme = () => useMinerStore((state) => state.ui.setTheme);

export const useSetDeviceTheme = () => useMinerStore((state) => state.ui.setDeviceTheme);

export const useTemperatureUnit = () => useMinerStore((state) => state.ui.temperatureUnit);

export const useSetTemperatureUnit = () => useMinerStore((state) => state.ui.setTemperatureUnit);

// =============================================================================
// Firmware Update Hooks
// =============================================================================

export const useFirmwareUpdateDismissed = () => useMinerStore((state) => state.ui.firmwareUpdateDismissed);

export const useSetFirmwareUpdateDismissed = () => useMinerStore((state) => state.ui.setFirmwareUpdateDismissed);

// =============================================================================
// Auth UI State Hooks
// =============================================================================

export const useShowLoginModal = () => useMinerStore((state) => state.ui.showLoginModal);

export const useDismissedLoginModal = () => useMinerStore((state) => state.ui.dismissedLoginModal);

export const usePausedAuthAction = () => useMinerStore((state) => state.ui.pausedAuthAction);

// =============================================================================
// Auth UI Action Hooks
// =============================================================================

export const useSetShowLoginModal = () => useMinerStore((state) => state.ui.setShowLoginModal);

export const useSetDismissedLoginModal = () => useMinerStore((state) => state.ui.setDismissedLoginModal);

export const useSetPausedAuthAction = () => useMinerStore((state) => state.ui.setPausedAuthAction);
