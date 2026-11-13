import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  type HashboardFirmware,
  type HashboardFirmwareAttempt,
  HashboardFirmwareCancelError,
  HashboardFirmwareStartRefusedError,
} from "../adapters/hashboardFirmware";
import { readAttempts, removeAttempt, saveAttempt } from "./hashboardFirmwareStorage";
import { useHashboardFirmware } from "./useHashboardFirmware";

const boards = [{ mac: "02:00:00:00:00:01", owner: "controller", label: "Board 1" }];
const file = new File(["test"], "f1-app-prod.bin");
const setup = () => ({
  readImage: vi
    .fn<HashboardFirmware["readImage"]>()
    .mockImplementation(async (chosen) => ({ file: chosen, version: "9.0.0", build: "prod" })),
  prepare: vi.fn<HashboardFirmware["prepare"]>().mockResolvedValue(boards),
  install: vi
    .fn<HashboardFirmware["install"]>()
    .mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
    }),
  progress: vi.fn<HashboardFirmware["progress"]>().mockResolvedValue({ outcome: "completed", boards: [], errors: [] }),
  cancel: vi.fn<HashboardFirmware["cancel"]>().mockResolvedValue({ pending: false, finished: false, absent: [] }),
});
const tracked = (taskId = "task"): HashboardFirmwareAttempt => ({ taskId, boards, version: "9.0.0", build: "prod" });
/** What another tab, or an earlier page load, left in storage. */
const save = (attempt: HashboardFirmwareAttempt) => saveAttempt("device", attempt);
const saved = () => readAttempts("device");

describe("Hashboard firmware recovery", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());
  it("persists before Start, blocks duplicates, and resumes status after a remount without installing", async () => {
    const firmware = setup();
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      throw new Error("Lost response");
    });
    const first = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(() => first.result.current.choose(file));
    await act(() => first.result.current.install());
    expect(first.result.current.errors).toEqual(["Lost response"]);
    expect(saved()).toHaveLength(1);
    const [record] = saved();
    expect(record).toMatchObject({ boards, version: "9.0.0", build: "prod" });
    await act(() => first.result.current.install());
    expect(firmware.install).toHaveBeenCalledOnce();
    first.unmount();
    const second = renderHook(() => useHashboardFirmware(firmware, "device"));
    await waitFor(() => expect(second.result.current.progress?.outcome).toBe("completed"));
    expect(firmware.progress).toHaveBeenCalledWith(
      record,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(firmware.install).toHaveBeenCalledOnce();
    act(() => second.result.current.clear());
    expect(saved()).toEqual([]);
  });
  it("polls while the update is unresolved and stops once it has ended", async () => {
    vi.useFakeTimers();
    try {
      const firmware = setup();
      save(tracked());
      firmware.progress.mockResolvedValue({ boards: [], errors: [] });
      const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
      await act(() => vi.advanceTimersByTimeAsync(4500));
      expect(firmware.progress).toHaveBeenCalledTimes(3);
      firmware.progress.mockResolvedValue({ outcome: "canceled", boards: [], errors: [] });
      await act(() => vi.advanceTimersByTimeAsync(2000));
      expect(result.current.progress?.outcome).toBe("canceled");
      expect(firmware.progress).toHaveBeenCalledTimes(4);
      await act(() => vi.advanceTimersByTimeAsync(10_000));
      expect(firmware.progress).toHaveBeenCalledTimes(4);
    } finally {
      vi.useRealTimers();
    }
  });
  it("retains recovery state when cancellation fails", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.cancel.mockRejectedValueOnce(new Error("Peer unavailable"));
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual({ ...attempt, cancel: "unconfirmed" });
    expect(result.current.errors).toEqual(["Peer unavailable"]);
    expect(saved()).toEqual([{ ...attempt, cancel: "unconfirmed" }]);
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toBeUndefined();
    expect(saved()).toEqual([]);
  });
  it("reports each owner that could not answer a cancel on a line of its own", async () => {
    const firmware = setup();
    save(tracked());
    const failures = ["controller-a: Controller unavailable", "controller-b: Controller unavailable"];
    firmware.cancel.mockRejectedValue(new HashboardFirmwareCancelError(failures));
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.errors).toEqual(failures);
    expect(result.current.attempt?.cancel).toBe("unconfirmed");
  });
  it("retains and reloads a cancellation that is still stopping boards", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.cancel.mockResolvedValue({ pending: true, finished: false, absent: [] });
    const first = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await first.result.current.cancel();
    });
    expect(first.result.current.attempt?.cancel).toBe("requested");
    first.unmount();
    const second = renderHook(() => useHashboardFirmware(firmware, "device"));
    expect(second.result.current.attempt?.cancel).toBe("requested");
    expect(saved()).toEqual([{ ...attempt, cancel: "requested" }]);
  });
  it("aborts the active upload when leaving the page", async () => {
    const firmware = setup();
    let signal: AbortSignal | null | undefined;
    firmware.install.mockImplementation(async (_image, _attempt, _progress, params) => {
      signal = params?.signal;
      await new Promise<void>((resolve) => signal?.addEventListener("abort", () => resolve(), { once: true }));
    });
    const { result, unmount } = renderHook(() => useHashboardFirmware(firmware, "device"));
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
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
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
    expect(result.current.errors).toEqual([]);
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
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
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
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.errors).toEqual(["Storage is full"]);
    expect(result.current.attempt).toBeUndefined();
    expect(firmware.progress).not.toHaveBeenCalled();
  });
  it("does not track an update the controller definitively refused", async () => {
    const firmware = setup();
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      throw new HashboardFirmwareStartRefusedError("hashboard update already running");
    });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.errors).toEqual(["hashboard update already running"]);
    expect(result.current.attempt).toBeUndefined();
    expect(result.current.image?.file).toBe(file);
    expect(saved()).toEqual([]);
    expect(firmware.progress).not.toHaveBeenCalled();
  });
  it("keeps an owner that does not know the task tracked until the operator stops tracking", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.cancel.mockResolvedValue({ pending: false, finished: false, absent: ["controller"] });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual({ ...attempt, cancel: "unconfirmed" });
    expect(saved()).toEqual([{ ...attempt, cancel: "unconfirmed" }]);
    act(() => result.current.clear());
    expect(result.current.attempt).toBeUndefined();
    expect(saved()).toEqual([]);
  });
  it("still offers the release after a reload when a cancel was not confirmed", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.cancel.mockRejectedValue(new Error("Peer unavailable"));
    const first = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await first.result.current.cancel();
    });
    first.unmount();
    const second = renderHook(() => useHashboardFirmware(firmware, "device"));
    expect(second.result.current.attempt).toEqual({ ...attempt, cancel: "unconfirmed" });
    expect(firmware.cancel).toHaveBeenCalledOnce();
  });
  it("does not record a cancel that was aborted by leaving the page", async () => {
    const firmware = setup();
    const attempt = tracked();
    save(attempt);
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.cancel.mockImplementation(
      (_attempt, params) =>
        new Promise((_resolve, reject) =>
          params?.signal?.addEventListener("abort", () => reject(new Error("Aborted")), { once: true }),
        ),
    );
    const { result, unmount } = renderHook(() => useHashboardFirmware(firmware, "device"));
    let pending: Promise<void> | undefined;
    act(() => {
      pending = result.current.cancel();
    });
    unmount();
    await pending;
    expect(saved()).toEqual([attempt]);
  });
  it("keeps the result of an update that finished before the cancel arrived", async () => {
    const firmware = setup();
    const attempt = tracked();
    // An earlier cancel could not be confirmed; the job then finished on its own.
    save({ ...attempt, cancel: "unconfirmed" });
    firmware.cancel.mockResolvedValue({ pending: false, finished: true, absent: [] });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(result.current.attempt).toEqual(attempt);
    expect(result.current.attempt?.cancel).toBeUndefined();
    expect(saved()).toEqual([attempt]);
    await waitFor(() => expect(result.current.progress?.outcome).toBe("completed"));
  });
  it("adopts an update another tab started instead of uploading a second one", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(() => result.current.choose(file));
    save(other);
    await act(() => result.current.install());
    expect(firmware.install).not.toHaveBeenCalled();
    expect(result.current.errors).toEqual([expect.stringContaining("already tracked")]);
    expect(result.current.attempt).toEqual(other);
    expect(saved()).toEqual([other]);
  });
  it("refuses Start when another tab saved its update during this tab's upload", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      save(other);
      starting();
      throw new Error("Start was sent");
    });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.errors).toEqual([expect.stringContaining("already tracked")]);
    expect(result.current.attempt).toEqual(other);
    expect(saved()).toEqual([other]);
  });
  it("keeps an accepted update's record when a tab that claimed at the same moment is refused", async () => {
    const firmware = setup();
    const accepted = tracked("accepted");
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      // The controller accepted the other tab's Start and refuses this one.
      throw new HashboardFirmwareStartRefusedError("hashboard update already running");
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
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(() => result.current.choose(file));
    await act(() => result.current.install());
    expect(result.current.errors).toEqual(["hashboard update already running"]);
    expect(saved()).toEqual([accepted]);
    expect(result.current.attempt).toEqual(accepted);
  });
  it("keeps both records when two simultaneous Starts are uncertain, and shows them one at a time", async () => {
    const firmware = setup();
    const other = tracked("other-tab");
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.install.mockImplementation(async (_image, _attempt, _progress, _params, starting) => {
      starting();
      save(other);
      throw new Error("Lost response");
    });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
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
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await waitFor(() => expect(result.current.progress?.outcome).toBe("completed"));
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
    firmware.progress.mockResolvedValue({ boards: [], errors: [] });
    firmware.cancel.mockImplementation(async () => {
      removeAttempt("device", attempt.taskId);
      return { pending: true, finished: false, absent: [] };
    });
    const { result } = renderHook(() => useHashboardFirmware(firmware, "device"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(saved()).toEqual([]);
    expect(result.current.attempt).toBeUndefined();
  });
});
