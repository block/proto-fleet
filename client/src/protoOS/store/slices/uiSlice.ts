import type { StateCreator } from "zustand";
import type { TemperatureUnit, Theme, ThemeColor } from "@/protoOS/store/types";
import { Duration, durations } from "@/shared/components/DurationSelector";

// =============================================================================
// UI Slice Interface
// =============================================================================

export interface UISlice {
  // Chart State
  duration: Duration; // "1h" | "12h" | "24h" | "48h" | "5d"
  activeChartLines: string[]; // Chart lines that are currently visible

  // Preferences State
  theme: Theme;
  deviceTheme: ThemeColor | undefined; // OS theme preference
  temperatureUnit: TemperatureUnit;

  // Chart Actions
  setDuration: (duration: Duration) => void;
  setActiveChartLines: (lines: string[]) => void;
  toggleActiveChartLine: (line: string) => void;

  // Preference Actions
  setTheme: (theme: Theme) => void;
  setDeviceTheme: (theme: ThemeColor) => void;
  setTemperatureUnit: (unit: TemperatureUnit) => void;
}

// =============================================================================
// UI Slice Implementation
// =============================================================================

export const createUISlice: StateCreator<
  { hardware: any; telemetry: any; ui: UISlice },
  [["zustand/immer", never]],
  [],
  UISlice
> = (set) => ({
  // Chart Initial State
  duration: durations[2], // Default to "24h"
  activeChartLines: [],

  // Preferences Initial State
  theme: "system",
  deviceTheme: undefined,
  temperatureUnit: "C",

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
});
