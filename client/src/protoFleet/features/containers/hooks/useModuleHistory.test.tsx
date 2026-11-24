import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { moduleHistoryChart } from "../moduleHistoryChart";
import { useModuleHistory } from "./useModuleHistory";
import { ModuleSampleSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const api = vi.hoisted(() => ({ listModuleHistory: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ fleetInventoryClient: api }));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: ({ onError }: { onError: () => void }) => onError() }),
}));
const sample = create(ModuleSampleSchema, {
  observedAt: timestampFromMs(Date.now() - 30_000),
  hashrateHs: 100e12,
  powerW: 3000,
  temperatureC: 60,
  efficiencyJh: 30e-12,
});
beforeEach(() => {
  vi.resetAllMocks();
  api.listModuleHistory.mockResolvedValue({ samples: [sample], nextPageToken: "" });
});

it("loads every page within one fixed time window before publishing history", async () => {
  api.listModuleHistory.mockResolvedValueOnce({ samples: [sample], nextPageToken: "next" });
  const { result } = renderHook(() => useModuleHistory(91n, "1h", true));
  await waitFor(() => expect(result.current.samples).toHaveLength(2));
  expect(api.listModuleHistory.mock.calls[1][0]).toMatchObject({
    ...api.listModuleHistory.mock.calls[0][0],
    pageToken: "next",
  });
});
it("publishes a complete 1000+2 sample window and keeps cross-page tuple ties and missing values correct in charts", async () => {
  const start = Math.floor((Date.now() - 20 * 3600_000) / 90_000) * 90_000;
  const firstPage = Array.from({ length: 1000 }, (_, index) =>
    create(ModuleSampleSchema, {
      observedAt: timestampFromMs(start + index * 60_000),
      collectedAt: timestampFromMs(start + index * 60_000 + 1000),
      hashrateHs: 10e12,
    }),
  );
  const lastAt = start + 999 * 60_000;
  const secondPage = [
    create(ModuleSampleSchema, {
      observedAt: timestampFromMs(lastAt),
      collectedAt: timestampFromMs(lastAt + 2000),
      hashrateHs: 0,
      efficiencyJh: 0,
    }),
    create(ModuleSampleSchema, { observedAt: timestampFromMs(lastAt + 180_000) }),
  ];
  let finish!: (value: unknown) => void;
  api.listModuleHistory
    .mockResolvedValueOnce({ samples: firstPage, nextPageToken: "second" })
    .mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
  const { result } = renderHook(() => useModuleHistory(91n, "24h", true));
  await waitFor(() => expect(api.listModuleHistory).toHaveBeenCalledTimes(2));
  expect(result.current.samples).toBeUndefined();
  const [initial, initialOptions] = api.listModuleHistory.mock.calls[0];
  const [continuation, continuationOptions] = api.listModuleHistory.mock.calls[1];
  expect(initial).toMatchObject({ moduleId: 91n, pageSize: 1000, pageToken: "" });
  expect(continuation).toEqual({ ...initial, pageToken: "second" });
  expect(initialOptions.timeoutMs).toBe(10_000);
  expect(continuationOptions).toEqual(initialOptions);
  expect(Number(initial.endTime.seconds - initial.startTime.seconds)).toBe(24 * 3600);
  await act(async () => finish({ samples: secondPage, nextPageToken: "" }));
  expect(result.current.samples).toHaveLength(1002);
  expect(result.current.error).toBeUndefined();
  const points = moduleHistoryChart(result.current.samples!, "24h", "C");
  expect(points.find((point) => point.datetime === lastAt)).toMatchObject({
    hashrate: 0,
    power: null,
    efficiency: 0,
    temperature: null,
  });
  expect(points.find((point) => point.datetime === lastAt + 90_000)?.hashrate).toBeNull();
  expect(points.find((point) => point.datetime === lastAt + 180_000)?.hashrate).toBeNull();
});

it("reports a failed continuation without publishing the successful first page as a complete chart", async () => {
  api.listModuleHistory
    .mockResolvedValueOnce({ samples: [sample], nextPageToken: "second" })
    .mockRejectedValueOnce(new Error("History deadline exceeded"));
  const { result } = renderHook(() => useModuleHistory(91n, "24h", true));
  await waitFor(() => expect(result.current.error).toContain("History deadline exceeded"));
  expect(result.current.samples).toEqual([]);
  expect(result.current.endTime).toBeUndefined();
  expect(api.listModuleHistory).toHaveBeenCalledTimes(2);
});
it("clears history on failure and retries", async () => {
  const { result } = renderHook(() => useModuleHistory(91n, "1h", true));
  await waitFor(() => expect(result.current.samples).toHaveLength(1));
  api.listModuleHistory.mockRejectedValueOnce(new Error("Access denied"));
  act(() => result.current.retry());
  await waitFor(() => expect(result.current.error).toContain("Access denied"));
  expect(result.current.samples).toEqual([]);
  act(() => result.current.retry());
  await waitFor(() => expect(result.current.samples).toHaveLength(1));
});
it("cancels old module requests and only displays the current module", async () => {
  let resolve!: (value: unknown) => void;
  api.listModuleHistory.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const { result, rerender } = renderHook(({ moduleId }) => useModuleHistory(moduleId, "1h", true), {
    initialProps: { moduleId: 91n },
  });
  const signal = api.listModuleHistory.mock.calls[0][1].signal;
  rerender({ moduleId: 92n });
  await waitFor(() => expect(result.current.samples).toHaveLength(1));
  expect(api.listModuleHistory.mock.calls[1][0]).toMatchObject({ moduleId: 92n });
  expect(signal.aborted).toBe(true);
  await act(async () => resolve({ samples: [], nextPageToken: "" }));
  expect(result.current.samples).toEqual([sample]);
});
it("rejects looping pagination instead of showing an incomplete chart", async () => {
  api.listModuleHistory.mockResolvedValue({ samples: [sample], nextPageToken: "loop" });
  const { result } = renderHook(() => useModuleHistory(91n, "1h", true));
  await waitFor(() => expect(result.current.error).toBeTruthy());
  expect(result.current.samples).toEqual([]);
  expect(api.listModuleHistory).toHaveBeenCalledTimes(2);
});
it("does not request or retain history when access cannot be refreshed", async () => {
  const { result, rerender } = renderHook(({ enabled }) => useModuleHistory(91n, "1h", enabled), {
    initialProps: { enabled: false },
  });
  expect(api.listModuleHistory).not.toHaveBeenCalled();
  rerender({ enabled: true });
  await waitFor(() => expect(result.current.samples).toHaveLength(1));
  rerender({ enabled: false });
  expect(result.current.samples).toEqual([]);
});

it.each([false, true])(
  "bounds distinct history pages and accepts completion on page 20 (complete: %s)",
  async (complete) => {
    let page = 0;
    api.listModuleHistory.mockImplementation(async () => ({
      samples: [sample],
      nextPageToken: ++page === 20 && complete ? "" : `page-${page}`,
    }));
    const { result } = renderHook(() => useModuleHistory(91n, "24h", true));
    await waitFor(() => expect(api.listModuleHistory).toHaveBeenCalledTimes(20));
    if (complete) {
      await waitFor(() => expect(result.current.samples).toHaveLength(20));
      expect(result.current.error).toBeUndefined();
    } else {
      await waitFor(() => expect(result.current.error).toContain("loaded completely"));
      expect(result.current.samples).toEqual([]);
    }
  },
);
