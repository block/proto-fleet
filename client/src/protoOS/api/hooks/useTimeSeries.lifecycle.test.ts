import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { useTimeSeries } from "./useTimeSeries";
import type { TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import type { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import type { Duration } from "@/shared/components/DurationSelector";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: vi.fn() }));
vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({ useAuthRetry: () => retry }));

const retry = async ({ request, onSuccess, onError, signal }: Parameters<ReturnType<typeof useAuthRetry>>[0]) => {
  try {
    await onSuccess?.(await request({ signal }));
  } catch {
    onError?.({ status: 500, error: { message: "Request failed" } });
  }
};
const getTimeSeries = vi.fn();
const adapter = { getTimeSeries };
const setAdapter = (value: typeof adapter | undefined) =>
  vi.mocked(useMinerHosting).mockReturnValue({ adapter: value } as unknown as ReturnType<typeof useMinerHosting>);
const loadHardware = () => useMinerStore.getState().hardware.setHashboards([{ serial: "board-1", slot: 1 }]);
const levels: TimeSeriesLevel[] = [{ type: "miner", fields: ["hashrate"] }];
const history = (value: number) => {
  const telemetry = createEmptyTelemetryState();
  telemetry.rollupsByEntity.set(ROOT_MODULE_KEY, {
    hashrate: { timeSeries: { units: "TH/s", points: [{ timestampMs: 1000, value }] } },
  });
  return telemetry;
};

describe("useTimeSeries request lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    getTimeSeries.mockReset().mockResolvedValue(createEmptyTelemetryState());
    setAdapter(adapter);
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().ui.setDuration("24h");
    loadHardware();
  });

  afterEach(() => vi.useRealTimers());

  test("fetches once on mount and once per polling interval, even with recreated levels", async () => {
    const { rerender, unmount } = renderHook(() =>
      useTimeSeries({ duration: "24h", levels: [{ type: "miner", fields: ["hashrate"] }], pollIntervalMs: 1000 }),
    );
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(1);
    rerender();
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(getTimeSeries).toHaveBeenCalledTimes(2);
    unmount();
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(getTimeSeries).toHaveBeenCalledTimes(2);
  });

  test("fetches once when both hardware and adapter become available with polling disabled", async () => {
    useMinerStore.getState().resetDeviceData();
    setAdapter(undefined);
    const { rerender } = renderHook(() => useTimeSeries({ duration: "24h", levels, poll: false }));
    act(loadHardware);
    expect(getTimeSeries).not.toHaveBeenCalled();
    setAdapter(adapter);
    rerender();
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(getTimeSeries).toHaveBeenCalledTimes(1);
  });

  test("fetches once for each changed field, board selection, duration, or adapter", async () => {
    const { rerender } = renderHook(
      (props: { duration: Duration; levels: TimeSeriesLevel[] }) => useTimeSeries({ ...props, poll: false }),
      { initialProps: { duration: "24h", levels } },
    );
    await act(async () => {});
    rerender({ duration: "24h", levels: [{ type: "hashboard", fields: ["power"], indexes: [0] }] });
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(2);
    rerender({ duration: "24h", levels: [{ type: "hashboard", fields: ["power"], indexes: [1] }] });
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(3);
    act(() => useMinerStore.getState().ui.setDuration("1h"));
    rerender({ duration: "1h", levels });
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(4);
    expect(getTimeSeries).toHaveBeenLastCalledWith(
      expect.objectContaining({ duration: "PT60M", interval: "PT1M", levels }),
      expect.anything(),
    );
    setAdapter({ getTimeSeries });
    rerender({ duration: "1h", levels });
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(5);
  });

  test("waits for a slow response before scheduling the next poll", async () => {
    let finish!: (value: ReturnType<typeof history>) => void;
    getTimeSeries.mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
    const { result } = renderHook(() => useTimeSeries({ duration: "24h", levels, pollIntervalMs: 1000 }));
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(getTimeSeries).toHaveBeenCalledTimes(1);
    expect(result.current.pending).toBe(true);
    await act(async () => finish(history(12)));
    expect(result.current.pending).toBe(false);
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(getTimeSeries).toHaveBeenCalledTimes(2);
  });

  test("does not merge responses from a previous duration or device connection", async () => {
    let finish!: (value: ReturnType<typeof history>) => void;
    getTimeSeries.mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
    renderHook(() => useTimeSeries({ duration: "24h", levels, poll: false }));
    act(() => useMinerStore.getState().ui.setDuration("1h"));
    await act(async () => finish(history(12)));
    expect(useMinerStore.getState().telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)).toBeUndefined();

    act(() => useMinerStore.getState().ui.setDuration("24h"));
    getTimeSeries.mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)));
    renderHook(() => useTimeSeries({ duration: "24h", levels, poll: false }));
    act(() => useMinerStore.getState().resetDeviceData());
    await act(async () => finish(history(24)));
    expect(useMinerStore.getState().telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)).toBeUndefined();
  });

  test("retains histories for different fields when their responses arrive out of order", async () => {
    let finishHashrate!: (value: ReturnType<typeof history>) => void;
    getTimeSeries.mockImplementationOnce(() => new Promise((resolve) => (finishHashrate = resolve)));
    renderHook(() => useTimeSeries({ duration: "24h", levels, poll: false }));
    const temperature = createEmptyTelemetryState();
    temperature.rollupsByEntity.set(ROOT_MODULE_KEY, {
      temperature: { timeSeries: { units: "°C", points: [{ timestampMs: 1000, value: 60 }] } },
    });
    getTimeSeries.mockResolvedValueOnce(temperature);
    renderHook(() =>
      useTimeSeries({ duration: "24h", levels: [{ type: "miner", fields: ["temperature"] }], poll: false }),
    );
    await act(async () => {});
    await act(async () => finishHashrate(history(12)));
    expect(useMinerStore.getState().telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)).toMatchObject({
      hashrate: { timeSeries: { points: [{ value: 12 }] } },
      temperature: { timeSeries: { points: [{ value: 60 }] } },
    });
  });

  test("cancels superseded requests and ignores late successful responses", async () => {
    let finishOld!: (value: ReturnType<typeof history>) => void;
    let finishNew!: (value: ReturnType<typeof history>) => void;
    getTimeSeries
      .mockImplementationOnce(() => new Promise((resolve) => (finishOld = resolve)))
      .mockImplementationOnce(() => new Promise((resolve) => (finishNew = resolve)));
    const { result, rerender, unmount } = renderHook(
      (fields: TimeSeriesLevel[]) => useTimeSeries({ duration: "24h", levels: fields, poll: false }),
      { initialProps: levels },
    );
    const oldSignal = getTimeSeries.mock.calls[0][1].signal as AbortSignal;
    rerender([{ type: "miner", fields: ["power"] }]);
    expect(oldSignal.aborted).toBe(true);
    await act(async () => finishOld(history(12)));
    expect(result.current.pending).toBe(true);
    expect(useMinerStore.getState().telemetry.rollupsByEntity.get(ROOT_MODULE_KEY)).toBeUndefined();
    await act(async () => finishNew(createEmptyTelemetryState()));
    expect(result.current.pending).toBe(false);
    const newSignal = getTimeSeries.mock.calls[1][1].signal as AbortSignal;
    unmount();
    expect(newSignal.aborted).toBe(true);
  });

  test("does not fetch until explicitly enabled", async () => {
    const { rerender } = renderHook(
      (enabled: boolean) => useTimeSeries({ duration: "24h", levels, enabled, poll: false }),
      { initialProps: false },
    );
    await act(async () => {});
    expect(getTimeSeries).not.toHaveBeenCalled();
    rerender(true);
    await act(async () => {});
    expect(getTimeSeries).toHaveBeenCalledTimes(1);
  });
});
