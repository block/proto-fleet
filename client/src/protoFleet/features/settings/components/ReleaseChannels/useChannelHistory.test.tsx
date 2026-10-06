import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { deferred } from "./__tests__/helpers";
import { useChannelHistory } from "./useChannelHistory";
import { type Rollout, RolloutSchema, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useFleetStore } from "@/protoFleet/store";

const initialAuth = useFleetStore.getState().auth;
beforeEach(() =>
  useFleetStore.setState({
    auth: { ...initialAuth, isAuthenticated: true, username: "operator", sessionGeneration: 1 },
  }),
);
afterEach(() => {
  cleanup();
  useFleetStore.setState({ auth: initialAuth });
});
const flush = () => act(async () => {});
const pendingHistory = deferred<Rollout[]>;
const historical = create(RolloutSchema, {
  id: 10n,
  channelId: 1n,
  revision: 2n,
  status: RolloutStatus.COMPLETED_WITH_FAILURES,
});

describe("on-demand channel history", () => {
  it("bounds complete scans and queues retries without blocking peers after a failure", async () => {
    const scans = new Map<bigint, ReturnType<typeof pendingHistory>>();
    let active = 0;
    let peak = 0;
    const loader = vi.fn((id: bigint) => {
      const scan = pendingHistory();
      scans.set(id, scan);
      peak = Math.max(peak, ++active);
      return scan.promise.finally(() => active--);
    });
    const ids = [1n, 2n, 3n, 4n, 5n, 6n, 7n];
    const { result } = renderHook(() =>
      useChannelHistory({ channelIds: ids, rollouts: [], listChannelRollouts: loader }),
    );
    expect(loader.mock.calls.map(([id]) => id)).toEqual([1n, 2n, 3n, 4n]);
    expect(ids.map((id) => result.current.states.get(id)?.status)).toEqual(ids.map(() => "loading"));

    await act(async () => {
      scans.get(1n)!.resolve([historical, { ...historical, id: 99n, channelId: 99n }]);
      scans.get(2n)!.reject(new Error("History unavailable"));
    });
    expect(result.current.rollouts).toEqual([historical]);
    expect(result.current.states.get(1n)?.status).toBe("ready");
    expect(result.current.states.get(2n)).toMatchObject({ status: "error", error: "History unavailable" });
    expect(loader.mock.calls.map(([id]) => id)).toEqual([1n, 2n, 3n, 4n, 5n, 6n]);
    act(() => result.current.retry(2n));
    expect(result.current.states.get(2n)?.status).toBe("loading");
    expect(loader).toHaveBeenCalledTimes(6);

    await act(async () => scans.get(3n)!.resolve([]));
    expect(loader.mock.calls.map(([id]) => id)).toEqual([1n, 2n, 3n, 4n, 5n, 6n, 2n]);
    await act(async () => scans.get(2n)!.resolve([]));
    expect(loader).toHaveBeenLastCalledWith(7n, expect.any(AbortSignal));
    await act(async () => {
      for (const id of [4n, 5n, 6n, 7n]) scans.get(id)!.resolve([]);
    });
    expect(ids.map((id) => result.current.states.get(id)?.status)).toEqual(ids.map(() => "ready"));
    expect(peak).toBe(4);
    expect(active).toBe(0);
    expect(loader).toHaveBeenCalledTimes(8);
  });

  it("loads only demanded channels and keeps completed history across new core snapshots and reopens", async () => {
    const loader = vi.fn().mockResolvedValue([historical]);
    const { result, rerender } = renderHook(
      ({ ids, rows }) => useChannelHistory({ channelIds: ids, rollouts: rows, listChannelRollouts: loader }),
      {
        initialProps: { ids: [] as bigint[], rows: [] as Rollout[] },
      },
    );
    expect(loader).not.toHaveBeenCalled();
    rerender({ ids: [1n], rows: [] });
    await flush();
    expect(loader).toHaveBeenCalledExactlyOnceWith(1n, expect.any(AbortSignal));
    expect(result.current.states.get(1n)?.status).toBe("ready");
    expect(result.current.rollouts).toEqual([historical]);
    const newer = { ...historical, revision: 3n, status: RolloutStatus.COMPLETED };
    rerender({ ids: [1n], rows: [newer] });
    expect(result.current.rollouts).toEqual([newer]);
    rerender({ ids: [], rows: [newer] });
    rerender({ ids: [1n], rows: [{ ...newer, channelName: "Updated name" }] });
    await flush();
    expect(loader).toHaveBeenCalledOnce();
    expect(result.current.rollouts).toEqual([{ ...newer, channelName: "Updated name" }]);
  });

  it("does not restart retained demand when another channel opens, and cancels only closed requests", async () => {
    const first = pendingHistory();
    const second = pendingHistory();
    const loader = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const { result, rerender } = renderHook(
      ({ ids }) => useChannelHistory({ channelIds: ids, rollouts: [], listChannelRollouts: loader }),
      { initialProps: { ids: [1n] } },
    );
    rerender({ ids: [1n, 2n] });
    expect(loader).toHaveBeenCalledTimes(2);
    const firstSignal = loader.mock.calls[0][1] as AbortSignal;
    const secondSignal = loader.mock.calls[1][1] as AbortSignal;
    rerender({ ids: [2n] });
    expect(firstSignal.aborted).toBe(true);
    expect(secondSignal.aborted).toBe(false);
    await act(async () => first.reject(new Error("Old request failed")));
    expect(result.current.states.has(1n)).toBe(false);
    const row = { ...historical, channelId: 2n };
    await act(async () => second.resolve([row]));
    expect(result.current.rollouts).toEqual([row]);
    expect(result.current.states.get(2n)?.status).toBe("ready");
  });

  it("keeps core rows after history failure and retries without letting an older history revision replace them", async () => {
    const loader = vi.fn().mockRejectedValueOnce(new Error("History unavailable")).mockResolvedValueOnce([historical]);
    const newer = { ...historical, revision: 3n, status: RolloutStatus.COMPLETED };
    const { result } = renderHook(() =>
      useChannelHistory({ channelIds: [1n], rollouts: [newer], listChannelRollouts: loader }),
    );
    await flush();
    expect(result.current.states.get(1n)).toMatchObject({ status: "error", error: "History unavailable" });
    expect(result.current.rollouts).toEqual([newer]);
    act(() => result.current.retry(1n));
    await flush();
    expect(loader).toHaveBeenCalledTimes(2);
    expect(result.current.states.get(1n)?.status).toBe("ready");
    expect(result.current.rollouts).toEqual([newer]);
  });

  it.each(["unmount", "new login"])("aborts pending history after %s and ignores late results", async (change) => {
    const old = pendingHistory();
    const loader = vi.fn().mockReturnValueOnce(old.promise).mockResolvedValueOnce([]);
    const { result, unmount } = renderHook(() =>
      useChannelHistory({ channelIds: [1n], rollouts: [], listChannelRollouts: loader }),
    );
    const signal = loader.mock.calls[0][1] as AbortSignal;
    if (change === "unmount") unmount();
    else act(() => useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, sessionGeneration: 2 } }));
    expect(signal.aborted).toBe(true);
    await act(async () => old.resolve([historical]));
    if (change === "new login") {
      expect(loader).toHaveBeenCalledTimes(2);
      expect(result.current.rollouts).toEqual([]);
      expect(result.current.states.get(1n)?.status).toBe("ready");
    }
  });
});
