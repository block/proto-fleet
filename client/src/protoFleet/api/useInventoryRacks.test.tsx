import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import {
  EntityKind,
  type ListInventoryResponse,
  ListInventoryResponseSchema,
} from "./generated/fleetinventory/v1/fleetinventory_pb";
import { useInventoryRacks } from "./useInventoryRacks";
const { listInventory } = vi.hoisted(() => ({ listInventory: vi.fn() }));
vi.mock("./clients", () => ({ fleetInventoryClient: { listInventory } }));

describe("useInventoryRacks", () => {
  it("clears tanks and rejects late results when site intersection becomes empty", async () => {
    const response = create(ListInventoryResponseSchema, {
      items: [{ ref: { kind: EntityKind.TANK, id: 1n }, name: "Tank 1" }],
      totalCount: 1n,
    });
    listInventory.mockResolvedValueOnce(response);
    const { result, rerender } = renderHook(({ enabled }) => useInventoryRacks(enabled), {
      initialProps: { enabled: true },
    });
    await act(() => result.current.listRacks({ siteIds: [1n] }));
    expect(result.current.items).toHaveLength(1);
    let resolve!: (response: ListInventoryResponse) => void;
    listInventory.mockImplementationOnce(
      () =>
        new Promise<ListInventoryResponse>((done) => {
          resolve = done;
        }),
    );
    const onSuccess = vi.fn();
    let pending!: Promise<void>;
    act(() => {
      pending = result.current.listRacks({ siteIds: [1n], onSuccess });
    });
    const signal = listInventory.mock.calls[1][1].signal as AbortSignal;
    rerender({ enabled: false });
    expect(result.current.items).toEqual([]);
    expect(signal.aborted).toBe(true);
    await act(async () => {
      resolve(response);
      await pending;
    });
    expect(onSuccess).not.toHaveBeenCalled();
    expect(result.current.items).toEqual([]);
    rerender({ enabled: true });
    expect(result.current.items).toEqual([]);
  });
});
