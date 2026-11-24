import { useLayoutEffect } from "react";
import { create } from "zustand";
import { readPoolAttempts, removePoolAttempt, savePoolAttempt } from "./attemptStorage";
import { withPoolWriteLock } from "./poolWriteLock";
import { isAuthenticationError } from "@/protoOS/api/adapters/errors";
import {
  type PoolAssignmentAttempt,
  type PoolAssignmentService,
  type WorkerNameReview,
  WorkerNameUpdateRolledBack,
} from "@/protoOS/api/adapters/poolAssignment";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { pushToast, removeToast, STATUSES, updateToast } from "@/shared/features/toaster";
import { withDeadline } from "@/shared/utils/withDeadline";

const STATUS_POLL_INTERVAL_MS = 3_000;
const MAX_STATUS_FAILURES = 3;
const PENDING_STATUS_DEADLINE_MS = 60_000;

type WorkerNameUpdateService = Pick<PoolAssignmentService, "applyWorkerNames" | "status">;
type ActiveUpdate = { owner: string; service: WorkerNameUpdateService; controller: AbortController };
type UpdateResult =
  | { state: "saved"; message: string }
  | { state: "failed"; message: string; dismiss: boolean }
  | { state: "interrupted" };

// The controller owns the operation. A page can stop displaying its editor
// without stopping the operation or allowing another pool write to start.
const useActiveUpdates = create<Map<string, ActiveUpdate>>(() => new Map());

export function useWorkerNameUpdateOwner(controllerId?: string) {
  return useActiveUpdates((updates) => (controllerId ? updates.get(controllerId)?.owner : undefined));
}

export function hasWorkerNameUpdate(controllerId: string) {
  return useActiveUpdates.getState().has(controllerId);
}

export function useWorkerNameUpdateScope() {
  const { adapter, isFleetHosted } = useMinerHosting();
  const controllerId = useMinerStore((state) => state.systemInfo.data?.identity.deviceId);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const service = isFleetHosted ? undefined : adapter?.poolAssignment;
  useLayoutEffect(
    () => () => {
      const active = controllerId ? useActiveUpdates.getState().get(controllerId) : undefined;
      if (active && active.service === service) active.controller.abort();
    },
    [controllerId, generation, service],
  );
}

export function poolAssignmentErrorMessage(error: unknown) {
  if (isAuthenticationError(error))
    return "Sign in to the controller, then check assignment status. Writes were not retried.";
  if (error instanceof Error) return error.message;
  return "The controller could not confirm the assignment. Check status before trying again.";
}

function waitForStatus(delay: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal.reason);
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, delay);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

async function readPendingStatus(
  service: WorkerNameUpdateService,
  attempt: PoolAssignmentAttempt,
  signal: AbortSignal,
) {
  const check = new AbortController();
  const onAbort = () => check.abort(signal.reason);
  if (signal.aborted) onAbort();
  else signal.addEventListener("abort", onAbort, { once: true });
  try {
    return await withDeadline(
      async () => {
        let failures = 0;
        let delay = 0;
        while (true) {
          if (delay) await waitForStatus(delay, check.signal);
          try {
            const result = await service.status(attempt, { signal: check.signal });
            check.signal.throwIfAborted();
            if (result.state !== "pending") return result;
            failures = 0;
            delay = STATUS_POLL_INTERVAL_MS;
          } catch (error) {
            check.signal.throwIfAborted();
            if (isAuthenticationError(error) || ++failures >= MAX_STATUS_FAILURES) throw error;
            delay = Math.max(delay, STATUS_POLL_INTERVAL_MS) * 2;
          }
        }
      },
      check,
      PENDING_STATUS_DEADLINE_MS,
    );
  } catch (error) {
    if (check.signal.aborted && !signal.aborted)
      throw Object.assign(new Error("The controller is still updating worker names."), { cause: error });
    throw error;
  } finally {
    signal.removeEventListener("abort", onAbort);
  }
}

async function runWorkerNameUpdate(
  service: WorkerNameUpdateService,
  controllerId: string,
  review: WorkerNameReview,
  signal: AbortSignal,
): Promise<UpdateResult> {
  const count = review.groups.reduce((total, { assignment }) => total + assignment.boards.length, 0);
  const toastId = pushToast({
    message: count === 1 ? "Updating worker name" : "Updating worker names",
    status: STATUSES.loading,
    longRunning: true,
  });
  const progress: { confirmed: number; attempt?: PoolAssignmentAttempt } = { confirmed: 0 };
  const saved = (attempt: PoolAssignmentAttempt) => {
    removePoolAttempt(controllerId, attempt.id);
    progress.confirmed += attempt.macs.length;
    progress.attempt = undefined;
  };
  const success = (message: string): UpdateResult => {
    updateToast(toastId, { message, status: STATUSES.success });
    return { state: "saved", message };
  };
  try {
    let result = await withPoolWriteLock(controllerId, signal, async (writeSignal) => {
      if (readPoolAttempts(controllerId).length)
        throw new Error("Another pool assignment is tracked in this browser. Check its status first.");
      const outcome = await service.applyWorkerNames(
        review,
        {
          track: (record) => {
            writeSignal.throwIfAborted();
            savePoolAttempt(controllerId, record);
            progress.attempt = record;
          },
          saved: (record) => {
            writeSignal.throwIfAborted();
            saved(record);
          },
        },
        { signal: writeSignal },
      );
      writeSignal.throwIfAborted();
      return outcome;
    });
    signal.throwIfAborted();
    if (result.state === "saved") return success(result.message);
    const attempt = progress.attempt;
    if (result.state === "pending" && attempt) {
      updateToast(toastId, { message: result.message });
      result = await readPendingStatus(service, attempt, signal);
      signal.throwIfAborted();
      if (result.state === "saved") {
        saved(attempt);
        if (!attempt.unsubmittedWorkerNameBoards) return success("Worker names saved.");
        throw new Error("Review the remaining hashboards again.");
      }
    }
    throw new Error(result.message);
  } catch (error) {
    if (error instanceof WorkerNameUpdateRolledBack && progress.attempt?.id === error.attemptId) {
      removePoolAttempt(controllerId, error.attemptId);
      progress.attempt = undefined;
    }
    if (signal.aborted) {
      removeToast(toastId);
      return { state: "interrupted" };
    }
    const reason = poolAssignmentErrorMessage(error);
    const outcome = progress.confirmed
      ? `Worker names were saved for ${progress.confirmed} of the selected hashboards. ${reason}`
      : reason;
    const message = progress.attempt ? `${outcome} Check Pool assignment status in Settings → Pools.` : outcome;
    updateToast(toastId, { message, status: STATUSES.error });
    return { state: "failed", message, dismiss: Boolean(progress.attempt || progress.confirmed) };
  }
}

export function startWorkerNameUpdate(
  service: WorkerNameUpdateService,
  controllerId: string,
  owner: string,
  review: WorkerNameReview,
) {
  if (hasWorkerNameUpdate(controllerId)) return undefined;
  const update = { owner, service, controller: new AbortController() };
  useActiveUpdates.setState((updates) => new Map(updates).set(controllerId, update), true);
  return runWorkerNameUpdate(service, controllerId, review, update.controller.signal).finally(() => {
    useActiveUpdates.setState((updates) => {
      const next = new Map(updates);
      next.delete(controllerId);
      return next;
    }, true);
  });
}
