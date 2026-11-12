import { useEffect, useRef, useState } from "react";
import {
  type HashboardFirmware,
  type HashboardFirmwareAttempt,
  type HashboardFirmwareBoard,
  HashboardFirmwareCancelError,
  type HashboardFirmwareImage,
  type HashboardFirmwareProgress,
  HashboardFirmwareStartRefusedError,
} from "../adapters/hashboardFirmware";
import { readAttempt, readAttempts, removeAttempt, saveAttempt } from "./hashboardFirmwareStorage";
import { usePoll } from "@/shared/hooks/usePoll";

const message = (error: unknown) =>
  error instanceof Error
    ? error.message
    : "The controller could not confirm the operation. Check the update status before trying again.";
/** One line per failure: a cancel reports every owner that could not answer on a line of its own. */
const messages = (error: unknown) =>
  error instanceof HashboardFirmwareCancelError ? error.failures : [message(error)];

const alreadyTracked =
  "Another hashboard update is already tracked in this browser. Check its status before starting a new one.";

export function useHashboardFirmware(firmware: HashboardFirmware, deviceId: string) {
  // One update is shown at a time. Any other saved record is adopted once this one is released.
  const [attempt, setAttempt] = useState(() => readAttempt(deviceId));
  const [image, setImage] = useState<HashboardFirmwareImage>();
  const [boards, setBoards] = useState<HashboardFirmwareBoard[]>([]);
  const [progress, setProgress] = useState<HashboardFirmwareProgress>();
  const [errors, setErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [uploadPercent, setUploadPercent] = useState(0);
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
    enabled: !!attempt && !busy && !progress?.outcome,
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
        if (!signal.aborted) setErrors(messages(failure));
      }
    },
  });

  // Records are saved per task (see hashboardFirmwareStorage), so this tab only ever writes or removes its own
  // task's record. The checks below keep a second tab from starting another update in the ordinary, sequential
  // case; two tabs that pass them at the same moment each keep their own record, and the controller refuses one
  // Start.
  const track = (planned: HashboardFirmwareAttempt) => {
    const other = readAttempt(deviceId);
    if (other) {
      if (mounted.current) setAttempt(other);
      throw new Error(alreadyTracked);
    }
    // A storage failure refuses Start rather than losing recovery information.
    saveAttempt(deviceId, planned);
    if (mounted.current) setAttempt(planned);
  };
  /** Rewrites this task's record unless another tab already released it. */
  const retrack = (record: HashboardFirmwareAttempt) => {
    const saved = readAttempts(deviceId);
    const own = saved.some((candidate) => candidate.taskId === record.taskId);
    if (own) saveAttempt(deviceId, record);
    setAttempt(own ? record : saved[0]);
  };
  /** Releases this task's record and returns another saved record to show next, if any. */
  const untrack = (record: HashboardFirmwareAttempt) => {
    removeAttempt(deviceId, record.taskId);
    return readAttempt(deviceId);
  };

  const run = async (operation: (signal: AbortSignal) => Promise<void>) => {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setErrors([]);
    try {
      await operation(controller.signal);
    } catch (failure) {
      if (mounted.current && !controller.signal.aborted) setErrors(messages(failure));
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
      const selected = await firmware.readImage(file);
      const scope = await firmware.prepare({ signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]) });
      signal.throwIfAborted();
      setImage(selected);
      setBoards(scope);
    });

  const install = () =>
    run(async (signal) => {
      if (!image || !boards.length || attempt) return;
      const tracked = readAttempt(deviceId);
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
        if (failure instanceof HashboardFirmwareStartRefusedError) {
          const other = untrack(planned);
          if (mounted.current) setAttempt(other);
        }
        throw failure;
      }
      if (mounted.current) setImage(undefined);
    });

  const clear = () => {
    setAttempt(attempt ? untrack(attempt) : readAttempt(deviceId));
    setImage(undefined);
    setBoards([]);
    setProgress(undefined);
    setErrors([]);
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
          // An owner could not be reached. Save that with the record, so the release stays on offer after a reload.
          if (!signal.aborted) retrack({ ...attempt, cancel: "unconfirmed" });
          throw failure;
        }
        signal.throwIfAborted();
        // Clear only once every owner confirmed a canceled job. Owners that do not know the task and boards still
        // stopping stay tracked; a job that ended before the cancel arrived keeps its result until Done.
        if (canceled.absent.length || canceled.pending) {
          retrack({ ...attempt, cancel: canceled.absent.length ? "unconfirmed" : "requested" });
          setProgress(undefined);
        } else if (canceled.finished) retrack({ ...attempt, cancel: undefined });
        else clear();
      });
  };

  return {
    image,
    boards,
    attempt,
    progress,
    errors,
    busy,
    uploadPercent,
    choose,
    install,
    cancel,
    clear,
  };
}
