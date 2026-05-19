import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useActiveSite } from "./useActiveSite";

const setUsernameMock = vi.fn();
const mockUseUsername = vi.fn<() => string>(() => "alice");

vi.mock("@/protoFleet/store", () => ({
  useUsername: () => mockUseUsername(),
}));

const localStorageStub = (() => {
  let store: Record<string, string> = {};
  return {
    get store() {
      return store;
    },
    reset: () => {
      store = {};
    },
    getItem: (key: string) => (key in store ? store[key] : null),
    setItem: (key: string, value: string) => {
      store[key] = value;
    },
    removeItem: (key: string) => {
      delete store[key];
    },
    clear: () => {
      store = {};
    },
  };
})();

beforeEach(() => {
  localStorageStub.reset();
  setUsernameMock.mockReset();
  mockUseUsername.mockReturnValue("alice");
  vi.stubGlobal("localStorage", localStorageStub);
});

describe("useActiveSite", () => {
  it("returns the default { kind: 'all' } when storage is empty", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }));
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("persists writes through localStorage under a username-keyed slot", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["7"]) }));
    act(() => result.current.setActiveSite({ kind: "site", id: "7" }));
    expect(result.current.activeSite).toEqual({ kind: "site", id: "7" });
    expect(localStorageStub.getItem("multiSite.activeSite:alice")).toContain('"id":"7"');
  });

  it("keys storage per username so two users on the same browser stay isolated", () => {
    // useReactiveLocalStorage initialises from the key once at mount, so a
    // username change requires a fresh mount (which is what happens on login
    // anyway). The contract this test pins down is: each username gets its
    // own slot, and selections written under one don't leak into the other.
    mockUseUsername.mockReturnValue("alice");
    const aliceHook = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }));
    act(() => aliceHook.result.current.setActiveSite({ kind: "site", id: "1" }));

    mockUseUsername.mockReturnValue("bob");
    const bobHook = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }));
    expect(bobHook.result.current.activeSite).toEqual({ kind: "all" });
    expect(localStorageStub.getItem("multiSite.activeSite:alice")).toContain('"id":"1"');
    expect(localStorageStub.getItem("multiSite.activeSite:bob")).toBeNull();
  });

  it("falls back to { kind: 'all' } when the stored site id is not in the known set", () => {
    localStorageStub.setItem("multiSite.activeSite:alice", JSON.stringify({ kind: "site", id: "999" }));
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1", "2"]) }));
    expect(result.current.activeSite).toEqual({ kind: "all" });
  });

  it("preserves a stored selection while known set is empty (pre-fetch window)", () => {
    localStorageStub.setItem("multiSite.activeSite:alice", JSON.stringify({ kind: "site", id: "12" }));
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set() }));
    // ListSites hasn't returned yet; do not clobber the selection.
    expect(result.current.activeSite).toEqual({ kind: "site", id: "12" });
  });

  it("supports the unassigned selection variant", () => {
    const { result } = renderHook(() => useActiveSite({ knownSiteIds: new Set(["1"]) }));
    act(() => result.current.setActiveSite({ kind: "unassigned" }));
    expect(result.current.activeSite).toEqual({ kind: "unassigned" });
  });
});
