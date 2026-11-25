import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { usePoolsInfo } from "./usePoolsInfo";
import { asPoolId, type PoolInfo, ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({ adapter: { getPools: vi.fn() } }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: true }),
}));

const deferred = () => {
  let resolve!: (pools: PoolInfo[]) => void;
  const promise = new Promise<PoolInfo[]>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
};
const pools = (name: string): PoolInfo[] => [{ id: asPoolId("pool:1"), priority: 0, name }];

describe("shared pool requests", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    useMinerStore.getState().resetDeviceData();
  });

  it.each([true, false])("clears each caller's loading state when older finishes first: %s", async (olderFirst) => {
    const older = deferred(),
      newer = deferred();
    mocks.adapter.getPools.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
    const settings = renderHook(() => usePoolsInfo());
    const shell = renderHook(() => usePoolsInfo({ poll: true }));
    expect(settings.result.current.pending).toBe(true);
    expect(shell.result.current.pending).toBe(true);
    if (olderFirst) {
      await act(async () => older.resolve(pools("Old")));
      expect(settings.result.current.pending).toBe(false);
      expect(shell.result.current.pending).toBe(true);
      expect(useMinerStore.getState().pools.resourceByOwner.get(ROOT_MODULE_KEY)?.status).toBe("loading");
      await act(async () => newer.resolve(pools("Current")));
    } else {
      await act(async () => newer.resolve(pools("Current")));
      await act(async () => older.resolve(pools("Old")));
    }
    expect(settings.result.current.pending).toBe(false);
    expect(shell.result.current.pending).toBe(false);
    expect(settings.result.current.data).toEqual(pools("Current"));
  });

  it("does not clear loading while a newer request from the same caller is still pending", async () => {
    const older = deferred(),
      newer = deferred();
    mocks.adapter.getPools.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
    const { result } = renderHook(() => usePoolsInfo());
    let refresh!: Promise<void> | undefined;
    act(() => {
      refresh = result.current.fetchData();
    });
    await act(async () => older.resolve(pools("Old")));
    expect(result.current.pending).toBe(true);
    await act(async () => {
      newer.resolve(pools("Current"));
      await refresh;
    });
    expect(result.current.pending).toBe(false);
    expect(result.current.data).toEqual(pools("Current"));
  });
});
