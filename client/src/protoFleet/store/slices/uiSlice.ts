import type { StateCreator } from "zustand";
import type { FleetStore } from "../useFleetStore";
import type { FleetDuration } from "@/shared/components/DurationSelector";
import type { TemperatureUnit, Theme, ThemeColor } from "@/shared/features/preferences";

// =============================================================================
// UI Slice Interface
// =============================================================================

export interface UISlice {
  theme: Theme;
  deviceTheme: ThemeColor | undefined;
  temperatureUnit: TemperatureUnit;
  duration: FleetDuration;

  // Actions
  setTheme: (theme: Theme) => void;
  setDeviceTheme: (theme: ThemeColor) => void;
  setTemperatureUnit: (unit: TemperatureUnit) => void;
  setDuration: (duration: FleetDuration) => void;
}

// =============================================================================
// UI Slice Creator
// =============================================================================

export const createUISlice: StateCreator<FleetStore, [["zustand/immer", never]], [], UISlice> = (set) => ({
  // Initial state
  theme: "system",
  deviceTheme: undefined,
  temperatureUnit: "C",
  duration: "24h",

  // Actions
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

  setDuration: (duration) =>
    set((state) => {
      state.ui.duration = duration;
    }),
});
