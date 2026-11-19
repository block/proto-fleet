import { useEffect, useEffectEvent, useRef, useState } from "react";
import { readPoolAttempts, removePoolAttempt, savePoolAttempt } from "./attemptStorage";
import { withPoolWriteLock } from "./poolWriteLock";
import { isAuthenticationError } from "@/protoOS/api/adapters/errors";
import type {
  AssignmentPool,
  PoolAssignmentAttempt,
  PoolAssignmentInspection,
  PoolAssignmentPlan,
  PoolAssignmentService,
  PoolAssignmentStatus,
  PoolAssignmentTarget,
  SavedPoolSet,
} from "@/protoOS/api/adapters/poolAssignment";
import { usePoll } from "@/shared/hooks/usePoll";

const STATUS_POLL_INTERVAL_MS = 3_000;
const MAX_STATUS_POLL_INTERVAL_MS = 60_000;

const message = (error: unknown) => {
  if (isAuthenticationError(error))
    return "Sign in to the controller, then check assignment status. Writes were not retried.";
  if (error instanceof Error) return error.message;
  return "The controller could not confirm the assignment. Check status before trying again.";
};

export function usePoolAssignment(
  service: PoolAssignmentService,
  controllerId: string,
  target: PoolAssignmentTarget,
  {
    openStatus,
    onConfigurationSaved,
    onAssignmentSaved,
  }: {
    openStatus: boolean;
    onConfigurationSaved: () => void;
    onAssignmentSaved: (status: PoolAssignmentStatus) => void;
  },
) {
  const [inspection, setInspection] = useState<PoolAssignmentInspection>();
  const [reviewedPlan, setReviewedPlan] = useState<{ plan: PoolAssignmentPlan; replacesAttemptId?: string }>();
  const [attempt, setAttempt] = useState<PoolAssignmentAttempt | undefined>(() => readPoolAttempts(controllerId)[0]);
  const [openingAttemptId] = useState(attempt?.id);
  const [createdId, setCreatedId] = useState<string>();
  const [status, setStatus] = useState<PoolAssignmentStatus>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const statusPollDelayMs = useRef(0);
  const checkingPrevious = Boolean(
    !openStatus && target.macs.length && attempt && attempt.id === openingAttemptId && !attempt.restoreLegacyMining,
  );
  const request = useRef<AbortController | null>(null);
  const run = async (operation: (signal: AbortSignal) => Promise<void>) => {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      await operation(controller.signal);
    } catch (error) {
      if (!controller.signal.aborted) setError(message(error));
    } finally {
      if (request.current === controller) {
        request.current = null;
        setBusy(false);
      }
    }
  };
  useEffect(
    () => () => {
      request.current?.abort();
      request.current = null;
    },
    [service, controllerId],
  );
  const inspect = () => run(async (signal) => setInspection(await service.inspect(target, { signal })));
  const forgetAttempt = (id: string) => {
    removePoolAttempt(controllerId, id);
    setAttempt(readPoolAttempts(controllerId)[0]);
    setStatus(undefined);
  };
  const recordStatus = (record: PoolAssignmentAttempt, result: PoolAssignmentStatus) => {
    if (checkingPrevious && result.state === "saved") {
      // Another tab may have updated the record while this status read was in flight.
      const latest = readPoolAttempts(controllerId).find((entry) => entry.id === record.id);
      if (!latest || JSON.stringify(latest) === JSON.stringify(record)) forgetAttempt(record.id);
      else {
        setAttempt(latest);
        setStatus(undefined);
      }
    } else setStatus(result);
    setError(undefined);
  };
  const readStatus = async (record: PoolAssignmentAttempt, signal: AbortSignal) => {
    try {
      const result = await service.status(record, { signal });
      if (signal.aborted) return;
      statusPollDelayMs.current = STATUS_POLL_INTERVAL_MS;
      recordStatus(record, result);
    } catch (error) {
      if (signal.aborted) return;
      statusPollDelayMs.current = Math.min(
        Math.max(statusPollDelayMs.current, STATUS_POLL_INTERVAL_MS) * 2,
        MAX_STATUS_POLL_INTERVAL_MS,
      );
      setError(message(error));
    }
  };
  const check = () =>
    run(async (signal) => {
      if (attempt) await readStatus(attempt, signal);
    });
  const checkMiningRecovery = () =>
    run((signal) =>
      withPoolWriteLock(controllerId, signal, async (writeSignal) => {
        const current = readPoolAttempts(controllerId).find(({ id }) => id === attempt?.id);
        if (!current?.restoreLegacyMining) {
          setAttempt(current);
          setStatus(undefined);
          return;
        }
        const track = (record: PoolAssignmentAttempt) => {
          savePoolAttempt(controllerId, record);
          setAttempt(record);
        };
        setStatus(await service.checkMiningRecovery(current, controllerId, track, { signal: writeSignal }));
      }),
    );
  const reviewRemaining = () =>
    run(async (signal) => {
      if (!attempt?.destinationId || !status?.remainingMacs?.length) return;
      const plan = await service.review(
        { macs: status.remainingMacs },
        { pools: [], savedPoolSetId: attempt.destinationId },
        { signal },
      );
      signal.throwIfAborted();
      setReviewedPlan({ plan, replacesAttemptId: attempt.id });
    });
  const applyPlan = async (planned: PoolAssignmentPlan, signal: AbortSignal, replacesAttemptId?: string) => {
    let latest: PoolAssignmentAttempt | undefined;
    const track = (record: PoolAssignmentAttempt) => {
      savePoolAttempt(controllerId, record);
      if (!latest) {
        if (replacesAttemptId) removePoolAttempt(controllerId, replacesAttemptId);
        setStatus(undefined);
      }
      latest = record;
      setAttempt(record);
    };
    try {
      await withPoolWriteLock(controllerId, signal, async (writeSignal) => {
        const tracked = readPoolAttempts(controllerId);
        if (tracked.some((record) => record.id !== replacesAttemptId))
          throw new Error("Another pool assignment is tracked in this browser. Check its status first.");
        if (replacesAttemptId && !tracked.length)
          throw new Error("The original pool assignment is no longer tracked. Check its status first.");
        const result = await service.apply(planned, track, { signal: writeSignal });
        writeSignal.throwIfAborted();
        if (result.state !== "saved" || latest?.restoreLegacyMining) {
          setStatus(result);
          return;
        }
        forgetAttempt(planned.id);
        if (planned.configurationOnly) {
          setCreatedId(latest?.destinationId);
          if (target.macs.length) setInspection(undefined);
          onConfigurationSaved();
        } else {
          onAssignmentSaved(result);
        }
      });
    } finally {
      setReviewedPlan(undefined);
    }
  };
  const assign = (savedPoolSetId: string, name: string, singleSelection: boolean) =>
    run(async (signal) => {
      const plan = await service.review(target, { name, pools: [], savedPoolSetId }, { signal });
      signal.throwIfAborted();
      if (singleSelection && !plan.requiresActivation) await applyPlan(plan, signal);
      else setReviewedPlan({ plan });
    });
  const create = (name: string, pools: AssignmentPool[], original?: SavedPoolSet) =>
    run(async (signal) => {
      const planned = await service.review(
        target,
        {
          name,
          pools,
          createOnly: !original,
          ...(original
            ? { editPoolSetId: original.id, expectedRevision: original.revision, expectedPools: original.pools }
            : {}),
        },
        { signal },
      );
      signal.throwIfAborted();
      if (planned.requiresActivation || (planned.editingRevision && planned.target.macs.length))
        setReviewedPlan({ plan: planned });
      else await applyPlan(planned, signal);
    });
  const confirm = () =>
    run(async (signal) => {
      if (reviewedPlan) await applyPlan(reviewedPlan.plan, signal, reviewedPlan.replacesAttemptId);
    });
  usePoll({
    enabled: target.macs.length > 0 && !inspection && !attempt,
    poll: false,
    params: target,
    fetchData: async (signal) => {
      try {
        const value = await service.inspect(target, { signal });
        if (!signal.aborted) {
          setInspection(value);
          setError(undefined);
        }
      } catch (error) {
        if (!signal.aborted) setError(message(error));
      }
    },
  });
  const pollStatus = useEffectEvent(readStatus);
  const canPollStatus = status === undefined || status.state === "pending";
  useEffect(() => {
    statusPollDelayMs.current = 0;
  }, [attempt, controllerId, service]);
  useEffect(() => {
    if (!attempt || busy || !canPollStatus) return;
    const controller = new AbortController();
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      await pollStatus(attempt, controller.signal);
      if (!controller.signal.aborted) timeout = setTimeout(poll, statusPollDelayMs.current);
    };
    if (statusPollDelayMs.current) timeout = setTimeout(poll, statusPollDelayMs.current);
    else void poll();
    return () => {
      controller.abort();
      clearTimeout(timeout);
    };
  }, [attempt, busy, canPollStatus, controllerId, service]);
  return {
    inspection,
    createdId,
    create,
    plan: reviewedPlan?.plan,
    attempt: checkingPrevious && status === undefined && !error ? undefined : attempt,
    status,
    error,
    busy,
    inspect,
    assign,
    reviewRemaining,
    confirm,
    check,
    checkMiningRecovery,
    back: () => setReviewedPlan(undefined),
    clear: () => {
      if (!busy && attempt) forgetAttempt(attempt.id);
    },
  };
}
