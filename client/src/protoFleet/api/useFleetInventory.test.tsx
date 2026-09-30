import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  EntityKind,
  InventoryView,
  ListInventoryResponseSchema,
} from "./generated/fleetinventory/v1/fleetinventory_pb";
import { useFleetInventory } from "./useFleetInventory";
const { listInventory } = vi.hoisted(() => ({ listInventory: vi.fn() }));
vi.mock("./clients", () => ({ fleetInventoryClient: { listInventory } }));

describe("useFleetInventory", () => {
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
});
