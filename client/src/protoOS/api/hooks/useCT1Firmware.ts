import { useEffect, useRef, useState } from "react";
import type {
  CT1Firmware,
  CT1FirmwareAttempt,
  CT1FirmwareBoard,
  CT1FirmwareImage,
  CT1FirmwareProgress,
} from "../adapters/ct1Firmware";
import { readFirmwareImage } from "../adapters/mdkV2/firmwareImage";
import { usePoll } from "@/shared/hooks/usePoll";

const message = (error: unknown) =>
  error instanceof Error
    ? error.message
    : "The controller could not confirm the operation. Check the update status before trying again.";

export function useCT1Firmware(firmware: CT1Firmware, deviceId: string) {
  const key = `proto-os:ct1-firmware:${deviceId}`;
  const [attempt, setAttempt] = useState<CT1FirmwareAttempt | undefined>(() => {
    try {
      const value = JSON.parse(localStorage.getItem(key) || "null") as CT1FirmwareAttempt | null;
      if (
        value?.taskId &&
        Array.isArray(value.boards) &&
        value.boards.length &&
        value.boards.every(
          (board) =>
            typeof board.mac === "string" && typeof board.owner === "string" && typeof board.label === "string",
        )
      )
        return value;
    } catch {
      /* Invalid or inaccessible storage is handled before starting. */
    }
    return undefined;
  });
  const [image, setImage] = useState<CT1FirmwareImage>();
  const [boards, setBoards] = useState<CT1FirmwareBoard[]>([]);
  const [progress, setProgress] = useState<CT1FirmwareProgress>();
  const [error, setError] = useState<string>();
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
      // getRandomValues is also available on non-secure HTTP origins.
      const taskId = Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) =>
        value.toString(16).padStart(2, "0"),
      ).join("");
      const planned = { taskId, boards, build: image.build, version: image.version };
      setUploadPercent(0);
      await firmware.install(
        image,
        planned,
        (percent) => {
          if (mounted.current) setUploadPercent(percent);
        },
        { signal: AbortSignal.any([signal, AbortSignal.timeout(120_000)]) },
        () => {
          // A storage failure refuses Start rather than losing recovery information.
          localStorage.setItem(key, JSON.stringify(planned));
          starting.current = true;
          if (mounted.current) setAttempt(planned);
        },
      );
      if (mounted.current) setImage(undefined);
    });

  const clear = () => {
    localStorage.removeItem(key);
    setAttempt(undefined);
    setImage(undefined);
    setBoards([]);
    setProgress(undefined);
    setError(undefined);
  };

  const cancel = () => {
    if (request.current && !starting.current) {
      request.current.abort();
      return;
    }
    if (attempt)
      return run(async (signal) => {
        const canceled = await firmware.cancel(attempt, {
          signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]),
        });
        signal.throwIfAborted();
        if (canceled.pending) {
          const remaining = { ...attempt, boards: canceled.boards, cancelRequested: true };
          localStorage.setItem(key, JSON.stringify(remaining));
          setAttempt(remaining);
          setProgress(undefined);
        } else clear();
      });
  };

  return { image, boards, attempt, progress, error, busy, uploadPercent, choose, install, cancel, clear };
}
