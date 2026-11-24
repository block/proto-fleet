import type { ReactNode } from "react";
import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FleetShellStoryDecorator } from "./FleetShellStoryDecorator";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

vi.mock("@/protoFleet/components/AppLayout", () => ({
  default: ({ children }: { children: ReactNode }) => <div data-testid="fleet-shell">{children}</div>,
}));

const STORAGE_KEYS = ["proto-fleet-auth", "proto-ui-preferences", "proto-fleet-multi-site"] as const;
const initialAuth = useFleetStore.getState().auth;
const initialFetch = window.fetch;

describe("FleetShellStoryDecorator", () => {
  let realFetch: typeof window.fetch;
  let realFetchMock: ReturnType<typeof vi.fn>;
  let baselineAuth: typeof initialAuth;

  beforeEach(() => {
    baselineAuth = {
      ...useFleetStore.getState().auth,
      sessionExpiry: new Date("2026-09-01T00:00:00.000Z"),
      isAuthenticated: true,
      username: "existing-user",
      role: "viewer",
      permissions: ["site:read"],
      authLoading: false,
      temporaryPassword: "existing-temp",
    };
    useFleetStore.setState({ auth: baselineAuth });

    localStorage.setItem(STORAGE_KEYS[0], "existing-auth");
    localStorage.setItem(STORAGE_KEYS[1], "existing-ui");
    localStorage.setItem(STORAGE_KEYS[2], "existing-site");

    realFetchMock = vi.fn().mockResolvedValue(new Response("real"));
    realFetch = realFetchMock as unknown as typeof window.fetch;
    window.fetch = realFetch;
  });

  afterEach(() => {
    useFleetStore.setState({ auth: initialAuth });
    localStorage.clear();
    window.fetch = initialFetch;
  });

  it("restores the prior auth, storage, and fetch after unmount", async () => {
    const { unmount } = render(
      <FleetShellStoryDecorator>
        <div>Story</div>
      </FleetShellStoryDecorator>,
    );

    expect(useFleetStore.getState().auth).toMatchObject({
      isAuthenticated: true,
      username: "demo",
      role: "admin",
      permissions: ["rack:read", "miner:read", "fleet:read", "curtailment:read", "activity:read"],
      authLoading: false,
      temporaryPassword: null,
    });
    expect(window.fetch).not.toBe(realFetch);
    await expect(window.fetch("/api-proxy/example").then((response) => response.json())).resolves.toEqual({});
    await window.fetch("/outside");
    expect(realFetchMock).toHaveBeenCalledWith("/outside", undefined);

    unmount();

    expect(window.fetch).toBe(realFetch);
    expect(useFleetStore.getState().auth).toEqual(baselineAuth);
    expect(STORAGE_KEYS.map((key) => localStorage.getItem(key))).toEqual([
      "existing-auth",
      "existing-ui",
      "existing-site",
    ]);
  });

  it("keeps shared shell state until the last overlapping mount unmounts", () => {
    const first = render(<FleetShellStoryDecorator>First</FleetShellStoryDecorator>);
    const shim = window.fetch;
    const second = render(<FleetShellStoryDecorator>Second</FleetShellStoryDecorator>);

    first.unmount();
    expect(window.fetch).toBe(shim);
    expect(useFleetStore.getState().auth.username).toBe("demo");

    second.unmount();
    expect(window.fetch).toBe(realFetch);
    expect(useFleetStore.getState().auth).toEqual(baselineAuth);
  });
});
