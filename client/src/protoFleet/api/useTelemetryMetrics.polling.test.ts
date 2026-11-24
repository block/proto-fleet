import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { MeasurementType } from "./generated/telemetry/v1/telemetry_pb";
import { useTelemetryMetrics } from "./useTelemetryMetrics";

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
  auth: vi.fn(({ onError }: { onError: () => void }) => onError()),
}));
vi.mock("./clients", () => ({ telemetryClient: { getCombinedMetrics: mocks.request } }));
vi.mock("@/protoFleet/store", () => ({ useAuthErrors: () => ({ handleAuthErrors: mocks.auth }) }));
const empty = { metrics: [] };
beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  mocks.request.mockResolvedValue(empty);
});
afterEach(() => vi.useRealTimers());

it("waits for a slow response before scheduling another poll", async () => {
  let resolve!: (value: unknown) => void;
  mocks.request.mockImplementationOnce(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const { result } = renderHook(() => useTelemetryMetrics({ duration: "24h", enabled: true, pollIntervalMs: 1000 }));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(mocks.request).toHaveBeenCalledTimes(1);
  await act(async () => {
    resolve(empty);
  });
  expect(result.current.hasLoaded).toBe(true);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(mocks.request).toHaveBeenCalledTimes(2);
});

it("aborts navigation and unmount requests, ignoring late responses and errors", async () => {
  let reject!: (value: unknown) => void;
  mocks.request.mockImplementationOnce(
    () =>
      new Promise((_resolve, r) => {
        reject = r;
      }),
  );
  const { result, rerender, unmount } = renderHook(
    ({ id }) => useTelemetryMetrics({ duration: "24h", deviceIds: [id], enabled: true, pollIntervalMs: 1000 }),
    { initialProps: { id: "a" } },
  );
  const oldSignal = mocks.request.mock.calls[0][1].signal;
  rerender({ id: "b" });
  await act(async () => {});
  expect(oldSignal.aborted).toBe(true);
  expect(result.current.data).toEqual(empty);
  await act(async () => {
    reject(new ConnectError("expired", Code.Unauthenticated));
  });
  expect(mocks.auth).not.toHaveBeenCalled();
  const signal = mocks.request.mock.calls[1][1].signal;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(mocks.request).toHaveBeenCalledTimes(2);
});

it("aborts when disabled and starts one request on re-enable or manual refresh", async () => {
  mocks.request.mockImplementation(() => new Promise(() => {}));
  const { result, rerender } = renderHook(
    ({ enabled }) => useTelemetryMetrics({ duration: "24h", enabled, pollIntervalMs: 1000 }),
    { initialProps: { enabled: true } },
  );
  const first = mocks.request.mock.calls[0][1].signal;
  rerender({ enabled: false });
  expect(first.aborted).toBe(true);
  expect(result.current.isLoading).toBe(false);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(mocks.request).toHaveBeenCalledTimes(1);
  rerender({ enabled: true });
  expect(mocks.request).toHaveBeenCalledTimes(2);
  const second = mocks.request.mock.calls[1][1].signal;
  act(() => result.current.refetch());
  expect(second.aborted).toBe(true);
  expect(mocks.request).toHaveBeenCalledTimes(3);
});

it("refetches measurement changes but not equivalent array identities", async () => {
  const { rerender } = renderHook(
    ({ measurement }) =>
      useTelemetryMetrics({ duration: "24h", enabled: true, deviceIds: ["a"], measurementTypes: [measurement] }),
    { initialProps: { measurement: MeasurementType.HASHRATE } },
  );
  await act(async () => {});
  rerender({ measurement: MeasurementType.HASHRATE });
  expect(mocks.request).toHaveBeenCalledTimes(1);
  rerender({ measurement: MeasurementType.POWER });
  await act(async () => {});
  expect(mocks.request).toHaveBeenCalledTimes(2);
  expect(mocks.request.mock.calls[1][0].measurementTypes).toEqual([MeasurementType.POWER]);
});
