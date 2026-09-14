import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useTimeSeries } from "./useTimeSeries";
import type { TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetTimeSeries = vi.fn();
let pollFetch: ((signal: AbortSignal) => Promise<void>) | undefined;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry: vi.fn(),
}));

vi.mock("@/protoOS/store/storeWriteGuard", () => ({
  beginStoreWrite: vi.fn(() => {
    const accept = (() => true) as unknown as { (): boolean; requestId: number };
    accept.requestId = 1;
    return accept;
  }),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

const runFetch = async (levels: TimeSeriesLevel[]) => {
  renderHook(() => useTimeSeries({ duration: "1h", levels, poll: false }));
  await act(async () => {
    await pollFetch?.(new AbortController().signal);
  });
};

describe("useTimeSeries", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pollFetch = undefined;
    mockGetTimeSeries.mockResolvedValue(createEmptyTelemetryState());
    (useMinerHosting as Mock).mockReturnValue({
      adapter: { getTimeSeries: mockGetTimeSeries },
    });
    // authRetry: run the request and hand its result to onSuccess, matching the
    // real hook's success path without the token-refresh machinery.
    (useAuthRetry as Mock).mockReturnValue(
      vi.fn(async ({ request, onSuccess, signal }) => {
        const result = await request({ signal });
        await onSuccess?.(result);
      }),
    );
    (usePoll as Mock).mockImplementation(({ fetchData }) => {
      pollFetch = fetchData;
    });
    // fetchData bails out when the store reports zero hashboards, so seed one.
    useMinerStore.getState().hardware.addHashboard({ serial: "board-1", asicIds: [] });
  });

  afterEach(() => {
    useMinerStore.getState().hardware.reset();
  });

  // Regression for the split-telemetry key collision: the miner aggregate and
  // the per-board hashboard series must NOT share a store-write key. levels is an
  // array of objects, so a naive join(",") stringified both to "[object Object]"
  // and the newest-requestId-wins guard silently dropped one series' writes.
  test("keys the write guard on query contents, not the stringified array", async () => {
    const miner: TimeSeriesLevel[] = [{ type: "miner", fields: ["hashrate", "power"] }];
    await runFetch(miner);
    expect(beginStoreWrite).toHaveBeenLastCalledWith(`telemetry:history:1h:${JSON.stringify(miner)}`);

    const hashboard: TimeSeriesLevel[] = [{ type: "hashboard", fields: ["hashrate"], indexes: [0] }];
    await runFetch(hashboard);
    expect(beginStoreWrite).toHaveBeenLastCalledWith(`telemetry:history:1h:${JSON.stringify(hashboard)}`);
  });

  test("aggregate and per-board calls at the same duration get distinct keys", async () => {
    await runFetch([{ type: "miner", fields: ["hashrate"] }]);
    await runFetch([{ type: "hashboard", fields: ["hashrate"], indexes: [0] }]);

    const keys = (beginStoreWrite as Mock).mock.calls.map(([key]) => key);
    expect(new Set(keys).size).toBe(keys.length);
    expect(keys).not.toContain("telemetry:history:1h:[object Object]");
  });
});
