import { useEffect, useRef, useState } from "react";
import { readPoolAttempts, removePoolAttempt, savePoolAttempt } from "./attemptStorage";
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
  onConfigurationSaved?: () => void,
) {
  const [inspection, setInspection] = useState<PoolAssignmentInspection>();
  const [reviewedPlan, setReviewedPlan] = useState<{ plan: PoolAssignmentPlan; replacesAttemptId?: string }>();
  const [attempt, setAttempt] = useState(() => readPoolAttempts(controllerId)[0]);
  const [createdId, setCreatedId] = useState<string>();
  const [status, setStatus] = useState<PoolAssignmentStatus>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  const run = async (operation: (signal: AbortSignal) => Promise<void>) => {
    if (request.current) return false;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      await operation(controller.signal);
      return !controller.signal.aborted;
    } catch (error) {
      if (!controller.signal.aborted) setError(message(error));
      return false;
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
  const check = () =>
    run(async (signal) => {
      if (attempt) setStatus(await service.status(attempt, { signal }));
    });
  const restoreMining = () =>
    run(async (signal) => {
      if (!attempt?.restoreLegacyMining) return;
      const track = (record: PoolAssignmentAttempt) => {
        savePoolAttempt(controllerId, record);
        setAttempt(record);
      };
      setStatus(await service.restoreMining(attempt, track, { signal }));
    });
  const review = (pools: AssignmentPool[], savedPoolSetId?: string, selectedMacs = target.macs, name?: string) =>
    run(async (signal) => {
      setReviewedPlan(undefined);
      const plan = await service.review({ ...target, macs: selectedMacs }, { name, pools, savedPoolSetId }, { signal });
      signal.throwIfAborted();
      setReviewedPlan({ plan });
    });
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
      const tracked = readPoolAttempts(controllerId);
      if (tracked.some((record) => record.id !== replacesAttemptId))
        throw new Error("Another pool assignment is tracked in this browser. Check its status first.");
      if (replacesAttemptId && !tracked.length)
        throw new Error("The original pool assignment is no longer tracked. Check its status first.");
      const result = await service.apply(planned, track, { signal });
      if (planned.configurationOnly && result.state === "saved" && !latest?.restoreLegacyMining) {
        setCreatedId(latest?.destinationId);
        removePoolAttempt(controllerId, planned.id);
        setAttempt(readPoolAttempts(controllerId)[0]);
        setStatus(undefined);
        if (target.macs.length) setInspection(undefined);
        onConfigurationSaved?.();
      } else setStatus(result);
    } finally {
      setReviewedPlan(undefined);
    }
  };
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
      if (planned.requiresActivation) setReviewedPlan({ plan: planned });
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
  usePoll({
    enabled: !!attempt && !busy && (status === undefined || status.state === "pending"),
    poll: true,
    pollIntervalMs: 3000,
    params: attempt?.id,
    fetchData: async (signal) => {
      if (!attempt) return;
      try {
        const value = await service.status(attempt, { signal });
        if (!signal.aborted) {
          setStatus(value);
          setError(undefined);
        }
      } catch (error) {
        if (!signal.aborted) setError(message(error));
      }
    },
  });
  return {
    inspection,
    createdId,
    create,
    plan: reviewedPlan?.plan,
    attempt,
    status,
    error,
    busy,
    inspect,
    review,
    reviewRemaining,
    confirm,
    check,
    restoreMining,
    back: () => setReviewedPlan(undefined),
    clear: () => {
      if (!busy && attempt) {
        removePoolAttempt(controllerId, attempt.id);
        setAttempt(readPoolAttempts(controllerId)[0]);
        setStatus(undefined);
      }
    },
  };
}
