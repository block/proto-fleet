import { describe, expect, it } from "vitest";
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import type { UISlice } from "./uiSlice";
import { createUISlice } from "./uiSlice";

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
      expect(state.visibleMinerIds).toEqual(new Set());
    });
  });

  describe("setVisibleMinerIds", () => {
    it("should update visibleMinerIds", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      const minerIds = new Set(["device-1", "device-2", "device-3"]);

      store.getState().ui.setVisibleMinerIds(minerIds);

      expect(store.getState().ui.visibleMinerIds).toEqual(minerIds);
    });

    it("should replace previous visibleMinerIds", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      const firstSet = new Set(["device-1", "device-2"]);
      const secondSet = new Set(["device-3", "device-4", "device-5"]);

      store.getState().ui.setVisibleMinerIds(firstSet);
      expect(store.getState().ui.visibleMinerIds).toEqual(firstSet);

      store.getState().ui.setVisibleMinerIds(secondSet);
      expect(store.getState().ui.visibleMinerIds).toEqual(secondSet);
    });

    it("should handle empty set", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      const minerIds = new Set(["device-1", "device-2"]);
      store.getState().ui.setVisibleMinerIds(minerIds);

      store.getState().ui.setVisibleMinerIds(new Set());

      expect(store.getState().ui.visibleMinerIds).toEqual(new Set());
    });

    it("should maintain Set reference when setting same Set", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      const minerIds = new Set(["device-1"]);

      store.getState().ui.setVisibleMinerIds(minerIds);
      const firstRef = store.getState().ui.visibleMinerIds;

      store.getState().ui.setVisibleMinerIds(minerIds);
      const secondRef = store.getState().ui.visibleMinerIds;

      // Should be the same reference (Immer's draft behavior)
      expect(firstRef).toBe(secondRef);
    });
  });

  describe("Integration with other UI state", () => {
    it("should not affect other UI state when updating visibleMinerIds", () => {
      const store = create<TestStore>()(
        immer((set, _get, _api) => ({
          ui: createUISlice(set as any, _get as any, _api as any),
        })),
      );

      // Set some initial state
      store.getState().ui.setTheme("dark");
      store.getState().ui.setTemperatureUnit("F");
      store.getState().ui.setDuration("5d");

      // Update visibleMinerIds
      store.getState().ui.setVisibleMinerIds(new Set(["device-1"]));

      // Other state should remain unchanged
      expect(store.getState().ui.theme).toBe("dark");
      expect(store.getState().ui.temperatureUnit).toBe("F");
      expect(store.getState().ui.duration).toBe("5d");
    });
  });
});
