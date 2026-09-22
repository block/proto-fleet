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
    auth: { ...initialAuth, username: "operator", sessionGeneration: 1, isAuthenticated: true },
  }),
);
afterEach(() => {
  cleanup();
  useFleetStore.setState({ auth: initialAuth });
});

function pendingLoader() {
  const requests: { channelId: bigint; signal: AbortSignal; result: ReturnType<typeof deferred<Rollout[]>> }[] = [];
  const load = vi.fn((channelId: bigint, signal?: AbortSignal) => {
    const result = deferred<Rollout[]>();
    requests.push({ channelId, signal: signal!, result });
    return result.promise;
  });
  return { load, requests };
}

const historyRow = (channelId: bigint, id: bigint) =>
  create(RolloutSchema, {
    channelId,
    id,
    revision: 1n,
    status: RolloutStatus.COMPLETED,
  });
const ids = [1n, 2n, 3n, 4n, 5n, 6n];

describe("channel history queue lifecycle", () => {
  it("drops collapsed queued demand and waits for an aborted active scan to drain before reopening it", async () => {
    const loader = pendingLoader();
    const { result, rerender } = renderHook(
      ({ demand }) =>
        useChannelHistory({
          channelIds: demand,
          rollouts: [],
          listChannelRollouts: loader.load,
        }),
      { initialProps: { demand: ids } },
    );
    expect(loader.requests.map((request) => request.channelId)).toEqual([1n, 2n, 3n, 4n]);

    rerender({ demand: [2n, 3n, 4n, 6n] });
    expect(loader.requests[0].signal.aborted).toBe(true);
    expect(loader.requests.slice(1).every((request) => !request.signal.aborted)).toBe(true);
    expect(result.current.states.has(1n)).toBe(false);
    expect(result.current.states.has(5n)).toBe(false);
    expect(loader.load).toHaveBeenCalledTimes(4);
    rerender({ demand: [1n, 2n, 3n, 4n, 6n] });
    expect(result.current.states.get(1n)?.status).toBe("loading");
    expect(loader.load).toHaveBeenCalledTimes(4);

    await act(async () => loader.requests[1].result.resolve([]));
    expect(loader.requests.map((request) => request.channelId)).toEqual([1n, 2n, 3n, 4n, 6n]);
    const obsolete = historyRow(1n, 100n);
    await act(async () => loader.requests[0].result.resolve([obsolete]));
    expect(loader.requests.map((request) => request.channelId)).toEqual([1n, 2n, 3n, 4n, 6n, 1n]);
    expect(result.current.states.get(1n)?.status).toBe("loading");
    expect(result.current.rollouts).not.toContainEqual(obsolete);
    const current = historyRow(1n, 101n);
    await act(async () => loader.requests[5].result.resolve([current]));
    expect(result.current.states.get(1n)?.status).toBe("ready");
    expect(result.current.rollouts).toEqual([current]);
    expect(loader.requests.some((request) => request.channelId === 5n)).toBe(false);
  });

  it.each(["unmount", "new login", "new loader"] as const)(
    "isolates queued and draining work after %s",
    async (change) => {
      const old = pendingLoader();
      const replacement = pendingLoader();
      const { result, rerender, unmount } = renderHook(
        ({ load }) =>
          useChannelHistory({
            channelIds: ids,
            rollouts: [],
            listChannelRollouts: load,
          }),
        { initialProps: { load: old.load } },
      );
      expect(old.requests).toHaveLength(4);
      const abandoned = [...old.requests];
      if (change === "unmount") unmount();
      else if (change === "new loader") rerender({ load: replacement.load });
      else act(() => useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, sessionGeneration: 2 } }));
      expect(abandoned.every((request) => request.signal.aborted)).toBe(true);

      const currentRequests = change === "new loader" ? replacement.requests : old.requests.slice(4);
      if (change !== "unmount") {
        expect(currentRequests).toHaveLength(4);
        expect(currentRequests.every((request) => !request.signal.aborted)).toBe(true);
      }
      await act(async () => {
        abandoned.forEach((request, index) => {
          if (index === 0) request.result.reject(new Error("Old history failed"));
          else request.result.resolve([historyRow(request.channelId, BigInt(100 + index))]);
        });
      });
      expect(old.requests).toHaveLength(change === "new login" ? 8 : 4);
      expect(replacement.requests).toHaveLength(change === "new loader" ? 4 : 0);
      if (change === "unmount") return;
      expect(result.current.rollouts).toEqual([]);
      for (const id of ids) expect(result.current.states.get(id)?.status).toBe("loading");

      const row = historyRow(currentRequests[0].channelId, 200n);
      await act(async () => currentRequests[0].result.resolve([row]));
      expect(result.current.rollouts).toEqual([row]);
      expect(result.current.states.get(row.channelId)?.status).toBe("ready");
      const currentCalls = change === "new loader" ? replacement.requests : old.requests.slice(4);
      expect(currentCalls.map((request) => request.channelId)).toEqual([1n, 2n, 3n, 4n, 5n]);
    },
  );
});
