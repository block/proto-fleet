import type { StateCreator } from "zustand";
import { Duration, durations } from "@/shared/components/DurationSelector";

// =============================================================================
// UI Slice Interface
// =============================================================================

export interface UISlice {
  // State
  duration: Duration; // "1h" | "12h" | "24h" | "48h" | "5d"
  activeChartLines: string[]; // Chart lines that are currently visible

  // Actions
  setDuration: (duration: Duration) => void;
  setActiveChartLines: (lines: string[]) => void;
  toggleActiveChartLine: (line: string) => void;
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
  // Initial state
  duration: durations[2], // Default to "24h"
  activeChartLines: [],

  // Actions
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
});
