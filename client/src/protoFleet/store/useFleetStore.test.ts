import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const UI_KEY = "proto-ui-preferences";

const seedPersistedDuration = (duration: string) => {
  localStorage.setItem(
    UI_KEY,
    JSON.stringify({
      state: {
        ui: {
          duration,
        },
      },
      version: 0,
    }),
  );
};

describe("useFleetStore persistence", () => {
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("falls back to the default fleet duration when persisted duration is no longer supported", async () => {
    seedPersistedDuration("3d");

    const { useFleetStore } = await import("./useFleetStore");
    useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().ui.duration).toBe("24h");
  });

  it("preserves persisted fleet durations that are still supported", async () => {
    seedPersistedDuration("7d");

    const { useFleetStore } = await import("./useFleetStore");
    useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().ui.duration).toBe("7d");
  });
});
