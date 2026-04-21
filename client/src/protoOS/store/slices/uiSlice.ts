import type { StateCreator } from "zustand";
import type { MinerStore } from "../useMinerStore";
import type { AuthAction, TemperatureUnit, Theme, ThemeColor } from "@/protoOS/store/types";
import { Duration, durations } from "@/shared/components/DurationSelector";

// =============================================================================
// UI Slice Interface
// =============================================================================

export interface WakeDialog {
  show: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

export interface UISlice {
  // Chart State
  duration: Duration; // "1h" | "12h" | "24h" | "48h" | "5d"
  activeChartLines: string[]; // Chart lines that are currently visible

  // Preferences State
  theme: Theme;
  deviceTheme: ThemeColor | undefined; // OS theme preference
  temperatureUnit: TemperatureUnit;

  // Dialog State
  wakeDialog: WakeDialog;

  // Firmware Update State
  firmwareUpdateDismissed: boolean;

  // Auth UI State
  showLoginModal: boolean;
  dismissedLoginModal: boolean;
  pausedAuthAction: AuthAction;

  // Chart Actions
  setDuration: (duration: Duration) => void;
  setActiveChartLines: (lines: string[]) => void;
  toggleActiveChartLine: (line: string) => void;

  // Preference Actions
  setTheme: (theme: Theme) => void;
  setDeviceTheme: (theme: ThemeColor) => void;
  setTemperatureUnit: (unit: TemperatureUnit) => void;

  // Dialog Actions
  showWakeDialog: (onConfirm: () => void, onClose: () => void) => void;
  hideWakeDialog: () => void;

  // Firmware Update Actions
  setFirmwareUpdateDismissed: (dismissed: boolean) => void;

  // Auth UI Actions
  setShowLoginModal: (show: boolean) => void;
  setDismissedLoginModal: (dismissed: boolean) => void;
  setPausedAuthAction: (action: AuthAction) => void;
}

// =============================================================================
// UI Slice Implementation
// =============================================================================

export const createUISlice: StateCreator<MinerStore, [["zustand/immer", never]], [], UISlice> = (set) => ({
  // Chart Initial State
  duration: durations[2], // Default to "24h"
  activeChartLines: [],

  // Preferences Initial State
  theme: "system",
  deviceTheme: undefined,
  temperatureUnit: "C",

  // Dialog Initial State
  wakeDialog: {
    show: false,
    onConfirm: () => {},
    onClose: () => {},
  },

  // Firmware Update Initial State
  firmwareUpdateDismissed: false,

  // Auth UI Initial State
  showLoginModal: false,
  dismissedLoginModal: false,
  pausedAuthAction: null,

  // Chart Actions
  setDuration: (duration) =>
    set((state) => {
      state.ui.duration = duration;
    }),

  setActiveChartLines: (lines) =>
    set((state) => {
      state.ui.activeChartLines = lines;
    }),

  toggleActiveChartLine: (line) =>
    set((state) => {
      const index = state.ui.activeChartLines.indexOf(line);
      if (index === -1) {
        state.ui.activeChartLines.push(line);
      } else {
        state.ui.activeChartLines.splice(index, 1);
      }
    }),

  // Preference Actions
  setTheme: (theme) =>
    set((state) => {
      state.ui.theme = theme;
    }),

  setDeviceTheme: (theme) =>
    set((state) => {
      state.ui.deviceTheme = theme;
    }),

  setTemperatureUnit: (unit) =>
    set((state) => {
      state.ui.temperatureUnit = unit;
    }),

  // Dialog Actions
  showWakeDialog: (onConfirm, onClose) =>
    set((state) => {
      state.ui.wakeDialog = {
        show: true,
        onConfirm,
        onClose,
      };
    }),

  hideWakeDialog: () =>
    set((state) => {
      state.ui.wakeDialog = {
        show: false,
        onConfirm: () => {},
        onClose: () => {},
      };
    }),

  // Firmware Update Actions
  setFirmwareUpdateDismissed: (dismissed) =>
    set((state) => {
      state.ui.firmwareUpdateDismissed = dismissed;
    }),

  // Auth UI Actions
  setShowLoginModal: (show) =>
    set((state) => {
      state.ui.showLoginModal = show;
      // When showing modal, reset dismissed state
      if (show) {
        state.ui.dismissedLoginModal = false;
      }
    }),

  setDismissedLoginModal: (dismissed) =>
    set((state) => {
      state.ui.dismissedLoginModal = dismissed;
    }),

  setPausedAuthAction: (action) =>
    set((state) => {
      state.ui.pausedAuthAction = action;
    }),
});
