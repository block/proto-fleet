import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { getSavedViewsStorageKey } from "./savedViews";
import useFleetViews from "./useFleetViews";

const KEY_ALICE = getSavedViewsStorageKey("alice");
const KEY_BOB = getSavedViewsStorageKey("bob");

describe("useFleetViews", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("starts with default record when storage is empty", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    expect(result.current.record.views).toEqual([]);
  });

  it("hydrates and migrates v1 payloads, defaulting legacy entries to tab=miners", () => {
    localStorage.setItem(
      KEY_ALICE,
      JSON.stringify({
        version: 1,
        views: [{ id: "u1", name: "User one", searchParams: "model=S21", createdAt: "2026-04-30T00:00:00.000Z" }],
        deletedBuiltInIds: ["offline"],
      }),
    );

    const { result } = renderHook(() => useFleetViews("alice"));
    expect(result.current.record.views).toHaveLength(1);
    expect(result.current.record.views[0].name).toBe("User one");
    expect(result.current.record.views[0].tab).toBe("miners");
  });

  it("falls back to default record on corrupt JSON", () => {
    localStorage.setItem(KEY_ALICE, "{not json");
    const { result } = renderHook(() => useFleetViews("alice"));
    expect(result.current.record.views).toEqual([]);
  });

  it("addUserView roundtrips through localStorage", () => {
    const { result } = renderHook(() => useFleetViews("alice"));

    act(() => {
      result.current.addUserView({ name: "S21", tab: "miners", searchParams: "status=hashing&model=S21" });
    });

    expect(result.current.record.views).toHaveLength(1);
    expect(result.current.record.views[0].name).toBe("S21");
    expect(result.current.record.views[0].tab).toBe("miners");
    expect(result.current.record.views[0].searchParams).toBe("model=S21&status=hashing");

    const stored = JSON.parse(localStorage.getItem(KEY_ALICE) ?? "{}");
    expect(stored.views[0].name).toBe("S21");
    expect(stored.views[0].tab).toBe("miners");
  });

  it("addUserView accepts non-miners tabs and canonicalizes against that tab's whitelist", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      // status is a miners-only key — should be stripped against the racks whitelist.
      result.current.addUserView({ name: "DC1 racks", tab: "racks", searchParams: "zone=DC1&status=offline" });
    });
    expect(result.current.record.views[0].tab).toBe("racks");
    expect(result.current.record.views[0].searchParams).toBe("zone=DC1");
  });

  it("renameUserView updates the stored name", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      result.current.addUserView({ name: "First", tab: "miners", searchParams: "" });
    });
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.renameUserView(id, "Renamed");
    });
    expect(result.current.record.views[0].name).toBe("Renamed");
  });

  it("renameUserView ignores empty/whitespace names", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      result.current.addUserView({ name: "Keep", tab: "miners", searchParams: "" });
    });
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.renameUserView(id, "   ");
    });
    expect(result.current.record.views[0].name).toBe("Keep");
  });

  it("updateUserViewParams canonicalizes new params against the view's own tab", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      result.current.addUserView({ name: "Mixed", tab: "miners", searchParams: "status=offline" });
    });
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.updateUserViewParams(id, "view=ignored&status=hashing&model=S21");
    });
    expect(result.current.record.views[0].searchParams).toBe("model=S21&status=hashing");
  });

  it("deleteUserView removes the entry", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      result.current.addUserView({ name: "A", tab: "miners", searchParams: "" });
      result.current.addUserView({ name: "B", tab: "miners", searchParams: "model=S21" });
    });
    const idA = result.current.record.views[0].id;
    act(() => {
      result.current.deleteUserView(idA);
    });
    expect(result.current.record.views.map((view) => view.name)).toEqual(["B"]);
  });

  it("reorderUserViews respects the supplied id order", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      result.current.addUserView({ name: "A", tab: "miners", searchParams: "" });
      result.current.addUserView({ name: "B", tab: "miners", searchParams: "model=S21" });
      result.current.addUserView({ name: "C", tab: "miners", searchParams: "status=offline" });
    });
    const ids = result.current.record.views.map((view) => view.id);
    act(() => {
      result.current.reorderUserViews([ids[2], ids[0], ids[1]]);
    });
    expect(result.current.record.views.map((view) => view.name)).toEqual(["C", "A", "B"]);
  });

  it("scopes records by username", () => {
    const alice = renderHook(() => useFleetViews("alice"));
    act(() => {
      alice.result.current.addUserView({ name: "Alice view", tab: "miners", searchParams: "model=S21" });
    });
    expect(localStorage.getItem(KEY_ALICE)).toBeTruthy();
    expect(localStorage.getItem(KEY_BOB)).toBeNull();

    const bob = renderHook(() => useFleetViews("bob"));
    expect(bob.result.current.record.views).toEqual([]);
  });

  it("removes the storage entry when reverting to default state", () => {
    const { result } = renderHook(() => useFleetViews("alice"));
    act(() => {
      result.current.addUserView({ name: "Temp", tab: "miners", searchParams: "" });
    });
    expect(localStorage.getItem(KEY_ALICE)).not.toBeNull();
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.deleteUserView(id);
    });
    expect(localStorage.getItem(KEY_ALICE)).toBeNull();
  });
});
