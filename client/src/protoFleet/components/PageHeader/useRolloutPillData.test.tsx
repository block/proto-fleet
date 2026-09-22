import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { useRolloutPillData } from "./useRolloutPillData";
import {
  ListRolloutsResponseSchema,
  RolloutSchema,
  RolloutService,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";

const { mockListRollouts, mockHandleAuthErrors, auth } = vi.hoisted(() => ({
  mockListRollouts: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  auth: { username: "operator", sessionGeneration: 1, isAuthenticated: true, permissions: ["miner:firmware_update"] },
}));

vi.mock("@/protoFleet/api/clients", () => ({ rolloutClient: { listRollouts: mockListRollouts } }));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mockHandleAuthErrors }),
  useHasPermission: (permission: string) => auth.permissions.includes(permission),
  useIsAuthenticated: () => auth.isAuthenticated,
  useUsername: () => auth.username,
  useSessionGeneration: () => auth.sessionGeneration,
  useFleetStore: { getState: () => ({ auth }) },
}));

const first = create(RolloutSchema, { id: 1n, status: RolloutStatus.ACTIVE, revision: 1n });
const second = create(RolloutSchema, { id: 2n, status: RolloutStatus.ACTIVE, revision: 1n });
const page = (rollouts = [first], cursor = "") => create(ListRolloutsResponseSchema, { rollouts, cursor });

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

async function tick() {
  await act(async () => vi.advanceTimersByTimeAsync(15_000));
}

describe("useRolloutPillData", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    auth.username = "operator";
    auth.sessionGeneration = 1;
    auth.isAuthenticated = true;
    auth.permissions = ["miner:firmware_update"];
    mockListRollouts.mockResolvedValue(page());
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("requests every active page and publishes only the complete scan", async () => {
    const lastPage = deferred<ReturnType<typeof page>>();
    mockListRollouts.mockResolvedValueOnce(page([first], "next-page")).mockReturnValueOnce(lastPage.promise);
    const { result } = renderHook(() => useRolloutPillData());
    await act(async () => {});
    expect(result.current).toEqual({ activeRollouts: [], hasVisiblePill: false });
    expect(mockListRollouts.mock.calls.map(([request]) => request)).toEqual([
      { status: RolloutStatus.ACTIVE, pageSize: 1000, cursor: "" },
      { status: RolloutStatus.ACTIVE, pageSize: 1000, cursor: "next-page" },
    ]);
    for (const [, options] of mockListRollouts.mock.calls) {
      expect(options).toEqual({ timeoutMs: 30_000, signal: expect.any(AbortSignal) });
    }
    await act(async () => lastPage.resolve(page([second])));
    expect(result.current).toEqual({ activeRollouts: [first, second], hasVisiblePill: true });
    mockListRollouts.mockResolvedValue(page([]));
    await tick();
    expect(result.current).toEqual({ activeRollouts: [], hasVisiblePill: false });
  });

  it("retains the previous complete scan when a later page fails and restarts at page one", async () => {
    const { result } = renderHook(() => useRolloutPillData());
    await act(async () => {});
    mockListRollouts
      .mockResolvedValueOnce(page([second], "partial-page"))
      .mockRejectedValueOnce(new ConnectError("offline", Code.Unavailable));
    await tick();
    expect(result.current.activeRollouts).toEqual([first]);
    mockListRollouts.mockResolvedValue(page([second]));
    await tick();
    expect(mockListRollouts.mock.lastCall?.[0].cursor).toBe("");
    expect(result.current.activeRollouts).toEqual([second]);
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  it("skips polling ticks while a scan is pending", async () => {
    const pending = deferred<ReturnType<typeof page>>();
    mockListRollouts.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(() => useRolloutPillData());
    await tick();
    await tick();
    expect(mockListRollouts).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve(page([])));
    expect(result.current.hasVisiblePill).toBe(false);
    await tick();
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    expect(result.current.activeRollouts).toEqual([first]);
  });

  it("aborts a stalled RPC at its deadline and allows a later polling tick to recover", async () => {
    const signals: AbortSignal[] = [];
    const client = createClient(
      RolloutService,
      createConnectTransport({
        baseUrl: "https://fleet.invalid",
        fetch: async (_input, init) => {
          const signal = init?.signal;
          if (!signal) throw new Error("Expected the Connect transport's AbortSignal");
          signals.push(signal);
          return new Promise<Response>((_resolve, reject) => {
            const abort = () => reject(signal.reason);
            if (signal.aborted) abort();
            else signal.addEventListener("abort", abort, { once: true });
          });
        },
      }),
    );
    const { result } = renderHook(() => useRolloutPillData());
    await act(async () => {});
    mockListRollouts.mockImplementation(client.listRollouts);
    await tick();
    expect(signals).toHaveLength(1);
    await act(async () => vi.advanceTimersByTimeAsync(29_999));
    expect(signals[0].aborted).toBe(false);
    expect(mockListRollouts).toHaveBeenCalledTimes(2);
    await act(async () => vi.advanceTimersByTimeAsync(1));
    expect(signals[0].aborted).toBe(true);
    expect(result.current.activeRollouts).toEqual([first]);
    mockListRollouts.mockResolvedValue(page([second]));
    await tick();
    expect(result.current.activeRollouts).toEqual([second]);
  });

  it.each([Code.PermissionDenied, Code.Unauthenticated, Code.InvalidArgument])(
    "clears cached data on permanent RPC error %s and routes authentication failures to logout",
    async (code) => {
      const { result } = renderHook(() => useRolloutPillData());
      await act(async () => {});
      const error = new ConnectError("request rejected", code);
      mockListRollouts.mockResolvedValueOnce(page([second], "next")).mockRejectedValueOnce(error);
      await tick();
      expect(result.current).toEqual({ activeRollouts: [], hasVisiblePill: false });
      expect(mockHandleAuthErrors).toHaveBeenCalledTimes(code === Code.Unauthenticated ? 1 : 0);
      if (code === Code.Unauthenticated) expect(mockHandleAuthErrors).toHaveBeenCalledWith({ error });
      mockListRollouts.mockRejectedValueOnce(new ConnectError("offline", Code.Unavailable));
      await tick();
      expect(result.current.hasVisiblePill).toBe(false);
      mockListRollouts.mockResolvedValue(page([second]));
      await tick();
      expect(result.current.activeRollouts).toEqual([second]);
    },
  );

  it.each(["hidden header", "missing permission", "logged out"])("does not poll with %s", async (reason) => {
    if (reason === "missing permission") auth.permissions = [];
    if (reason === "logged out") auth.isAuthenticated = false;
    const { result } = renderHook(() => useRolloutPillData({ enabled: reason !== "hidden header" }));
    await tick();
    expect(mockListRollouts).not.toHaveBeenCalled();
    expect(result.current).toEqual({ activeRollouts: [], hasVisiblePill: false });
  });

  it.each(["hidden header", "permission revoked"])(
    "does not redisplay cached data after %s and re-enabling",
    async (reason) => {
      const { result, rerender } = renderHook(({ enabled }) => useRolloutPillData({ enabled }), {
        initialProps: { enabled: true },
      });
      await act(async () => {});
      expect(result.current.activeRollouts).toEqual([first]);
      if (reason === "permission revoked") auth.permissions = [];
      rerender({ enabled: reason !== "hidden header" });
      expect(result.current.hasVisiblePill).toBe(false);
      const pending = deferred<ReturnType<typeof page>>();
      mockListRollouts.mockReturnValueOnce(pending.promise);
      auth.permissions = ["miner:firmware_update"];
      rerender({ enabled: true });
      expect(result.current).toEqual({ activeRollouts: [], hasVisiblePill: false });
      await act(async () => pending.resolve(page([second])));
      expect(result.current.activeRollouts).toEqual([second]);
    },
  );

  it.each(["success", "unauthenticated"])(
    "ignores a previous login's late %s without blocking the new scan",
    async (lateResult) => {
      const old = deferred<ReturnType<typeof page>>();
      const current = deferred<ReturnType<typeof page>>();
      mockListRollouts.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise);
      const { result, rerender } = renderHook(() => useRolloutPillData());
      const signal = mockListRollouts.mock.calls[0][1].signal as AbortSignal;
      auth.sessionGeneration += 1;
      rerender();
      expect(signal.aborted).toBe(true);
      await act(async () => current.resolve(page([second])));
      expect(result.current.activeRollouts).toEqual([second]);
      await act(async () => {
        if (lateResult === "success") old.resolve(page([first], "must-not-fetch"));
        else old.reject(new ConnectError("old login expired", Code.Unauthenticated));
      });
      expect(result.current.activeRollouts).toEqual([second]);
      expect(mockListRollouts).toHaveBeenCalledTimes(2);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
      await tick();
      expect(mockListRollouts).toHaveBeenCalledTimes(3);
    },
  );

  it.each(["unmount", "new login before rerender", "permission revoked before rerender"])(
    "does not continue pagination after %s",
    async (change) => {
      const pending = deferred<ReturnType<typeof page>>();
      mockListRollouts.mockReturnValueOnce(pending.promise);
      const { result, unmount } = renderHook(() => useRolloutPillData());
      const signal = mockListRollouts.mock.calls[0][1].signal as AbortSignal;
      if (change === "unmount") unmount();
      else if (change === "new login before rerender") auth.sessionGeneration += 1;
      else auth.permissions = [];
      await act(async () => pending.resolve(page([first], "must-not-fetch")));
      expect(signal.aborted).toBe(true);
      expect(mockListRollouts).toHaveBeenCalledTimes(1);
      expect(result.current.hasVisiblePill).toBe(false);
      expect(mockHandleAuthErrors).not.toHaveBeenCalled();
    },
  );
});
