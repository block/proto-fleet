import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const UI_KEY = "proto-ui-preferences";
const AUTH_KEY = "proto-fleet-auth";

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

const seedPersistedAuth = (auth: Record<string, unknown>) => {
  localStorage.setItem(
    AUTH_KEY,
    JSON.stringify({
      state: { auth },
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
    await useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().ui.duration).toBe("24h");
  });

  it("preserves persisted fleet durations that are still supported", async () => {
    seedPersistedDuration("7d");

    const { useFleetStore } = await import("./useFleetStore");
    await useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().ui.duration).toBe("7d");
  });

  it("preserves persisted org-scoped permissions", async () => {
    seedPersistedAuth({
      sessionExpiry: new Date(Date.now() + 60_000),
      isAuthenticated: true,
      username: "alice@example.com",
      role: "ADMIN",
      permissions: ["site:read"],
      permissionsScope: "org",
    });

    const { useFleetStore } = await import("./useFleetStore");
    await useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().auth.permissions).toEqual(["site:read"]);
    expect(useFleetStore.getState().auth.isAuthenticated).toBe(true);
  });

  it("preserves org-scoped sessions with no permissions", async () => {
    seedPersistedAuth({
      sessionExpiry: new Date(Date.now() + 60_000),
      isAuthenticated: true,
      username: "alice@example.com",
      role: "CUSTOM",
      permissions: [],
      permissionsScope: "org",
    });

    const { useFleetStore } = await import("./useFleetStore");
    await useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().auth.permissions).toEqual([]);
    expect(useFleetStore.getState().auth.isAuthenticated).toBe(true);
  });

  it("migrates persisted notification:* permission keys to alert:*", async () => {
    seedPersistedAuth({
      sessionExpiry: new Date(Date.now() + 60_000),
      isAuthenticated: true,
      username: "alice@example.com",
      role: "ADMIN",
      permissions: ["notification:read", "notification:manage", "site:read"],
      permissionsScope: "org",
    });

    const { useFleetStore } = await import("./useFleetStore");
    await useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().auth.permissions).toEqual(["alert:read", "alert:manage", "site:read"]);
    expect(useFleetStore.getState().auth.isAuthenticated).toBe(true);
  });

  it("drops persisted sessions missing the org-scoped permissions marker", async () => {
    seedPersistedAuth({
      sessionExpiry: new Date(Date.now() + 60_000),
      isAuthenticated: true,
      username: "alice@example.com",
      role: "ADMIN",
      permissions: ["curtailment:manage"],
    });

    const { useFleetStore } = await import("./useFleetStore");
    await useFleetStore.persist.rehydrate();

    expect(useFleetStore.getState().auth.isAuthenticated).toBe(false);
    expect(useFleetStore.getState().auth.permissions).toEqual([]);
    expect(useFleetStore.getState().auth.authLoading).toBe(false);
  });
});
