import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { GetModuleHashboardsResponseSchema } from "./generated/fleetinventory/v1/fleetinventory_pb";
import { useModuleHashboards } from "./useModuleHashboards";

const { getModuleHashboards } = vi.hoisted(() => ({ getModuleHashboards: vi.fn() }));
vi.mock("./clients", () => ({ fleetInventoryClient: { getModuleHashboards } }));

describe("useModuleHashboards", () => {
  it("keeps other expanded modules visible while adding or removing a module", async () => {
    getModuleHashboards.mockResolvedValueOnce(
      create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 1n }] }),
    );
    let finish!: (value: unknown) => void;
    getModuleHashboards.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { result, rerender } = renderHook(({ ids }) => useModuleHashboards(ids, "page"), {
      initialProps: { ids: [1n] },
    });
    await waitFor(() => expect(result.current.modules["1"]?.data).toBeDefined());
    rerender({ ids: [1n, 2n] });
    expect(result.current.modules["1"]?.data).toBeDefined();
    expect(result.current.modules["2"]).toBeUndefined();
    await act(async () =>
      finish(create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 1n }, { moduleId: 2n }] })),
    );
    getModuleHashboards.mockImplementationOnce(() => new Promise(() => {}));
    rerender({ ids: [1n] });
    expect(result.current.modules["1"]?.data).toBeDefined();
    expect(result.current.modules["2"]).toBeUndefined();
  });

  it("shows loading instead of resurrecting old boards when re-expanded", async () => {
    getModuleHashboards.mockResolvedValueOnce(
      create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 1n }] }),
    );
    let finish!: (value: unknown) => void;
    getModuleHashboards.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { result, rerender } = renderHook(({ ids }) => useModuleHashboards(ids, "page"), {
      initialProps: { ids: [1n] },
    });
    await waitFor(() => expect(result.current.modules["1"].data).toBeDefined());
    rerender({ ids: [] });
    rerender({ ids: [1n] });
    expect(result.current.modules["1"]).toBeUndefined();
    await act(async () => {
      finish(create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 1n }] }));
    });
    await waitFor(() => expect(result.current.modules["1"].data).toBeDefined());
  });
  beforeEach(() => getModuleHashboards.mockReset());

  it("loads only expanded modules and maps responses by module ID", async () => {
    getModuleHashboards.mockResolvedValue(
      create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 2n }, { moduleId: 1n }] }),
    );
    const { result, rerender } = renderHook(({ ids }) => useModuleHashboards(ids, "page"), {
      initialProps: { ids: [] as bigint[] },
    });
    expect(getModuleHashboards).not.toHaveBeenCalled();
    rerender({ ids: [1n, 2n] });
    await waitFor(() => expect(result.current.modules["1"].data?.moduleId).toBe(1n));
    expect(result.current.modules["2"].data?.moduleId).toBe(2n);
    expect(getModuleHashboards.mock.calls[0][0]).toEqual({ moduleIds: [1n, 2n] });
  });

  it("discards a cancelled response after navigation", async () => {
    let finish!: (response: ReturnType<typeof create<typeof GetModuleHashboardsResponseSchema>>) => void;
    getModuleHashboards.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    getModuleHashboards.mockResolvedValue(create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 2n }] }));
    const { result, rerender } = renderHook(({ id, scope }) => useModuleHashboards([id], scope), {
      initialProps: { id: 1n, scope: "a" },
    });
    rerender({ id: 2n, scope: "b" });
    await waitFor(() => expect(result.current.modules["2"].data?.moduleId).toBe(2n));
    await act(async () => {
      finish(create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 1n }] }));
    });
    expect(result.current.modules["1"]).toBeUndefined();
    expect((getModuleHashboards.mock.calls[0][1].signal as AbortSignal).aborted).toBe(true);
  });

  it("clears current data on errors and retries", async () => {
    getModuleHashboards.mockRejectedValueOnce(new Error("unavailable"));
    getModuleHashboards.mockResolvedValue(create(GetModuleHashboardsResponseSchema, { modules: [{ moduleId: 1n }] }));
    const { result } = renderHook(() => useModuleHashboards([1n], "page"));
    await waitFor(() => expect(result.current.modules["1"].error).toBeDefined());
    expect(result.current.modules["1"].data).toBeUndefined();
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.modules["1"].data?.moduleId).toBe(1n));
  });
});
