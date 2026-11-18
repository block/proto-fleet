import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type FeedMapping,
  type MappingAssessment,
  mappingError,
  type MappingRun,
  mappingRunActive,
} from "@/protoOS/api/adapters/feedMapping";
import type { AdapterRequestOptions } from "@/protoOS/api/adapters/types";
import { usePoll } from "@/shared/hooks/usePoll";
import { withDeadline } from "@/shared/utils/withDeadline";

interface Attempt {
  token: string;
  runId?: string;
}
// Direct controllers can serve ProtoOS over HTTP, where randomUUID is unavailable.
const newAttemptToken = () =>
  Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) => value.toString(16).padStart(2, "0")).join("");
const storageKey = (deviceId: string) => `proto-os:feed-mapping:${encodeURIComponent(deviceId)}`;
const sameAttempt = (a: Attempt | undefined, b: Attempt | undefined) => a?.token === b?.token && a?.runId === b?.runId;
const load = (deviceId: string): Attempt | undefined => {
  // A failed read is not a deletion. Let callers retain state and report the failure.
  const stored = localStorage.getItem(storageKey(deviceId));
  try {
    const value = JSON.parse(stored || "null");
    return value && typeof value.token === "string" && (value.runId === undefined || typeof value.runId === "string")
      ? value
      : undefined;
  } catch {
    return undefined;
  }
};

/** Mounted with a key for the device and connection generation; writes never follow a changed connection. */
export function useFeedMapping(
  mapping: FeedMapping,
  deviceId: string,
  onSettled?: (signal: AbortSignal) => Promise<void>,
) {
  const [assessment, setAssessment] = useState<MappingAssessment>();
  const [run, setRun] = useState<MappingRun>();
  const [runningConfirmed, setRunningConfirmed] = useState(false);
  const [attempt, setAttempt] = useState(() => {
    try {
      return load(deviceId);
    } catch {
      return undefined; // The first poll reports storage failures.
    }
  });
  const current = useRef(attempt);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [activity, setActivity] = useState<string>();
  const [ready, setReady] = useState(false);
  const [revision, setRevision] = useState(0);
  const busy = useRef(false);
  const mounted = useRef(true);
  const request = useRef<AbortController | undefined>(undefined);
  const refreshedRun = useRef<string | undefined>(undefined);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
    };
  }, []);

  const syncAttempt = useCallback((value: Attempt | undefined) => {
    const changed = !sameAttempt(current.current, value);
    current.current = value;
    if (!mounted.current) return;
    setAttempt(value);
    if (changed) {
      setRun(undefined);
      setRunningConfirmed(false);
    }
  }, []);
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key !== null && event.key !== storageKey(deviceId)) return;
      try {
        if (event.storageArea !== localStorage) return;
        syncAttempt(load(deviceId));
        setRevision((value) => value + 1);
      } catch (failure) {
        setError(mappingError(failure));
        setReady(false);
        setRunningConfirmed(false);
      }
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [deviceId, syncAttempt]);

  const save = (value: Attempt | undefined, expected: Attempt | undefined) => {
    // A delayed response must not restore a deleted record or replace a newer attempt.
    const saved = load(deviceId);
    if (!sameAttempt(saved, expected)) {
      syncAttempt(saved);
      return false;
    }
    if (value) localStorage.setItem(storageKey(deviceId), JSON.stringify(value));
    else localStorage.removeItem(storageKey(deviceId));
    syncAttempt(value);
    return true;
  };
  const refresh = () => setRevision((value) => value + 1);
  const params = useMemo(() => ({ mapping, revision }), [mapping, revision]);
  usePoll({
    params,
    enabled: !activity,
    poll: true,
    pollIntervalMs: (attempt && !run) || mappingRunActive(run) ? 3000 : 15000,
    fetchData: async (signal) => {
      let confirmedRunning = false;
      const controller = new AbortController();
      const abort = () => controller.abort();
      signal.addEventListener("abort", abort, { once: true });
      try {
        const saved = load(deviceId);
        syncAttempt(saved);
        const result = await withDeadline(
          async () => {
            const options = { signal: controller.signal };
            const [assessment, active] = await Promise.allSettled([mapping.assess(options), mapping.active(options)]);
            let observed = active.status === "fulfilled" ? active.value : undefined;
            let missing = false;
            let statusError: unknown;
            if (!observed && saved?.runId) {
              try {
                observed = await mapping.get(saved.runId, options);
                missing = observed === null;
              } catch (failure) {
                statusError = failure;
              }
            }
            return { assessment, active, observed, saved, missing, statusError };
          },
          controller,
          15000,
        );
        if (signal.aborted || !mounted.current) return;
        const latest = load(deviceId);
        if (!sameAttempt(saved, latest)) {
          syncAttempt(latest);
          refresh();
          return;
        }
        confirmedRunning = result.observed?.status === "running";
        setRunningConfirmed(confirmedRunning);
        if (result.assessment.status === "fulfilled") setAssessment(result.assessment.value);
        if (result.missing) setRun(undefined);
        if (result.observed) {
          if (!save({ token: result.saved?.token ?? newAttemptToken(), runId: result.observed.id }, saved)) {
            refresh();
            return;
          }
          setRun(result.observed);
          setRunningConfirmed(confirmedRunning);
          if (!mappingRunActive(result.observed) && refreshedRun.current !== result.observed.id && onSettled) {
            await withDeadline(() => onSettled(controller.signal), controller, 15000);
            if (signal.aborted || !mounted.current) return;
            refreshedRun.current = result.observed.id;
          }
        }
        const failure =
          result.statusError ??
          (result.assessment.status === "rejected"
            ? result.assessment.reason
            : result.active.status === "rejected" && result.assessment.value.hardMappingAvailable
              ? result.active.reason
              : undefined);
        setError(failure ? mappingError(failure) : undefined);
        setReady(!failure && result.assessment.status === "fulfilled" && !result.assessment.value.unavailable);
      } catch (failure) {
        if (!signal.aborted && mounted.current) {
          setRunningConfirmed(confirmedRunning);
          setError(mappingError(failure));
          setReady(false);
        }
      } finally {
        signal.removeEventListener("abort", abort);
      }
    },
  });

  const perform = async <T>(
    label: string,
    action: (options: AdapterRequestOptions) => Promise<T>,
  ): Promise<T | undefined> => {
    if (busy.current || !mounted.current) return;
    busy.current = true;
    setActivity(label);
    setNotice(undefined);
    request.current = new AbortController();
    try {
      return await withDeadline(() => action({ signal: request.current!.signal }), request.current, 15000);
    } catch (failure) {
      if (mounted.current) setNotice(mappingError(failure));
      return undefined;
    } finally {
      busy.current = false;
      if (mounted.current) {
        setActivity(undefined);
        refresh();
      }
    }
  };

  const unresolved = !!attempt && (!attempt.runId || attempt.runId !== run?.id || mappingRunActive(run));
  const canRemap =
    ready && !activity && !unresolved && assessment?.mode === "manual" && assessment.hardMappingAvailable;
  const canVerify = ready && !activity && !unresolved && assessment?.mode === "switch_ports";
  return {
    assessment,
    run,
    runningConfirmed,
    error,
    notice,
    activity,
    ready,
    unresolved,
    canRemap,
    canVerify,
    refresh,
    plan: () => (canRemap ? perform("Planning", (options) => mapping.plan(options)) : Promise.resolve(undefined)),
    start: (plan: MappingRun) => {
      if (!canRemap) return Promise.resolve(undefined);
      return perform("Starting", async (options) => {
        const previous = load(deviceId);
        if (!sameAttempt(previous, current.current)) {
          syncAttempt(previous);
          throw new Error("The saved mapping attempt changed. Check its status before starting another run.");
        }
        // Re-read before Start: a plan is only a preview, not a reservation.
        const [fresh, active] = await Promise.all([mapping.assess(options), mapping.active(options)]);
        options.signal?.throwIfAborted();
        if (!mounted.current) return;
        if (active) {
          if (save({ token: newAttemptToken(), runId: active.id }, previous)) {
            setRun(active);
            setRunningConfirmed(active.status === "running");
          }
          throw new Error("A mapping run is already active.");
        }
        if (fresh.mode !== "manual" || !fresh.hardMappingAvailable || fresh.unavailable)
          throw new Error("Mapping availability changed. Review the current mapping before you continue.");
        const token = newAttemptToken();
        // Failure to persist, or a newer attempt from another tab, refuses Start.
        if (!save({ token }, previous))
          throw new Error("The saved mapping attempt changed. Check its status before starting another run.");
        setRun(undefined);
        setRunningConfirmed(false);
        try {
          const started = await mapping.start(
            plan.feeds.map((feed) => feed.id),
            options,
          );
          // Keep the recovery record even if navigation interrupted the response.
          const saved = save({ token, runId: started.id }, { token });
          if (saved && mounted.current) {
            setRun(started);
            setRunningConfirmed(started.status === "running");
          }
          return started;
        } catch (failure) {
          if ((failure as { writeOutcome?: string })?.writeOutcome === "refused") save(undefined, { token });
          throw failure;
        }
      });
    },
    cancel: () =>
      run && mappingRunActive(run)
        ? perform("Canceling", async (options) => {
            const canceled = await mapping.cancel(run.id, options);
            if (mounted.current) {
              setRun(canceled);
              setRunningConfirmed(canceled.status === "running");
            }
            return canceled;
          })
        : Promise.resolve(undefined),
    verify: (macs: string[]) =>
      canVerify
        ? perform("Queuing verification", async (options) => {
            const fresh = await mapping.assess(options);
            options.signal?.throwIfAborted();
            if (!mounted.current) return;
            if (fresh.mode !== "switch_ports" || fresh.unavailable)
              throw new Error("Verification availability changed. Refresh the mapping before you continue.");
            const present = new Set(fresh.boards.map((board) => board.mac));
            if (macs.some((mac) => !present.has(mac)))
              throw new Error("The selected hashboards changed. Review the mapping before you continue.");
            await mapping.verify(macs, options);
            if (mounted.current)
              setNotice("Verification is queued. The controller will check the selected hashboards when it is ready.");
          })
        : Promise.resolve(undefined),
    clear: () => {
      try {
        save(undefined, current.current);
        setRun(undefined);
        setRunningConfirmed(false);
        setNotice(undefined);
        refresh();
      } catch (failure) {
        setNotice(mappingError(failure));
      }
    },
  };
}
