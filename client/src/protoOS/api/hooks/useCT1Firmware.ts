import { useEffect, useRef, useState } from "react";
import {
  type CT1Firmware,
  type CT1FirmwareAttempt,
  type CT1FirmwareBoard,
  type CT1FirmwareImage,
  type CT1FirmwareProgress,
  CT1FirmwareStartRefusedError,
} from "../adapters/ct1Firmware";
import { readFirmwareImage } from "../adapters/mdkV2/firmwareImage";
import { usePoll } from "@/shared/hooks/usePoll";

const message = (error: unknown) =>
  error instanceof Error
    ? error.message
    : "The controller could not confirm the operation. Check the update status before trying again.";

const alreadyTracked =
  "Another hashboard update is already tracked in this browser. Check its status before starting a new one.";

/** The saved record, or undefined when storage is empty, inaccessible, or holds something else. */
const readAttempt = (key: string): CT1FirmwareAttempt | undefined => {
  try {
    const value = JSON.parse(localStorage.getItem(key) || "null") as CT1FirmwareAttempt | null;
    if (
      value?.taskId &&
      Array.isArray(value.boards) &&
      value.boards.length &&
      value.boards.every(
        (board) => typeof board.mac === "string" && typeof board.owner === "string" && typeof board.label === "string",
      )
    )
      return value;
  } catch {
    /* Invalid or inaccessible storage is handled before starting. */
  }
  return undefined;
};

export function useCT1Firmware(firmware: CT1Firmware, deviceId: string) {
  const key = `proto-os:ct1-firmware:${deviceId}`;
  const [attempt, setAttempt] = useState(() => readAttempt(key));
  const [image, setImage] = useState<CT1FirmwareImage>();
  const [boards, setBoards] = useState<CT1FirmwareBoard[]>([]);
  const [progress, setProgress] = useState<CT1FirmwareProgress>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [uploadPercent, setUploadPercent] = useState(0);
  // The last cancel left an owner unreachable or unaware of the task, so only the operator can release the record.
  const [cancelUnconfirmed, setCancelUnconfirmed] = useState(false);
  const request = useRef<AbortController | null>(null);
  const starting = useRef(false);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
    };
  }, []);

  usePoll({
    params: attempt,
    enabled: !!attempt && !busy && !progress?.complete,
    poll: true,
    pollIntervalMs: 2000,
    fetchData: async (signal) => {
      if (!attempt) return;
      try {
        const result = await firmware.progress(attempt, {
          signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]),
        });
        if (!signal.aborted) setProgress(result);
      } catch (failure) {
        if (!signal.aborted) setError(message(failure));
      }
    },
  });

  // One update is tracked per device, and another tab on this origin may have saved or released the record since
  // this one last read it. Every write and removal re-reads first and leaves a different task's record alone.
  const track = (planned: CT1FirmwareAttempt) => {
    const stored = readAttempt(key);
    if (stored) {
      if (mounted.current) setAttempt(stored);
      throw new Error(alreadyTracked);
    }
    // A storage failure refuses Start rather than losing recovery information.
    localStorage.setItem(key, JSON.stringify(planned));
    if (mounted.current) setAttempt(planned);
  };
  const retrack = (record: CT1FirmwareAttempt) => {
    const stored = readAttempt(key);
    const own = stored?.taskId === record.taskId;
    if (own) localStorage.setItem(key, JSON.stringify(record));
    setAttempt(own ? record : stored);
  };
  /** Releases this task's record and returns the one another tab holds instead, if any. */
  const untrack = (record: CT1FirmwareAttempt) => {
    const stored = readAttempt(key);
    if (stored && stored.taskId !== record.taskId) return stored;
    localStorage.removeItem(key);
    return undefined;
  };

  const run = async (operation: (signal: AbortSignal) => Promise<void>) => {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      await operation(controller.signal);
    } catch (failure) {
      if (mounted.current && !controller.signal.aborted) setError(message(failure));
    } finally {
      request.current = null;
      starting.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  const choose = (file: File) =>
    run(async (signal) => {
      setImage(undefined);
      setBoards([]);
      setProgress(undefined);
      const selected = await readFirmwareImage(file);
      const scope = await firmware.prepare({ signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]) });
      signal.throwIfAborted();
      setImage(selected);
      setBoards(scope);
    });

  const install = () =>
    run(async (signal) => {
      if (!image || !boards.length || attempt) return;
      const tracked = readAttempt(key);
      if (tracked) {
        setAttempt(tracked);
        throw new Error(alreadyTracked);
      }
      // getRandomValues is also available on non-secure HTTP origins.
      const taskId = Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) =>
        value.toString(16).padStart(2, "0"),
      ).join("");
      const planned = { taskId, boards, build: image.build, version: image.version };
      setUploadPercent(0);
      try {
        await firmware.install(
          image,
          planned,
          (percent) => {
            if (mounted.current) setUploadPercent(percent);
          },
          { signal: AbortSignal.any([signal, AbortSignal.timeout(120_000)]) },
          () => {
            track(planned);
            starting.current = true;
          },
        );
      } catch (failure) {
        // The controller created no job: release the record and stay on the selection with its reason.
        if (failure instanceof CT1FirmwareStartRefusedError) {
          const other = untrack(planned);
          if (mounted.current) setAttempt(other);
        }
        throw failure;
      }
      if (mounted.current) setImage(undefined);
    });

  const clear = () => {
    setAttempt(attempt ? untrack(attempt) : readAttempt(key));
    setImage(undefined);
    setBoards([]);
    setProgress(undefined);
    setError(undefined);
    setCancelUnconfirmed(false);
  };

  const cancel = () => {
    if (request.current && !starting.current) {
      request.current.abort();
      return;
    }
    if (attempt)
      return run(async (signal) => {
        let canceled;
        try {
          canceled = await firmware.cancel(attempt, {
            signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]),
          });
        } catch (failure) {
          if (mounted.current && !signal.aborted) setCancelUnconfirmed(true);
          throw failure;
        }
        signal.throwIfAborted();
        const unconfirmed = canceled.absent.length > 0;
        setCancelUnconfirmed(unconfirmed);
        // Clear only once every owner confirmed a canceled job. Boards still stopping and owners that do not know
        // the task stay tracked; a job that ended before the cancel arrived keeps its result until Done.
        if (canceled.pending || unconfirmed) {
          retrack({ ...attempt, cancelRequested: true });
          setProgress(undefined);
        } else if (!canceled.finished) clear();
      });
  };

  return {
    image,
    boards,
    attempt,
    progress,
    error,
    busy,
    uploadPercent,
    cancelUnconfirmed,
    choose,
    install,
    cancel,
    clear,
  };
}
