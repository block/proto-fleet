import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";

import { useSystemInfo } from "./useSystemInfo";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV1/normalize";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useIsWebServerRunning } from "@/protoOS/store/hooks/useSystemInfo";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { usePoll } from "@/shared/hooks/usePoll";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

const deferredSystemInfo = () => {
  let resolve!: (value: ReturnType<typeof toCanonicalSystemInfo>) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<ReturnType<typeof toCanonicalSystemInfo>>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
};

describe("useSystemInfo", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useMinerStore.getState().resetDeviceData();
    (useMinerHosting as Mock).mockReturnValue({ adapter: null });
  });

  test("starts polling when asynchronous adapter resolution completes", () => {
    const adapter = { getSystemInfo: vi.fn() };
    const { rerender } = renderHook(() => useSystemInfo({ poll: true }));

    expect(usePoll).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false }));

    (useMinerHosting as Mock).mockReturnValue({ adapter });
    rerender();

    expect(usePoll).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: true }));
  });

  test("ignores an older request failure after a newer request succeeds", async () => {
    const older = deferredSystemInfo();
    const newer = deferredSystemInfo();
    const adapter = { getSystemInfo: vi.fn().mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise) };
    (useMinerHosting as Mock).mockReturnValue({ adapter });
    const { result } = renderHook(() => ({
      polling: useSystemInfo({ poll: true }),
      manual: useSystemInfo({ poll: false }),
      online: useIsWebServerRunning(),
    }));

    act(() => {
      result.current.polling.reload();
      result.current.manual.reload();
    });
    await act(async () => newer.resolve(toCanonicalSystemInfo({})));
    expect(result.current.online).toBe(true);

    await act(async () => older.reject(new Error("Connection refused")));
    expect(result.current.online).toBe(true);
    expect(result.current.polling.error).toBeUndefined();
  });

  test("keeps a newer failure authoritative when an older request succeeds later", async () => {
    const older = deferredSystemInfo();
    const newer = deferredSystemInfo();
    const adapter = { getSystemInfo: vi.fn().mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise) };
    (useMinerHosting as Mock).mockReturnValue({ adapter });
    useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({}));
    const { result } = renderHook(() => ({
      polling: useSystemInfo({ poll: true }),
      manual: useSystemInfo({ poll: false }),
      online: useIsWebServerRunning(),
    }));

    act(() => {
      result.current.polling.reload();
      result.current.manual.reload();
    });
    await act(async () => newer.reject(new Error("Connection refused")));
    expect(result.current.online).toBe(false);

    await act(async () => older.resolve(toCanonicalSystemInfo({})));
    expect(result.current.online).toBe(false);
    expect(result.current.polling.error).toBeDefined();
  });

  test("ignores a failure from the previous device after a reset", async () => {
    const previous = deferredSystemInfo();
    (useMinerHosting as Mock).mockReturnValue({ adapter: { getSystemInfo: () => previous.promise } });
    const { result } = renderHook(() => ({
      request: useSystemInfo({ poll: false }),
      online: useIsWebServerRunning(),
    }));
    act(() => result.current.request.reload());
    act(() => {
      useMinerStore.getState().resetDeviceData();
      useMinerStore.getState().systemInfo.setSystemInfo(toCanonicalSystemInfo({}));
    });

    await act(async () => previous.reject(new Error("Previous device disconnected")));
    expect(result.current.online).toBe(true);
    expect(result.current.request.error).toBeUndefined();
  });
});
