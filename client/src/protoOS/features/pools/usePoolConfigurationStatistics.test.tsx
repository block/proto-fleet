import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { configurationShares, usePoolConfigurationStatistics } from "./usePoolConfigurationStatistics";
import type { PoolConfigurationSummary } from "@/protoOS/api/adapters/poolStatistics";
import { asPoolId } from "@/protoOS/store/contract";

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
  list: vi.fn().mockResolvedValue({ enabled: true, configurations }),
  read: vi.fn().mockImplementation(async (entries: PoolConfigurationSummary[]) => entries.map(reading)),
});
beforeEach(() => {
  vi.useFakeTimers();
  mocks.adapter.getPoolHistory.mockImplementation(async ({ poolIds }: { poolIds: string[] }) => ({
    byPool: Object.fromEntries(
      poolIds.map((id) => [id, { accepted: Number(id.replace("pool:", "")), rejected: 1, partial: false }]),
    ),
    unavailable: [],
  }));
});
afterEach(() => vi.useRealTimers());
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
