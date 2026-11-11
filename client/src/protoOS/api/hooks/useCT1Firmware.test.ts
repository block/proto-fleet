import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type CT1Firmware, type CT1FirmwareAttempt, CT1FirmwareStartRefusedError } from "../adapters/ct1Firmware";
import { readAttempts, removeAttempt, saveAttempt } from "./ct1FirmwareStorage";
import { useCT1Firmware } from "./useCT1Firmware";

vi.mock("../adapters/mdkV2/firmwareImage", () => ({
  readFirmwareImage: async (file: File) => ({ file, version: "9.0.0", build: "prod" }),
}));
const boards = [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Board 1" }];
const file = new File(["test"], "f1-app-prod.bin");
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
/** What another tab, or an earlier page load, left in storage. */
const save = (attempt: CT1FirmwareAttempt) => saveAttempt("device", attempt);
const saved = () => readAttempts("device");

describe("CT1 firmware recovery", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());
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
    expect(saved()).toHaveLength(1);
    const [record] = saved();
    expect(record).toMatchObject({ boards, version: "9.0.0", build: "prod" });
    await act(() => first.result.current.install());
    expect(firmware.install).toHaveBeenCalledOnce();
    first.unmount();
    const second = renderHook(() => useCT1Firmware(firmware, "device"));
    await waitFor(() => expect(second.result.current.progress?.complete).toBe(true));
    expect(firmware.progress).toHaveBeenCalledWith(
      record,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(firmware.install).toHaveBeenCalledOnce();
    act(() => second.result.current.clear());
    expect(saved()).toEqual([]);
  });
  it("retains recovery state when cancellation fails", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.cancel.mockRejectedValueOnce(new Error("Peer unavailable"));
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual(attempt);
    expect(result.current.error).toBe("Peer unavailable");
    expect(result.current.cancelUnconfirmed).toBe(true);
    expect(saved()).toEqual([attempt]);
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.cancelUnconfirmed).toBe(false);
    expect(saved()).toEqual([]);
  });
  it("retains and reloads a cancellation that is still stopping boards", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
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
    expect(saved()).toEqual([{ ...attempt, cancelRequested: true }]);
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
    expect(saved()).toEqual([]);
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
    expect(saved()).toEqual([]);
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
    expect(saved()).toEqual([result.current.attempt]);
  });
  it("refuses Start when the record cannot be saved", async () => {
    const firmware = setup();
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      throw new Error("Start was sent");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("Storage is full");
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.error).toBe("Storage is full");
    expect(result.current.attempt).toBeUndefined();
    expect(firmware.progress).not.toHaveBeenCalled();
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
    expect(saved()).toEqual([]);
    expect(firmware.progress).not.toHaveBeenCalled();
  });
  it("keeps an owner that does not know the task tracked until the operator stops tracking", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.cancel.mockResolvedValue({ pending: false, finished: false, absent: ["controller"] });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual({ ...attempt, cancelRequested: true });
    expect(result.current.cancelUnconfirmed).toBe(true);
    expect(saved()).toEqual([{ ...attempt, cancelRequested: true }]);
    act(() => result.current.clear());
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.cancelUnconfirmed).toBe(false);
    expect(saved()).toEqual([]);
  });
  it("keeps the result of an update that finished before the cancel arrived", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.cancel.mockResolvedValue({ pending: false, finished: true, absent: [] });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual(attempt);
    expect(result.current.cancelUnconfirmed).toBe(false);
    expect(saved()).toEqual([attempt]);
    await waitFor(() => expect(result.current.progress?.complete).toBe(true));
  });
  it("adopts an update another tab started instead of uploading a second one", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    save(other);
    await act(() => result.current.install());
    expect(firmware.install).not.toHaveBeenCalled();
    expect(result.current.error).toContain("already tracked");
    expect(result.current.attempt).toEqual(other);
    expect(saved()).toEqual([other]);
  });
  it("refuses Start when another tab saved its update during this tab's upload", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      save(other);
      starting();
      throw new Error("Start was sent");
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.error).toContain("already tracked");
    expect(result.current.attempt).toEqual(other);
    expect(saved()).toEqual([other]);
  });
  it("keeps an accepted update's record when a tab that claimed at the same moment is refused", async () => {
    const firmware = setup();
    const accepted = tracked("accepted");
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      // The controller accepted the other tab's Start and refuses this one.
      throw new CT1FirmwareStartRefusedError("hashboard update already running");
    });
    // Both tabs found no record at the same moment. The other tab's save lands after this tab's check and just
    // before this tab's own save: the interleaving a single shared record would lose.
    const setItem = Storage.prototype.setItem;
    let raced = false;
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (this: Storage, key, value) {
      if (!raced) {
        raced = true;
        save(accepted);
      }
      setItem.call(this, key, value);
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.error).toBe("hashboard update already running");
    expect(saved()).toEqual([accepted]);
    expect(result.current.attempt).toEqual(accepted);
  });
  it("keeps both records when two simultaneous Starts are uncertain, and shows them one at a time", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      save(other);
      throw new Error("Lost response");
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    const own = result.current.attempt;
    expect(own?.taskId).not.toBe(other.taskId);
    expect(saved()).toHaveLength(2);
    expect(saved()).toEqual(expect.arrayContaining([own, other]));
    act(() => result.current.clear());
    expect(saved()).toEqual([other]);
    expect(result.current.attempt).toEqual(other);
  });
  it("leaves a newer record in place when a stale tab finishes an older update", async () => {
    const firmware = setup();
    const older = tracked("older");
    const newer = tracked("newer");
    save(older);
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await waitFor(() => expect(result.current.progress?.complete).toBe(true));
    // Another tab finished the older update and started a new one.
    removeAttempt("device", older.taskId);
    save(newer);
    act(() => result.current.clear());
    expect(saved()).toEqual([newer]);
    expect(result.current.attempt).toEqual(newer);
  });
  it("does not re-save a record another tab released while this tab's cancel was in flight", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.progress.mockResolvedValue({ complete: false, boards: [], errors: [] });
    firmware.cancel.mockImplementation(async () => {
      removeAttempt("device", attempt.taskId);
      return { pending: true, finished: false, absent: [] };
    });
    const { result } = renderHook(() => useCT1Firmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(saved()).toEqual([]);
    expect(result.current.attempt).toBeUndefined();
  });
});
