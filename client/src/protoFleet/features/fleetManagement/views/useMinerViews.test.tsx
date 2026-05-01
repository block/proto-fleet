import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { getSavedViewsStorageKey } from "./savedViews";
import useMinerViews from "./useMinerViews";

const KEY_ALICE = getSavedViewsStorageKey("alice");
const KEY_BOB = getSavedViewsStorageKey("bob");

describe("useMinerViews", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("starts with default record when storage is empty", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    expect(result.current.record.views).toEqual([]);
    expect(result.current.record.deletedBuiltInIds).toEqual([]);
  });

  it("hydrates from existing localStorage payload", () => {
    localStorage.setItem(
      KEY_ALICE,
      JSON.stringify({
        version: 1,
        views: [{ id: "u1", name: "User one", searchParams: "model=S21", createdAt: "2026-04-30T00:00:00.000Z" }],
        deletedBuiltInIds: ["offline"],
      }),
    );

    const { result } = renderHook(() => useMinerViews("alice"));
    expect(result.current.record.views).toHaveLength(1);
    expect(result.current.record.views[0].name).toBe("User one");
    expect(result.current.record.deletedBuiltInIds).toEqual(["offline"]);
  });

  it("falls back to default record on corrupt JSON", () => {
    localStorage.setItem(KEY_ALICE, "{not json");
    const { result } = renderHook(() => useMinerViews("alice"));
    expect(result.current.record.views).toEqual([]);
    expect(result.current.record.deletedBuiltInIds).toEqual([]);
  });

  it("addUserView roundtrips through localStorage", () => {
    const { result } = renderHook(() => useMinerViews("alice"));

    act(() => {
      result.current.addUserView({ name: "S21", searchParams: "status=hashing&model=S21" });
    });

    expect(result.current.record.views).toHaveLength(1);
    expect(result.current.record.views[0].name).toBe("S21");
    expect(result.current.record.views[0].searchParams).toBe("model=S21&status=hashing");

    const stored = JSON.parse(localStorage.getItem(KEY_ALICE) ?? "{}");
    expect(stored.views[0].name).toBe("S21");
  });

  it("renameUserView updates the stored name", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.addUserView({ name: "First", searchParams: "" });
    });
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.renameUserView(id, "Renamed");
    });
    expect(result.current.record.views[0].name).toBe("Renamed");
  });

  it("renameUserView ignores empty/whitespace names", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.addUserView({ name: "Keep", searchParams: "" });
    });
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.renameUserView(id, "   ");
    });
    expect(result.current.record.views[0].name).toBe("Keep");
  });

  it("updateUserViewParams canonicalizes new params", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.addUserView({ name: "Mixed", searchParams: "status=offline" });
    });
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.updateUserViewParams(id, "view=ignored&status=hashing&model=S21");
    });
    expect(result.current.record.views[0].searchParams).toBe("model=S21&status=hashing");
  });

  it("deleteUserView removes the entry", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.addUserView({ name: "A", searchParams: "" });
      result.current.addUserView({ name: "B", searchParams: "model=S21" });
    });
    const idA = result.current.record.views[0].id;
    act(() => {
      result.current.deleteUserView(idA);
    });
    expect(result.current.record.views.map((view) => view.name)).toEqual(["B"]);
  });

  it("reorderUserViews respects the supplied id order", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.addUserView({ name: "A", searchParams: "" });
      result.current.addUserView({ name: "B", searchParams: "model=S21" });
      result.current.addUserView({ name: "C", searchParams: "status=offline" });
    });
    const ids = result.current.record.views.map((view) => view.id);
    act(() => {
      result.current.reorderUserViews([ids[2], ids[0], ids[1]]);
    });
    expect(result.current.record.views.map((view) => view.name)).toEqual(["C", "A", "B"]);
  });

  it("dismissBuiltInView and restoreBuiltInView toggle the dismissal", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.dismissBuiltInView("offline");
    });
    expect(result.current.record.deletedBuiltInIds).toEqual(["offline"]);
    act(() => {
      result.current.dismissBuiltInView("offline");
    });
    expect(result.current.record.deletedBuiltInIds).toEqual(["offline"]);
    act(() => {
      result.current.restoreBuiltInView("offline");
    });
    expect(result.current.record.deletedBuiltInIds).toEqual([]);
  });

  it("dismissBuiltInView ignores unknown ids", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.dismissBuiltInView("not-a-builtin");
    });
    expect(result.current.record.deletedBuiltInIds).toEqual([]);
  });

  it("scopes records by username", () => {
    const alice = renderHook(() => useMinerViews("alice"));
    act(() => {
      alice.result.current.addUserView({ name: "Alice view", searchParams: "model=S21" });
    });
    expect(localStorage.getItem(KEY_ALICE)).toBeTruthy();
    expect(localStorage.getItem(KEY_BOB)).toBeNull();

    const bob = renderHook(() => useMinerViews("bob"));
    expect(bob.result.current.record.views).toEqual([]);
  });

  it("clearing storage restores built-ins (no dismissals on next read)", () => {
    const first = renderHook(() => useMinerViews("alice"));
    act(() => {
      first.result.current.dismissBuiltInView("offline");
    });
    expect(first.result.current.record.deletedBuiltInIds).toEqual(["offline"]);

    localStorage.clear();

    const second = renderHook(() => useMinerViews("alice"));
    expect(second.result.current.record.deletedBuiltInIds).toEqual([]);
  });

  it("removes the storage entry when reverting to default state", () => {
    const { result } = renderHook(() => useMinerViews("alice"));
    act(() => {
      result.current.addUserView({ name: "Temp", searchParams: "" });
    });
    expect(localStorage.getItem(KEY_ALICE)).not.toBeNull();
    const id = result.current.record.views[0].id;
    act(() => {
      result.current.deleteUserView(id);
    });
    expect(localStorage.getItem(KEY_ALICE)).toBeNull();
  });
});
