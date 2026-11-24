import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { configurationShares, usePoolConfigurationStatistics } from "./usePoolConfigurationStatistics";
import type { PoolHistoryResult } from "@/protoOS/api/adapters/poolHistory";
import type {
  PoolConfigurationReadings,
  PoolConfigurationStatisticsService,
  PoolConfigurationSummary,
} from "@/protoOS/api/adapters/poolStatistics";
import { asPoolId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({ adapter: { getPoolHistory: vi.fn() } }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ adapter: mocks.adapter }) }));
vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry:
    () =>
    async ({
      request,
      signal,
      onSuccess,
      onError,
    }: {
      request: (options: { signal: AbortSignal }) => Promise<unknown>;
      signal: AbortSignal;
      onSuccess: (data: unknown) => void;
      onError: () => void;
    }) => {
      try {
        onSuccess(await request({ signal }));
      } catch {
        if (!signal.aborted) onError();
      }
    },
}));
const configurations: PoolConfigurationSummary[] = Array.from({ length: 26 }, (_, index) => ({
  id: `set-${index}`,
  name: `Configuration ${index}`,
  revision: "1",
  assignedCount: 3,
}));
const reading = (configuration: PoolConfigurationSummary) => ({
  ...configuration,
  pools: [
    {
      id: asPoolId(`pool:${10 + Number(configuration.id.replace("set-", ""))}`),
      name: "Primary",
      username: configuration.id,
      url: "stratum+tcp://same:3333",
      priority: 0,
    },
  ],
});
const fixture = () => ({
  getMiningGroupsEnabled: vi.fn().mockResolvedValue(true),
  list: vi.fn().mockResolvedValue({ enabled: true, configurations }),
  read: vi.fn().mockImplementation(async (entries: PoolConfigurationSummary[]) => entries.map(reading)),
});
const deferNextList = (service: ReturnType<typeof fixture>) => {
  type ListResult = Awaited<ReturnType<PoolConfigurationStatisticsService["list"]>>;
  let resolve!: (value: ListResult) => void;
  service.list.mockReturnValueOnce(
    new Promise<ListResult>((resolveList) => {
      resolve = resolveList;
    }),
  );
  return resolve;
};
beforeEach(() => {
  vi.useFakeTimers();
  useMinerStore.getState().resetDeviceData();
  mocks.adapter.getPoolHistory.mockReset();
  mocks.adapter.getPoolHistory.mockImplementation(async ({ poolIds }: { poolIds: string[] }) => ({
    byPool: Object.fromEntries(
      poolIds.map((id) => [id, { accepted: Number(id.replace("pool:", "")), rejected: 1, partial: false }]),
    ),
    unavailable: [],
  }));
});
afterEach(() => vi.useRealTimers());
test("uses stored disabled mode until the list reports that mining groups are enabled", async () => {
  const service = fixture();
  const finishList = deferNextList(service);
  useMinerStore.getState().pools.setConfigurationMode(service, false);
  const { result } = renderHook(() => usePoolConfigurationStatistics(service, "24h", 0));

  expect(service.list).toHaveBeenCalledTimes(1);
  expect(result.current.enabled).toBe(false);
  expect(result.current.configurations).toEqual([]);
  expect(service.read).not.toHaveBeenCalled();

  const assignedConfigurations = [configurations[0]];
  await act(async () => finishList({ enabled: true, configurations: assignedConfigurations }));

  expect(useMinerStore.getState().pools.configurationMode?.value).toBe(true);
  expect(result.current.enabled).toBe(true);
  expect(result.current.configurations).toEqual(assignedConfigurations);
  expect(result.current.readings).toEqual(assignedConfigurations.map(reading));
});
test("keeps enabled mode and configuration data when the list refresh fails", async () => {
  const service = fixture();
  const { result } = renderHook(() => usePoolConfigurationStatistics(service, "24h", 0, "set-0"));
  await act(async () => {});
  const { history } = result.current;
  expect(history?.byPool[asPoolId("pool:10")]?.accepted).toBe(10);

  service.list.mockRejectedValueOnce(new Error("list unavailable"));
  await act(() => vi.advanceTimersByTimeAsync(30_000));

  expect(service.list).toHaveBeenCalledTimes(2);
  expect(useMinerStore.getState().pools.configurationMode?.value).toBe(true);
  expect(result.current.enabled).toBe(true);
  expect(result.current.configurations).toEqual(configurations);
  expect(result.current.readings).toEqual([reading(configurations[0])]);
  expect(result.current.history).toEqual(history);
  expect(result.current.error).toContain("Displayed values may be out of date");
});
test("leaves mode unknown until the first list returns", async () => {
  const service = fixture();
  const finishList = deferNextList(service);
  const { result } = renderHook(() => usePoolConfigurationStatistics(service, "24h", 0));

  expect(service.list).toHaveBeenCalledTimes(1);
  expect(result.current.enabled).toBeUndefined();
  expect(result.current.configurations).toEqual([]);
  expect(result.current.error).toBeUndefined();
  expect(service.read).not.toHaveBeenCalled();

  await act(async () => finishList({ enabled: false, configurations: [] }));

  expect(result.current.enabled).toBe(false);
});
test("polling is stable across renders and requests only the visible page", async () => {
  const service = fixture();
  const hook = renderHook(({ page }) => usePoolConfigurationStatistics(service, "24h", page), {
    initialProps: { page: 0 },
  });
  await act(async () => {});
  expect(service.list).toHaveBeenCalledTimes(1);
  expect(service.read).toHaveBeenCalledTimes(1);
  expect(service.read.mock.calls[0][0]).toHaveLength(25);
  hook.rerender({ page: 0 });
  await act(async () => {});
  expect(service.list).toHaveBeenCalledTimes(1);
  expect(service.read).toHaveBeenCalledTimes(1);
  hook.rerender({ page: 1 });
  await act(async () => {});
  expect(service.read.mock.calls[1][0]).toEqual([configurations[25]]);
  expect(hook.result.current.readings?.[0].pools[0].id).toBe("pool:35");
});
test("configuration details request only their own pool identities", async () => {
  const service = fixture();
  const hook = renderHook(({ id }) => usePoolConfigurationStatistics(service, "24h", 0, id), {
    initialProps: { id: "set-0" },
  });
  await act(async () => {});
  expect(mocks.adapter.getPoolHistory.mock.lastCall?.[0].poolIds).toEqual(["pool:10"]);
  hook.rerender({ id: "set-25" });
  await act(async () => {});
  expect(mocks.adapter.getPoolHistory.mock.lastCall?.[0].poolIds).toEqual(["pool:35"]);
  expect(hook.result.current.history?.byPool[asPoolId("pool:10")]).toBeUndefined();
});
test("refreshes live readings independently of the selected history duration and interval", async () => {
  const service = fixture();
  let finishFirstHistory!: (value: PoolHistoryResult) => void;
  mocks.adapter.getPoolHistory.mockReturnValueOnce(
    new Promise<PoolHistoryResult>((resolve) => {
      finishFirstHistory = resolve;
    }),
  );
  const hook = renderHook(
    ({ duration }: { duration: "24h" | "48h" }) => usePoolConfigurationStatistics(service, duration, 0, "set-0"),
    { initialProps: { duration: "24h" } },
  );
  await act(async () => {});
  expect(service.read).toHaveBeenCalledTimes(1);
  expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(1);
  expect(hook.result.current.historyPending).toBe(true);
  expect(hook.result.current.readings?.[0].pools[0].id).toBe("pool:10");
  await act(() => vi.advanceTimersByTimeAsync(30_000));
  expect(service.read).toHaveBeenCalledTimes(2);
  expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(1);

  hook.rerender({ duration: "48h" });
  expect(hook.result.current.readings?.[0].pools[0].id).toBe("pool:10");
  await act(async () => {});
  expect(service.read).toHaveBeenCalledTimes(2);
  expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(2);
  expect(mocks.adapter.getPoolHistory.mock.calls[0][1].signal.aborted).toBe(true);
  await act(async () => finishFirstHistory({ byPool: {}, unavailable: [asPoolId("pool:10")] }));
  expect(hook.result.current.history?.byPool[asPoolId("pool:10")]?.accepted).toBe(10);
  const query = mocks.adapter.getPoolHistory.mock.lastCall?.[0];
  expect(Date.parse(query?.endTime) - Date.parse(query?.startTime)).toBe(48 * 3_600_000);
  await act(() => vi.advanceTimersByTimeAsync(5 * 60_000));
  expect(service.read).toHaveBeenCalledTimes(12);
  expect(mocks.adapter.getPoolHistory).toHaveBeenCalledTimes(3);
});
test("keeps other rows and their history visible while it refreshes a changed configuration", async () => {
  const service = fixture();
  const hook = renderHook(() => usePoolConfigurationStatistics(service, "24h", 0));
  await act(async () => {});
  expect(hook.result.current.history?.byPool[asPoolId("pool:10")]?.accepted).toBe(10);
  let finish!: (value: PoolConfigurationReadings[]) => void;
  const pending = new Promise<PoolConfigurationReadings[]>((resolve) => {
    finish = resolve;
  });
  const updated = [{ ...configurations[0], revision: "2" }, ...configurations.slice(1)];
  service.list.mockResolvedValue({ enabled: true, configurations: updated });
  service.read.mockReturnValue(pending);
  await act(() => vi.advanceTimersByTimeAsync(30_000));

  expect(hook.result.current.readings?.[0]).toEqual({ ...updated[0], pools: [], stale: true });
  expect(hook.result.current.history?.byPool[asPoolId("pool:10")]).toBeUndefined();
  expect(hook.result.current.readings?.[1].pools[0].id).toBe("pool:11");
  expect(hook.result.current.history?.byPool[asPoolId("pool:11")]?.accepted).toBe(11);
  expect(mocks.adapter.getPoolHistory.mock.lastCall?.[0].poolIds).not.toContain("pool:10");
  expect(hook.result.current.error).toBeUndefined();

  await act(async () => finish(updated.slice(0, 25).map(reading)));
  expect(hook.result.current.readings?.[0]).toEqual(reading(updated[0]));
  expect(hook.result.current.history?.byPool[asPoolId("pool:10")]?.accepted).toBe(10);
});
test("keeps current configuration readings when share history is unavailable", async () => {
  const service = fixture();
  mocks.adapter.getPoolHistory.mockRejectedValueOnce(new Error("history unavailable"));
  const hook = renderHook(() => usePoolConfigurationStatistics(service, "24h", 0));
  await act(async () => {});
  expect(hook.result.current.readings).toHaveLength(25);
  expect(hook.result.current.history?.unavailable).toHaveLength(25);
  expect(hook.result.current.error).toBeUndefined();
});
test("configuration acceptance rate uses share totals rather than averaging rates", () => {
  const pools = [reading(configurations[0]).pools[0], reading(configurations[1]).pools[0]];
  const shares = configurationShares(pools, {
    byPool: {
      [pools[0].id]: { accepted: 99, rejected: 1, partial: false },
      [pools[1].id]: { accepted: 0, rejected: 1, partial: false },
    },
    unavailable: [],
  });
  expect(shares.acceptanceRate).toBeCloseTo(99 / 101);
  expect(shares.accepted).toBe(99);
  expect(shares.rejected).toBe(2);
  expect(
    configurationShares(pools, {
      byPool: { [pools[0].id]: { accepted: 99, rejected: 1, partial: false } },
      unavailable: [pools[1].id],
    }),
  ).toEqual({ partial: true });
});
