import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  EntityKind,
  InventoryView,
  ListInventoryResponseSchema,
} from "./generated/fleetinventory/v1/fleetinventory_pb";
import { useFleetInventory } from "./useFleetInventory";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
const { listInventory } = vi.hoisted(() => ({ listInventory: vi.fn() }));
vi.mock("./clients", () => ({ fleetInventoryClient: { listInventory } }));

describe("useFleetInventory", () => {
  beforeEach(() => {
    listInventory.mockReset();
  });

  it("uses the mixed server page and resets pagination on scope changes", async () => {
    listInventory
      .mockResolvedValueOnce(
        create(ListInventoryResponseSchema, {
          items: [{ ref: { kind: EntityKind.MODULE, id: 1n }, name: "Module" }],
          totalCount: 2n,
          nextPageToken: "next",
        }),
      )
      .mockResolvedValueOnce(
        create(ListInventoryResponseSchema, {
          items: [{ ref: { kind: EntityKind.MINER, id: 1n }, name: "Miner" }],
          totalCount: 2n,
        }),
      )
      .mockResolvedValueOnce(create(ListInventoryResponseSchema, { totalCount: 0n }));
    const { result, rerender } = renderHook(
      ({ site }) => useFleetInventory({ view: InventoryView.MINING_ASSETS, siteIds: [site] }),
      { initialProps: { site: 1n } },
    );
    await waitFor(() => expect(result.current.items[0]?.name).toBe("Module"));
    act(() => result.current.nextPage());
    await waitFor(() => expect(result.current.items[0]?.name).toBe("Miner"));
    expect(listInventory.mock.calls[1][0].pageToken).toBe("next");
    rerender({ site: 2n });
    expect(result.current.items).toEqual([]);
    expect(result.current.currentPage).toBe(0);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(listInventory.mock.calls[2][0].pageToken).toBe("");
  });

  it("refreshes facility rows and the unfiltered count immediately after site assignments", async () => {
    let moved = false;
    listInventory.mockImplementation(({ pageSize, pageToken }) =>
      Promise.resolve(
        create(ListInventoryResponseSchema, {
          items:
            pageSize === 100 && !moved
              ? [{ ref: { kind: EntityKind.CONTAINER, id: 1n }, name: "North container" }]
              : [],
          totalCount: pageSize === 100 ? (moved ? 0n : 2n) : moved ? 1n : 3n,
          nextPageToken: pageSize === 100 && !pageToken && !moved ? "next" : "",
        }),
      ),
    );
    const { result } = renderHook(() => ({
      filtered: useFleetInventory({ view: InventoryView.FACILITIES, pageSize: 100, siteIds: [1n] }),
      unfiltered: useFleetInventory({ view: InventoryView.FACILITIES, pageSize: 1, siteIds: [1n] }),
    }));
    await waitFor(() => expect(result.current.unfiltered.total).toBe(3));
    expect(result.current.filtered.items[0]?.name).toBe("North container");
    act(() => result.current.filtered.nextPage());
    await waitFor(() => expect(result.current.filtered.loading).toBe(false));
    expect(result.current.filtered.currentPage).toBe(1);
    listInventory.mockClear();

    moved = true;
    act(() => useFleetStore.getState().ui.bumpSitesRevision());
    expect(result.current.filtered.items).toEqual([]);
    expect(result.current.filtered.currentPage).toBe(0);
    await waitFor(() => expect(result.current.unfiltered.total).toBe(1));
    expect(result.current.filtered.total).toBe(0);
    expect(listInventory).toHaveBeenCalledTimes(2);
    expect(listInventory.mock.calls.map(([request]) => request.pageToken)).toEqual(["", ""]);
  });
});
