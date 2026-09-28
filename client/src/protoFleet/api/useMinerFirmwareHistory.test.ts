import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { MinerFirmwareHistoryEntrySchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { useMinerFirmwareHistory } from "@/protoFleet/api/useMinerFirmwareHistory";
import { useFleetStore } from "@/protoFleet/store";

const listHistory = vi.hoisted(() => vi.fn());
vi.mock("@/protoFleet/api/clients", () => ({
  rolloutClient: { listMinerFirmwareHistory: listHistory },
}));

const initialAuth = useFleetStore.getState().auth;
const entry = (rolloutId: bigint) => create(MinerFirmwareHistoryEntrySchema, { rolloutId });
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
const page = (ids: bigint[], cursor = "") => ({ entries: ids.map(entry), cursor });

beforeEach(() => {
  listHistory.mockReset().mockResolvedValue(page([]));
  useFleetStore.setState({
    auth: {
      ...initialAuth,
      username: "operator",
      sessionGeneration: 1,
      isAuthenticated: true,
      permissions: ["miner:firmware_update"],
    },
  });
});
afterEach(() => {
  cleanup();
  useFleetStore.setState({ auth: initialAuth });
});

describe("useMinerFirmwareHistory", () => {
  it("reads bounded pages only on demand and refreshes from the newest page", async () => {
    listHistory.mockResolvedValueOnce(page([3n], "older")).mockResolvedValueOnce(page([2n]));
    const { result } = renderHook(() => useMinerFirmwareHistory("miner-a"));
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    expect(listHistory).toHaveBeenCalledTimes(1);
    expect(listHistory).toHaveBeenLastCalledWith(
      { deviceIdentifier: "miner-a", pageSize: 50, cursor: "" },
      { timeoutMs: 30_000, signal: expect.any(AbortSignal) },
    );
    expect(result.current.hasMore).toBe(true);
    act(() => {
      result.current.loadMore();
      result.current.loadMore();
    });
    await waitFor(() => expect(result.current.entries.map((row) => row.rolloutId)).toEqual([3n, 2n]));
    expect(listHistory).toHaveBeenCalledTimes(2);
    expect(listHistory.mock.calls[1][0].cursor).toBe("older");
    expect(result.current.hasMore).toBe(false);
    listHistory.mockResolvedValueOnce(page([4n], "next"));
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.entries.map((row) => row.rolloutId)).toEqual([4n]));
    expect(listHistory.mock.calls[2][0].cursor).toBe("");
  });

  it("retries initial and later-page errors without discarding loaded history", async () => {
    listHistory.mockRejectedValueOnce(new Error("History unavailable"));
    const { result } = renderHook(() => useMinerFirmwareHistory("miner-a"));
    await waitFor(() => expect(result.current.error).toBe("History unavailable"));
    expect(result.current.hasLoaded).toBe(false);
    listHistory.mockResolvedValueOnce(page([3n], "older"));
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    listHistory.mockRejectedValueOnce(new Error("Older history unavailable"));
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.error).toBe("Older history unavailable"));
    expect(result.current.entries.map((row) => row.rolloutId)).toEqual([3n]);
    listHistory.mockResolvedValueOnce(page([2n]));
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.entries.map((row) => row.rolloutId)).toEqual([3n, 2n]));
    expect(listHistory.mock.calls[3][0].cursor).toBe("older");
    expect(result.current.error).toBeNull();
  });

  it("clears the previous miner and aborts its pending page when the identifier changes", async () => {
    const older = deferred<ReturnType<typeof page>>();
    listHistory.mockResolvedValueOnce(page([3n], "older")).mockReturnValueOnce(older.promise);
    const { result, rerender } = renderHook(({ id }) => useMinerFirmwareHistory(id), { initialProps: { id: "a" } });
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    act(() => result.current.loadMore());
    const oldSignal = listHistory.mock.calls[1][1].signal as AbortSignal;
    listHistory.mockResolvedValueOnce(page([7n]));
    rerender({ id: "b" });
    expect(oldSignal.aborted).toBe(true);
    expect(result.current.entries).toEqual([]);
    await waitFor(() => expect(result.current.entries.map((row) => row.rolloutId)).toEqual([7n]));
    await act(async () => older.resolve(page([2n])));
    expect(result.current.entries.map((row) => row.rolloutId)).toEqual([7n]);
  });

  it.each([Code.PermissionDenied, Code.NotFound, Code.InvalidArgument])(
    "clears loaded history after a permanent server error (%s) and retries from the beginning",
    async (code) => {
      listHistory.mockResolvedValueOnce(page([3n], "older"));
      const { result } = renderHook(() => useMinerFirmwareHistory("a"));
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      listHistory.mockRejectedValueOnce(new ConnectError("History unavailable", code));
      act(() => result.current.loadMore());
      await waitFor(() => expect(result.current.error).toContain("History unavailable"));
      expect(result.current.entries).toEqual([]);
      expect(result.current.hasLoaded).toBe(false);
      expect(result.current.hasMore).toBe(false);
      listHistory.mockResolvedValueOnce(page([]));
      act(() => result.current.retry());
      await waitFor(() => expect(result.current.hasLoaded).toBe(true));
      expect(listHistory.mock.calls[2][0].cursor).toBe("");
    },
  );

  it("requires permission, hides loaded data on revocation and rejects late results", async () => {
    useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, permissions: [] } });
    const { result } = renderHook(() => useMinerFirmwareHistory("a"));
    expect(result.current.canRead).toBe(false);
    expect(result.current.isLoading).toBe(false);
    expect(listHistory).not.toHaveBeenCalled();
    listHistory.mockResolvedValueOnce(page([3n], "older"));
    act(() =>
      useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, permissions: ["miner:firmware_update"] } }),
    );
    await waitFor(() => expect(result.current.hasLoaded).toBe(true));
    const older = deferred<ReturnType<typeof page>>();
    listHistory.mockReturnValueOnce(older.promise);
    act(() => result.current.loadMore());
    const signal = listHistory.mock.calls[1][1].signal as AbortSignal;
    act(() => useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, permissions: [] } }));
    expect(result.current.entries).toEqual([]);
    expect(signal.aborted).toBe(true);
    await act(async () => older.resolve(page([2n])));
    expect(result.current.entries).toEqual([]);
    expect(result.current.canRead).toBe(false);
  });

  it("rejects a stale authentication error after the same user starts a new session", async () => {
    const stale = deferred<ReturnType<typeof page>>();
    listHistory.mockReturnValueOnce(stale.promise);
    const { result } = renderHook(() => useMinerFirmwareHistory("a"));
    const signal = listHistory.mock.calls[0][1].signal as AbortSignal;
    listHistory.mockResolvedValueOnce(page([8n]));
    act(() => useFleetStore.setState({ auth: { ...useFleetStore.getState().auth, sessionGeneration: 2 } }));
    expect(signal.aborted).toBe(true);
    await waitFor(() => expect(result.current.entries.map((row) => row.rolloutId)).toEqual([8n]));
    await act(async () => stale.reject(new ConnectError("Expired", Code.Unauthenticated)));
    expect(useFleetStore.getState().auth.isAuthenticated).toBe(true);
    expect(result.current.error).toBeNull();
  });

  it("aborts pending reads when closed", () => {
    listHistory.mockReturnValueOnce(new Promise(() => undefined));
    const { unmount } = renderHook(() => useMinerFirmwareHistory("a"));
    const signal = listHistory.mock.calls[0][1].signal as AbortSignal;
    unmount();
    expect(signal.aborted).toBe(true);
  });
});
