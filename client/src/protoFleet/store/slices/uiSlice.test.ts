import { describe, expect, it } from "vitest";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import type { UISlice } from "./uiSlice";
import { createUISlice } from "./uiSlice";
import {
  bulkRenameModes,
  createDefaultBulkRenamePreferences,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/bulkRenameDefinitions";

type TestStore = { ui: UISlice };

describe("UISlice", () => {
  describe("Initial state", () => {
    it("should initialize with default values", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      const state = store.getState().ui;

      expect(state.theme).toBe("system");
      expect(state.deviceTheme).toBeUndefined();
      expect(state.temperatureUnit).toBe("C");
      expect(state.duration).toBe("24h");
      expect(state.bulkRenamePreferences.separator).toBe(createDefaultBulkRenamePreferences().separator);
      expect(state.bulkRenamePreferences.properties).toHaveLength(
        createDefaultBulkRenamePreferences().properties.length,
      );
      expect(state.bulkRenamePreferences.properties.every((property) => property.enabled === false)).toBe(true);
      expect(state.bulkWorkerNamePreferences.separator).toBe(
        createDefaultBulkRenamePreferences(bulkRenameModes.worker).separator,
      );
      expect(state.bulkWorkerNamePreferences.properties).toHaveLength(
        createDefaultBulkRenamePreferences(bulkRenameModes.worker).properties.length,
      );
      expect(state.bulkWorkerNamePreferences.properties.every((property) => property.enabled === false)).toBe(true);
      expect(state.isActionBarVisible).toBe(false);
    });
  });

  describe("setBulkWorkerNamePreferences", () => {
    it("should set bulkWorkerNamePreferences", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      const updatedPreferences = {
        ...createDefaultBulkRenamePreferences(bulkRenameModes.worker),
        separator: "underscore" as const,
      };

      store.getState().ui.setBulkWorkerNamePreferences(updatedPreferences);

      expect(store.getState().ui.bulkWorkerNamePreferences.separator).toBe("underscore");
    });
  });

  describe("setActionBarVisible", () => {
    it("should set isActionBarVisible", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      expect(store.getState().ui.isActionBarVisible).toBe(false);

      store.getState().ui.setActionBarVisible(true);
      expect(store.getState().ui.isActionBarVisible).toBe(true);

      store.getState().ui.setActionBarVisible(false);
      expect(store.getState().ui.isActionBarVisible).toBe(false);
    });
  });
});
