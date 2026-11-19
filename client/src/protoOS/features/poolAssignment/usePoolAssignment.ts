import { useEffect, useEffectEvent, useId, useRef, useState } from "react";
import { readPoolAttempts, removePoolAttempt, savePoolAttempt } from "./attemptStorage";
import { withPoolWriteLock } from "./poolWriteLock";
import { poolAssignmentErrorMessage, startWorkerNameUpdate } from "./workerNameUpdates";
import type {
  AssignmentPool,
  PoolAssignmentAttempt,
  PoolAssignmentChoice,
  PoolAssignmentInspection,
  PoolAssignmentPlan,
  PoolAssignmentService,
  PoolAssignmentStatus,
  PoolAssignmentTarget,
  SavedPoolSet,
  WorkerNameChoice,
  WorkerNameReview,
  WorkerNameSource,
} from "@/protoOS/api/adapters/poolAssignment";
import {
  bulkRenameModes,
  type BulkRenamePreferences,
  createDefaultBulkRenamePreferences,
} from "@/shared/components/BulkRename/bulkRenameDefinitions";
import { usePoll } from "@/shared/hooks/usePoll";

interface WorkerNameDraft {
  sources: WorkerNameSource[];
  preferences: BulkRenamePreferences;
}

const STATUS_POLL_INTERVAL_MS = 3_000;
const MAX_STATUS_POLL_INTERVAL_MS = 60_000;

export function usePoolAssignment(
  service: PoolAssignmentService,
  controllerId: string,
  target: PoolAssignmentTarget,
  {
    openStatus,
    workerNames,
    onConfigurationSaved,
    onAssignmentSaved,
    onDismiss,
    workerNameUpdateOwner,
  }: {
    openStatus: boolean;
    workerNames: boolean;
    onConfigurationSaved: () => void;
    onAssignmentSaved: (message: string, toastAlreadyShown?: boolean) => void;
    onDismiss: () => void;
    workerNameUpdateOwner?: string;
  },
) {
  const [inspection, setInspection] = useState<PoolAssignmentInspection>();
  const [workerNameDraft, setWorkerNameDraft] = useState<WorkerNameDraft>();
  const [workerNameReview, setWorkerNameReview] = useState<WorkerNameReview>();
  const [reviewedPlan, setReviewedPlan] = useState<{ plan: PoolAssignmentPlan; replacesAttemptId?: string }>();
  const [attempt, setAttempt] = useState<PoolAssignmentAttempt | undefined>(() => readPoolAttempts(controllerId)[0]);
  const [openingAttemptId] = useState(attempt?.id);
  const [createdId, setCreatedId] = useState<string>();
  const [status, setStatus] = useState<PoolAssignmentStatus>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [updatingWorkerNames, setUpdatingWorkerNames] = useState(false);
  const fallbackOwner = useId();
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
      if (!controller.signal.aborted) setError(poolAssignmentErrorMessage(error));
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
  const inspectSelection = async (signal: AbortSignal) => {
    if (workerNames) {
      const sources = await service.inspectWorkerNames(target, { signal });
      signal.throwIfAborted();
      setWorkerNameDraft((edited) => ({
        sources,
        preferences: edited?.preferences ?? createDefaultBulkRenamePreferences(bulkRenameModes.worker),
      }));
    } else {
      const value = await service.inspect(target, { signal });
      signal.throwIfAborted();
      setInspection(value);
    }
  };
  const inspect = () => run(inspectSelection);
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
      setError(poolAssignmentErrorMessage(error));
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
          onAssignmentSaved(result.message);
        }
      });
    } finally {
      setReviewedPlan(undefined);
    }
  };
  const reviewPoolChoice = (choice: PoolAssignmentChoice) =>
    run(async (signal) => {
      const plan = await service.review(target, choice, { signal });
      signal.throwIfAborted();
      setReviewedPlan({ plan });
    });
  const reviewAssignment = (savedPoolSetId: string, name: string) =>
    reviewPoolChoice({ name, pools: [], savedPoolSetId });
  const editUsernames = (name: string, pools: AssignmentPool[], usernameSource: SavedPoolSet) =>
    reviewPoolChoice({ name, pools, usernameSource });
  const reviewWorkerNames = (choice: WorkerNameChoice) =>
    run(async (signal) => {
      const review = await service.reviewWorkerNames(target, choice, { signal });
      signal.throwIfAborted();
      if (review.groups.length) setWorkerNameReview(review);
      else onAssignmentSaved("Worker names are unchanged.");
    });
  const confirmWorkerNames = () =>
    run(async (signal) => {
      if (!workerNameReview?.groups.length) return;
      const update = startWorkerNameUpdate(
        service,
        controllerId,
        workerNameUpdateOwner ?? fallbackOwner,
        workerNameReview,
      );
      if (!update) throw new Error("A worker name update is already running on this controller.");
      setUpdatingWorkerNames(true);
      setWorkerNameReview(undefined);
      const result = await update;
      if (signal.aborted) return;
      setUpdatingWorkerNames(false);
      if (result.state === "saved") onAssignmentSaved(result.message, true);
      else if (result.state === "interrupted" || result.dismiss) onDismiss();
      else setError(result.message);
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
    enabled: target.macs.length > 0 && !openStatus && !(workerNames ? workerNameDraft : inspection) && !attempt,
    poll: false,
    params: target,
    fetchData: async (signal) => {
      try {
        await inspectSelection(signal);
        if (!signal.aborted) setError(undefined);
      } catch (error) {
        if (!signal.aborted) setError(poolAssignmentErrorMessage(error));
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
    reviewAssignment,
    editUsernames,
    workerNameDraft,
    setWorkerNameDraft,
    workerNameReview,
    updatingWorkerNames,
    reviewWorkerNames,
    confirmWorkerNames,
    backFromWorkerNames: () => setWorkerNameReview(undefined),
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
