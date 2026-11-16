import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { HASHBOARD_STREAM_RETRY_MS, HASHBOARD_STREAM_STALL_MS, useHashboardStatus } from "./useHashboardStatus";
import type { AdapterRequestOptions, HashboardStatus } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { asAsicId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: vi.fn() }));
// Standalone ProtoOS is the only place this hook streams, so the tests run
// the real auth retry with a handler that refreshes on a 401 as the real one
// does.
const refreshToken = vi.fn(async (): Promise<string | null> => "fresh-token");
type AuthErrorHandler = (props: {
  error: { status?: number };
  onError?: (error: unknown) => void;
  onSuccess?: (token: string) => unknown;
}) => unknown;
const handleAuthErrors: AuthErrorHandler = async ({ error, onError, onSuccess }) => {
  if (error?.status !== 401) return onError?.(error);
  const token = await refreshToken();
  return token === null ? onError?.(error) : onSuccess?.(token);
};
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({ handleAuthErrors }),
}));

const status = (serial: string, temperature: number): HashboardStatus => {
  const id = `mac:${serial}:asic:0`;
  return {
    serial,
    asicIds: [id],
    asics: [{ id, hashboardSerial: serial, index: 0, row: 0, column: 0 }],
    asicTelemetry: [{ asicId: id, temperature: { value: temperature, units: "°C" } }],
  };
};

/** One open stream the test drives frame by frame. */
type StreamEvent = { frame: HashboardStatus } | { error: unknown } | { end: true };
const openStream = (signal?: AbortSignal | null) => {
  const queue: StreamEvent[] = [];
  let wake: (() => void) | undefined;
  const push = (event: StreamEvent) => {
    queue.push(event);
    wake?.();
  };
  signal?.addEventListener("abort", () => push({ error: signal.reason }));
  async function* frames() {
    for (;;) {
      if (!queue.length) await new Promise<void>((resolve) => (wake = resolve));
      const event = queue.shift()!;
      if ("end" in event) return;
      if ("error" in event) throw event.error;
      yield event.frame;
    }
  }
  return {
    signal,
    frames: frames(),
    send: (frame: HashboardStatus) => push({ frame }),
    fail: (error: unknown) => push({ error }),
    close: () => push({ end: true }),
  };
};

const streams: ReturnType<typeof openStream>[] = [];
const streamHashboardStatus = vi.fn((_serial: string, { signal }: AdapterRequestOptions = {}) => {
  const stream = openStream(signal);
  streams.push(stream);
  return stream.frames;
});
const getHashboardStatus = vi.fn(async (serials: string[]) => serials.map((serial) => status(serial, 40)));

const temperature = (serial: string) =>
  useMinerStore.getState().telemetry.asics.get(asAsicId(`mac:${serial}:asic:0`))?.temperature?.latest?.value;

const mockAdapter = (adapter: object) =>
  vi
    .mocked(useMinerHosting)
    .mockReturnValue({ adapter, isFleetHosted: false } as unknown as ReturnType<typeof useMinerHosting>);

describe("useHashboardStatus streaming", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    streams.length = 0;
    streamHashboardStatus.mockClear();
    getHashboardStatus.mockReset();
    getHashboardStatus.mockImplementation(async (serials: string[]) => serials.map((serial) => status(serial, 40)));
    refreshToken.mockReset();
    refreshToken.mockResolvedValue("fresh-token");
    mockAdapter({ getHashboardStatus, streamHashboardStatus });
    useMinerStore.getState().resetDeviceData();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  test("streams the selected board instead of polling and closes the stream on unmount", async () => {
    const { result, unmount } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    expect(streamHashboardStatus).toHaveBeenCalledWith("AA:01", expect.anything());
    expect(getHashboardStatus).not.toHaveBeenCalled();
    expect(result.current.pending).toBe(true);

    await act(async () => streams[0].send(status("AA:01", 61)));
    expect(result.current.pending).toBe(false);
    expect(temperature("AA:01")).toBe(61);
    expect(useMinerStore.getState().hardware.getHashboard("AA:01")?.asicIds).toHaveLength(1);

    await act(async () => streams[0].send(status("AA:01", 63)));
    expect(temperature("AA:01")).toBe(63);

    unmount();
    expect(streams[0].signal?.aborted).toBe(true);
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(1);
    expect(getHashboardStatus).not.toHaveBeenCalled();
  });

  test("reports a failed stream, keeps the last readings and reconnects", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    await act(async () => streams[0].send(status("AA:01", 61)));
    await act(async () => streams[0].fail({ status: 503, error: { code: 14, message: "controller restarting" } }));
    expect(result.current.error).toBe("controller restarting");
    expect(temperature("AA:01")).toBe(61);

    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS - 1));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(2);

    await act(async () => streams[1].send(status("AA:01", 64)));
    expect(result.current.error).toBeUndefined();
    expect(temperature("AA:01")).toBe(64);
  });

  test("treats a stream that closes without readings as a failure and backs off", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    await act(async () => streams[0].close());
    expect(result.current.error).toMatch(/closed the ASIC stream/);

    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS));
    await act(async () => streams[1].close());
    // The second retry waits twice as long.
    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS * 2 - 1));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(2);
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(3);
  });

  test("reports a stream that stays open without readings as stale and reopens it", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    await act(async () => streams[0].send(status("AA:01", 61)));
    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_STALL_MS - 1));
    expect(result.current.error).toBeUndefined();

    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(result.current.error).toMatch(/stopped sending new ASIC readings/);
    expect(streams[0].signal?.aborted).toBe(true);
    expect(temperature("AA:01")).toBe(61);

    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(2);
    await act(async () => streams[1].send(status("AA:01", 64)));
    expect(result.current.error).toBeUndefined();
    expect(temperature("AA:01")).toBe(64);
  });

  test("reports a stream that never sends its first reading as stale", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_STALL_MS));
    expect(result.current.error).toMatch(/stopped sending new ASIC readings/);
    expect(result.current.pending).toBe(false);
    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(2);
  });

  test("treats a resent sample as stale, also after reconnecting", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    const cached = { ...status("AA:01", 61), observedAt: "2026-11-15T08:00:00Z" };
    await act(async () => streams[0].send(cached));
    for (let second = 1; second < HASHBOARD_STREAM_STALL_MS / 1_000; second++) {
      await act(() => vi.advanceTimersByTimeAsync(1_000));
      await act(async () => streams[0].send(cached));
    }
    expect(result.current.error).toBeUndefined();
    await act(() => vi.advanceTimersByTimeAsync(1_000));
    expect(result.current.error).toMatch(/stopped sending new ASIC readings/);

    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS));
    await act(async () => streams[1].send(cached));
    expect(result.current.error).toMatch(/stopped sending new ASIC readings/);
    await act(async () => streams[1].send({ ...status("AA:01", 62), observedAt: "2026-11-15T08:00:30Z" }));
    expect(result.current.error).toBeUndefined();
  });

  test("goes stale when every reopened stream resends the old sample and closes", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    const cached = { ...status("AA:01", 61), observedAt: "2026-11-15T08:00:00Z" };
    await act(async () => streams[0].send(cached));
    await act(async () => streams[0].close());
    expect(result.current.error).toBeUndefined();

    // Each reopened stream resends the cached sample and closes cleanly.
    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_RETRY_MS));
    await act(async () => streams[1].send(cached));
    await act(async () => streams[1].close());
    expect(result.current.error).toBeUndefined();

    await act(() => vi.advanceTimersByTimeAsync(HASHBOARD_STREAM_STALL_MS - HASHBOARD_STREAM_RETRY_MS));
    expect(result.current.error).toMatch(/stopped sending new ASIC readings/);
    await act(async () => streams[streams.length - 1].send(cached));
    expect(result.current.error).toMatch(/stopped sending new ASIC readings/);
  });

  test("refreshes the session on a 401 and reopens the stream without waiting", async () => {
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    await act(async () => streams[0].send(status("AA:01", 61)));
    await act(async () => streams[0].fail({ status: 401, error: { message: "token expired" } }));
    expect(refreshToken).toHaveBeenCalledTimes(1);
    expect(streamHashboardStatus).toHaveBeenCalledTimes(2);
    expect(result.current.error).toBeUndefined();

    await act(async () => streams[1].send(status("AA:01", 62)));
    expect(temperature("AA:01")).toBe(62);
  });

  test("reports the failure when the session cannot be refreshed", async () => {
    refreshToken.mockResolvedValue(null);
    const { result } = renderHook(() =>
      useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true, stream: true }),
    );
    await act(async () => streams[0].fail({ status: 401, error: { message: "token expired" } }));
    expect(streamHashboardStatus).toHaveBeenCalledTimes(1);
    expect(result.current.error).toBe("token expired");
  });

  test("does not carry a failure over to the next board", async () => {
    const { result, rerender } = renderHook(
      ({ serial }) => useHashboardStatus({ hashboardSerialNumbers: [serial], poll: true, stream: true }),
      { initialProps: { serial: "AA:01" } },
    );
    await act(async () => streams[0].fail({ status: 503, error: { message: "controller restarting" } }));
    expect(result.current.error).toBe("controller restarting");

    rerender({ serial: "AA:02" });
    expect(result.current.error).toBeUndefined();
  });

  test("keeps a polling error until a refresh succeeds", async () => {
    mockAdapter({ getHashboardStatus });
    getHashboardStatus.mockRejectedValue({ status: 503, error: { message: "controller restarting" } });
    const errors: (string | undefined)[] = [];
    const { result } = renderHook(() => {
      const state = useHashboardStatus({ hashboardSerialNumbers: ["AA:01"], poll: true });
      errors.push(state.error);
      return state;
    });
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.error).toBe("controller restarting");

    // A second failed poll never clears the error while it is in flight.
    const seen = errors.length;
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(getHashboardStatus.mock.calls.length).toBeGreaterThan(1);
    expect(errors.slice(seen)).not.toContain(undefined);

    getHashboardStatus.mockImplementation(async (serials: string[]) => serials.map((serial) => status(serial, 40)));
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(result.current.error).toBeUndefined();
  });

  test("moves the stream when the selected board changes", async () => {
    const { rerender } = renderHook(
      ({ serial }) => useHashboardStatus({ hashboardSerialNumbers: [serial], poll: true, stream: true }),
      { initialProps: { serial: "AA:01" } },
    );
    rerender({ serial: "AA:02" });
    expect(streams[0].signal?.aborted).toBe(true);
    expect(streamHashboardStatus).toHaveBeenLastCalledWith("AA:02", expect.anything());

    // A frame already in flight for the old board is ignored.
    await act(async () => streams[0].send(status("AA:01", 70)));
    expect(temperature("AA:01")).toBeUndefined();
  });

  test.each([
    ["the adapter cannot stream", { getHashboardStatus }, ["AA:01"]],
    ["several boards are requested", { getHashboardStatus, streamHashboardStatus }, ["AA:01", "AA:02"]],
  ])("polls when %s", async (_case, adapter, serials) => {
    mockAdapter(adapter);
    renderHook(() => useHashboardStatus({ hashboardSerialNumbers: serials, poll: true, stream: true }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(getHashboardStatus).toHaveBeenCalledWith(serials, expect.anything());
    expect(streamHashboardStatus).not.toHaveBeenCalled();
    expect(temperature("AA:01")).toBe(40);
  });
});
