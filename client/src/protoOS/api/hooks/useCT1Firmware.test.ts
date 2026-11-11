import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { type CT1Firmware, type CT1FirmwareAttempt, CT1FirmwareStartRefusedError } from "../adapters/ct1Firmware";
import { useCT1Firmware } from "./useCT1Firmware";

vi.mock("../adapters/mdkV2/firmwareImage", () => ({
  readFirmwareImage: async (file: File) => ({ file, version: "9.0.0", build: "prod" }),
}));
const boards = [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Board 1" }];
const file = new File(["test"], "f1-app-prod.bin");
const key = "proto-os:ct1-firmware:device";
const setup = () => ({
  prepare: vi.fn<CT1Firmware["prepare"]>().mockResolvedValue(boards),
  install: vi
    .fn<CT1Firmware["install"]>()
    .mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
    }),
  progress: vi.fn<CT1Firmware["progress"]>().mockResolvedValue({ complete: true, boards: [], errors: [] }),
  cancel: vi.fn<CT1Firmware["cancel"]>().mockResolvedValue({ pending: false, finished: false, absent: [] }),
});
const tracked = (taskId = "task"): CT1FirmwareAttempt => ({ taskId, boards, version: "9.0.0", build: "prod" });
const stored = () => JSON.parse(localStorage.getItem(key) ?? "null");

describe("CT1 firmware recovery", () => {
  beforeEach(() => localStorage.clear());
  it("persists before Start, blocks duplicates, and resumes status after a remount without installing", async () => {
    const firmware = setup();
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      throw new Error("Lost response");
    });
    const first = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => first.result.current.choose(file));
    await act(() => first.result.current.install());
    expect(first.result.current.error).toBe("Lost response");
    const saved = JSON.parse(localStorage.getItem(key)!);
    expect(saved).toMatchObject({ boards, version: "9.0.0", build: "prod" });
    await act(() => first.result.current.install());
    expect(firmware.install).toHaveBeenCalledOnce();
    first.unmount();
    const second = renderHook(() => useCT1Firmware(firmware, "device"));
    await waitFor(() => expect(second.result.current.progress?.complete).toBe(true));
    expect(firmware.progress).toHaveBeenCalledWith(saved, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(firmware.install).toHaveBeenCalledOnce();
    act(() => second.result.current.clear());
    expect(localStorage.getItem(key)).toBeNull();
  });
  it("retains recovery state when cancellation fails", async () => {
    const firmware = setup();
    const attempt: CT1FirmwareAttempt = { taskId: "task", boards, version: "9.0.0", build: "prod" };
    localStorage.setItem(key, JSON.stringify(attempt));
    firmware.cancel.mockRejectedValueOnce(new Error("Peer unavailable"));
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual(attempt);
    expect(result.current.error).toBe("Peer unavailable");
    expect(result.current.cancelUnconfirmed).toBe(true);
    expect(localStorage.getItem(key)).not.toBeNull();
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.cancelUnconfirmed).toBe(false);
    expect(localStorage.getItem(key)).toBeNull();
  });
  it("retains and reloads a cancellation that is still stopping boards", async () => {
    const firmware = setup();
    const attempt: CT1FirmwareAttempt = { taskId: "task", boards, version: "9.0.0", build: "prod" };
    localStorage.setItem(key, JSON.stringify(attempt));
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.cancel.mockResolvedValue({ pending: true, finished: false, absent: [] });
    const first = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await first.result.current.cancel();
    });
    expect(first.result.current.attempt?.cancelRequested).toBe(true);
    first.unmount();
    const second = renderHook(() => useCT1Firmware(firmware, "device"));
    expect(second.result.current.attempt?.cancelRequested).toBe(true);
    expect(localStorage.getItem(key)).not.toBeNull();
  });
  it("aborts the active upload when leaving the page", async () => {
    const firmware = setup();
    let signal: AbortSignal | null | undefined;
    firmware.install.mockImplementation(async (_image, _attempt, _progress, params) => {
      signal = params?.signal;
      await new Promise<void>((resolve) => signal?.addEventListener("abort", () => resolve(), { once: true }));
    });
    const { result, unmount } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    let pending: Promise<void>;
    act(() => {
      pending = result.current.install();
    });
    expect(signal?.aborted).toBe(false);
    unmount();
    await pending!;
    expect(signal?.aborted).toBe(true);
    expect(localStorage.getItem(key)).toBeNull();
  });
  it("cancels an upload in flight without tracking it or sending a cancel to the controller", async () => {
    const firmware = setup();
    let signal: AbortSignal | null | undefined;
    firmware.install.mockImplementation(async (_image, _attempt, _progress, params) => {
      signal = params?.signal;
      await new Promise<void>((_resolve, reject) =>
        signal?.addEventListener("abort", () => reject(signal?.reason), { once: true }),
      );
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    let pending: Promise<void>;
    act(() => {
      pending = result.current.install();
    });
    expect(result.current.busy).toBe(true);
    await act(async () => {
      void result.current.cancel();
      await pending;
    });
    expect(signal?.aborted).toBe(true);
    expect(result.current.busy).toBe(false);
    expect(result.current.error).toBeUndefined();
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.image?.file).toBe(file);
    expect(firmware.cancel).not.toHaveBeenCalled();
    expect(localStorage.getItem(key)).toBeNull();
  });
  it("does not abort a Start that is already in flight", async () => {
    const firmware = setup();
    let signal: AbortSignal | null | undefined;
    let finishStart = () => {};
    firmware.install.mockImplementation(async (_image, _attempt, _progress, params, starting) => {
      signal = params?.signal;
      starting();
      await new Promise<void>((resolve) => {
        finishStart = resolve;
      });
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    let pending: Promise<void>;
    act(() => {
      pending = result.current.install();
    });
    await waitFor(() => expect(result.current.attempt).toBeDefined());
    await act(async () => {
      await result.current.cancel();
    });
    expect(signal?.aborted).toBe(false);
    expect(firmware.cancel).not.toHaveBeenCalled();
    await act(async () => {
      finishStart();
      await pending;
    });
    expect(result.current.attempt).toEqual(stored());
  });
  it("does not track an update the controller definitively refused", async () => {
    const firmware = setup();
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      throw new CT1FirmwareStartRefusedError("hashboard update already running");
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.error).toBe("hashboard update already running");
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.image?.file).toBe(file);
    expect(localStorage.getItem(key)).toBeNull();
    expect(firmware.progress).not.toHaveBeenCalled();
  });
  it("keeps an owner that does not know the task tracked until the operator stops tracking", async () => {
    const firmware = setup();
    const attempt = tracked();
    localStorage.setItem(key, JSON.stringify(attempt));
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.cancel.mockResolvedValue({ pending: false, finished: false, absent: ["controller"] });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual({ ...attempt, cancelRequested: true });
    expect(result.current.cancelUnconfirmed).toBe(true);
    expect(stored()).toEqual({ ...attempt, cancelRequested: true });
    act(() => result.current.clear());
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.cancelUnconfirmed).toBe(false);
    expect(localStorage.getItem(key)).toBeNull();
  });
  it("keeps the result of an update that finished before the cancel arrived", async () => {
    const firmware = setup();
    const attempt = tracked();
    localStorage.setItem(key, JSON.stringify(attempt));
    firmware.cancel.mockResolvedValue({ pending: false, finished: true, absent: [] });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual(attempt);
    expect(result.current.cancelUnconfirmed).toBe(false);
    expect(stored()).toEqual(attempt);
    await waitFor(() => expect(result.current.progress?.complete).toBe(true));
  });
  it("adopts an update another tab started instead of uploading over its record", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    localStorage.setItem(key, JSON.stringify(other));
    await act(() => result.current.install());
    expect(firmware.install).not.toHaveBeenCalled();
    expect(result.current.error).toContain("already tracked");
    expect(result.current.attempt).toEqual(other);
    expect(stored()).toEqual(other);
  });
  it("refuses Start when another tab saved its update during this tab's upload", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      localStorage.setItem(key, JSON.stringify(other));
      starting();
      throw new Error("Start was sent");
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.error).toContain("already tracked");
    expect(result.current.attempt).toEqual(other);
    expect(stored()).toEqual(other);
  });
  it("leaves a newer record in place when a stale tab finishes an older update", async () => {
    const firmware = setup();
    const newer = tracked("newer");
    localStorage.setItem(key, JSON.stringify(tracked("older")));
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await waitFor(() => expect(result.current.progress?.complete).toBe(true));
    localStorage.setItem(key, JSON.stringify(newer));
    act(() => result.current.clear());
    expect(stored()).toEqual(newer);
    expect(result.current.attempt).toEqual(newer);
  });
  it("does not re-save a record another tab released while this tab's cancel was in flight", async () => {
    const firmware = setup();
    localStorage.setItem(key, JSON.stringify(tracked()));
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.cancel.mockImplementation(async () => {
      localStorage.removeItem(key);
      return { pending: true, finished: false, absent: [] };
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(localStorage.getItem(key)).toBeNull();
    expect(result.current.attempt).toBeUndefined();
  });
});
