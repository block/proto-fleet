import type { StateCreator } from "zustand";
import { type ActiveSite, DEFAULT_ACTIVE_SITE } from "../types/activeSite";
import type { FleetStore } from "../useFleetStore";
import {
  bulkRenameModes,
  type BulkRenamePreferences,
  createDefaultBulkRenamePreferences,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/bulkRenameDefinitions";
import type { FleetDuration } from "@/shared/components/DurationSelector";
import type { TemperatureUnit, Theme, ThemeColor } from "@/shared/features/preferences";

// =============================================================================
// UI Slice Interface
// =============================================================================

export type RacksViewMode = "grid" | "list";

export interface UISlice {
  theme: Theme;
  deviceTheme: ThemeColor | undefined;
  temperatureUnit: TemperatureUnit;
  duration: FleetDuration;
  bulkRenamePreferences: BulkRenamePreferences;
  bulkWorkerNamePreferences: BulkRenamePreferences;
  racksViewMode: RacksViewMode;
  isActionBarVisible: boolean;
  // Ephemeral by design — deliberately absent from the persist
  // partialize so a reload never resurrects an open notepad.
  isNotepadOpen: boolean;
  activeSite: ActiveSite;

  // Actions
  setTheme: (theme: Theme) => void;
  setDeviceTheme: (theme: ThemeColor) => void;
  setTemperatureUnit: (unit: TemperatureUnit) => void;
  setDuration: (duration: FleetDuration) => void;
  setBulkRenamePreferences: (preferences: BulkRenamePreferences) => void;
  setBulkWorkerNamePreferences: (preferences: BulkRenamePreferences) => void;
  setRacksViewMode: (mode: RacksViewMode) => void;
  setActionBarVisible: (visible: boolean) => void;
  setNotepadOpen: (open: boolean) => void;
  setActiveSite: (next: ActiveSite) => void;
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
  bulkRenamePreferences: createDefaultBulkRenamePreferences(),
  bulkWorkerNamePreferences: createDefaultBulkRenamePreferences(bulkRenameModes.worker),
  racksViewMode: "grid",
  isActionBarVisible: false,
  isNotepadOpen: false,
  activeSite: DEFAULT_ACTIVE_SITE,

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

  setBulkRenamePreferences: (preferences) =>
    set((state) => {
      state.ui.bulkRenamePreferences = preferences;
    }),

  setBulkWorkerNamePreferences: (preferences) =>
    set((state) => {
      state.ui.bulkWorkerNamePreferences = preferences;
    }),

  setRacksViewMode: (mode) =>
    set((state) => {
      state.ui.racksViewMode = mode;
    }),

  setActionBarVisible: (visible) =>
    set((state) => {
      state.ui.isActionBarVisible = visible;
    }),

  setNotepadOpen: (open) =>
    set((state) => {
      state.ui.isNotepadOpen = open;
    }),

  setActiveSite: (next) =>
    set((state) => {
      state.ui.activeSite = next;
    }),
});
