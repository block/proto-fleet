import { useEffect, useMemo, useRef, useState } from "react";
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
const storageKey = (deviceId: string) => `proto-os:feed-mapping:${encodeURIComponent(deviceId)}`;
const load = (deviceId: string): Attempt | undefined => {
  try {
    const value = JSON.parse(localStorage.getItem(storageKey(deviceId)) || "null");
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
  const [attempt, setAttempt] = useState(() => load(deviceId));
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

  const save = (value: Attempt | undefined) => {
    // Do not erase an attempt another tab saved after this one.
    const saved = load(deviceId);
    if (!value && saved?.token !== current.current?.token) return;
    if (value) localStorage.setItem(storageKey(deviceId), JSON.stringify(value));
    else localStorage.removeItem(storageKey(deviceId));
    current.current = value;
    if (mounted.current) setAttempt(value);
  };
  const refresh = () => setRevision((value) => value + 1);
  const params = useMemo(() => ({ mapping, revision }), [mapping, revision]);
  usePoll({
    params,
    enabled: !activity,
    poll: true,
    pollIntervalMs: (attempt && !run) || mappingRunActive(run) ? 3000 : 15000,
    fetchData: async (signal) => {
      const controller = new AbortController();
      const abort = () => controller.abort();
      signal.addEventListener("abort", abort, { once: true });
      try {
        const result = await withDeadline(
          async () => {
            const options = { signal: controller.signal };
            const [assessment, active] = await Promise.allSettled([mapping.assess(options), mapping.active(options)]);
            let observed = active.status === "fulfilled" ? active.value : undefined;
            const saved = load(deviceId) ?? current.current;
            if (!observed && saved?.runId) observed = await mapping.get(saved.runId, options);
            return { assessment, active, observed, saved };
          },
          controller,
          15000,
        );
        if (signal.aborted || !mounted.current) return;
        if (result.assessment.status === "fulfilled") setAssessment(result.assessment.value);
        if (result.observed) {
          setRun(result.observed);
          save({ token: result.saved?.token ?? crypto.randomUUID(), runId: result.observed.id });
          if (!mappingRunActive(result.observed) && refreshedRun.current !== result.observed.id && onSettled) {
            await withDeadline(() => onSettled(controller.signal), controller, 15000);
            if (signal.aborted || !mounted.current) return;
            refreshedRun.current = result.observed.id;
          }
        } else if (result.saved) {
          current.current = result.saved;
          setAttempt(result.saved);
        }
        const failure =
          result.assessment.status === "rejected"
            ? result.assessment.reason
            : result.active.status === "rejected" && result.assessment.value.hardMappingAvailable
              ? result.active.reason
              : undefined;
        setError(failure ? mappingError(failure) : undefined);
        setReady(!failure && result.assessment.status === "fulfilled" && !result.assessment.value.unavailable);
      } catch (failure) {
        if (!signal.aborted && mounted.current) {
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
        // Re-read before Start: a plan is only a preview, not a reservation.
        const [fresh, active] = await Promise.all([mapping.assess(options), mapping.active(options)]);
        options.signal?.throwIfAborted();
        if (!mounted.current) return;
        if (active) {
          save({ token: crypto.randomUUID(), runId: active.id });
          setRun(active);
          throw new Error("A mapping run is already active.");
        }
        if (fresh.mode !== "manual" || !fresh.hardMappingAvailable || fresh.unavailable)
          throw new Error("Mapping availability changed. Review the current mapping before you continue.");
        const token = crypto.randomUUID();
        save({ token }); // Failure to persist refuses Start.
        setRun(undefined);
        try {
          const started = await mapping.start(
            plan.feeds.map((feed) => feed.id),
            options,
          );
          // Keep the recovery record even if navigation interrupted the response.
          save({ token, runId: started.id });
          if (mounted.current) setRun(started);
          return started;
        } catch (failure) {
          if ((failure as { writeOutcome?: string })?.writeOutcome === "refused") save(undefined);
          throw failure;
        }
      });
    },
    cancel: () =>
      run && mappingRunActive(run)
        ? perform("Canceling", async (options) => {
            const canceled = await mapping.cancel(run.id, options);
            if (mounted.current) setRun(canceled);
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
        save(undefined);
        setRun(undefined);
        setNotice(undefined);
        refresh();
      } catch (failure) {
        setNotice(mappingError(failure));
      }
    },
  };
}
