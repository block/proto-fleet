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
    });
  });
});
