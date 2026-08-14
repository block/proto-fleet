import { useCallback, useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { type UpgradeOperation, UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const ACTIVE_POLL_INTERVAL_MS = 2_000;
const IDLE_POLL_INTERVAL_MS = 60_000;
const STATUS_REQUEST_TIMEOUT_MS = 10_000;
const ACKNOWLEDGE_REQUEST_TIMEOUT_MS = 25_000;
const TRIGGER_REQUEST_TIMEOUT_MS = 30_000;
const TRIGGER_RECONCILIATION_TIMEOUT_MS = 15_000;

const createOperationID = () => {
  // getRandomValues is available in the plain-HTTP deployments Fleet supports,
  // while crypto.randomUUID() is restricted to secure contexts.
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
};

const DEFINITIVE_TRIGGER_REJECTION_CODES = new Set<Code>([
  Code.AlreadyExists,
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.Unimplemented,
]);

const isDefinitiveTriggerRejection = (error: unknown) =>
  error instanceof ConnectError && DEFINITIVE_TRIGGER_REJECTION_CODES.has(error.code);

interface PendingSubmission {
  id: string;
  targetVersion: string;
}

interface UseUpgradeOperationOptions {
  authSessionIdentity: string;
  enabled: boolean;
  onUntrackedSuccess?: (operation: UpgradeOperation) => void;
  onPollError?: (error: unknown) => void;
}

interface UseUpgradeOperationResult {
  acknowledgeOperation: () => Promise<void>;
  connectionLost: boolean;
  manualFallbackReady: boolean;
  operation: UpgradeOperation | undefined;
  operationStatusPending: boolean;
  reconciling: boolean;
  reloadFleet: () => void;
  triggerError: string | null;
  triggering: boolean;
  trackedTargetVersion: string | undefined;
  triggerUpgrade: (targetVersion: string) => Promise<void>;
  useManualFallback: () => void;
}

export const isUpgradeTerminal = (phase: UpgradePhase) =>
  phase === UpgradePhase.SUCCEEDED || phase === UpgradePhase.FAILED;

export const isUpgradeActive = (operation?: UpgradeOperation) =>
  Boolean(operation && !isUpgradeTerminal(operation.phase));

export function useUpgradeOperation({
  authSessionIdentity,
  enabled,
  onUntrackedSuccess,
  onPollError,
}: UseUpgradeOperationOptions): UseUpgradeOperationResult {
  const [operation, setOperation] = useState<UpgradeOperation>();
  const [pendingSubmission, setPendingSubmission] = useState<PendingSubmission>();
  const [triggering, setTriggering] = useState(false);
  const [reconciling, setReconciling] = useState(false);
  const [connectionLost, setConnectionLost] = useState(false);
  const [manualFallbackReady, setManualFallbackReady] = useState(false);
  const [triggerError, setTriggerError] = useState<string | null>(null);
  const [pollRevision, setPollRevision] = useState(0);
  const [resolvedStatusSessionIdentity, setResolvedStatusSessionIdentity] = useState<string | null>(null);

  const operationRef = useRef(operation);
  const pendingSubmissionRef = useRef(pendingSubmission);
  const reconcilingRef = useRef(reconciling);
  const reconciliationDeadlineRef = useRef<number | null>(null);
  const statusRequestEpochRef = useRef(0);
  const authSessionIdentityRef = useRef(authSessionIdentity);
  const resolvedStatusSessionIdentityRef = useRef<string | null>(null);
  const onPollErrorRef = useRef(onPollError);
  const onUntrackedSuccessRef = useRef(onUntrackedSuccess);
  const submittedOperationIDsRef = useRef(new Set<string>());
  const refreshedUntrackedSuccessesRef = useRef(new Set<string>());

  authSessionIdentityRef.current = authSessionIdentity;
  onPollErrorRef.current = onPollError;
  onUntrackedSuccessRef.current = onUntrackedSuccess;

  const operationStatusPending = enabled && resolvedStatusSessionIdentity !== authSessionIdentity;

  const updateOperation = useCallback((next: UpgradeOperation | undefined) => {
    operationRef.current = next;
    setOperation(next);
  }, []);

  const updatePendingSubmission = useCallback((next: PendingSubmission | undefined) => {
    pendingSubmissionRef.current = next;
    setPendingSubmission(next);
  }, []);

  const updateReconciling = useCallback((next: boolean) => {
    reconcilingRef.current = next;
    setReconciling(next);
  }, []);

  const resetReconciliation = useCallback(() => {
    reconciliationDeadlineRef.current = null;
    setManualFallbackReady(false);
    updateReconciling(false);
    setConnectionLost(false);
  }, [updateReconciling]);

  const beginReconciliation = useCallback(
    (lostConnection: boolean) => {
      if (!reconcilingRef.current) {
        reconciliationDeadlineRef.current = Date.now() + TRIGGER_RECONCILIATION_TIMEOUT_MS;
        setManualFallbackReady(false);
        updateReconciling(true);
      }
      setConnectionLost(lostConnection);
    },
    [updateReconciling],
  );

  const reconciliationExpired = useCallback(() => {
    const deadline = reconciliationDeadlineRef.current;
    return deadline !== null && Date.now() >= deadline;
  }, []);

  const notifyUntrackedSuccess = useCallback((next: UpgradeOperation) => {
    if (next.phase !== UpgradePhase.SUCCEEDED || submittedOperationIDsRef.current.has(next.id)) {
      return;
    }
    const revision = `${next.id}:${next.outcomeRevision}`;
    if (refreshedUntrackedSuccessesRef.current.has(revision)) {
      return;
    }
    refreshedUntrackedSuccessesRef.current.add(revision);
    onUntrackedSuccessRef.current?.(next);
  }, []);

  const acceptServerOperation = useCallback(
    (next: UpgradeOperation) => {
      if (!next.id) {
        return;
      }

      const pendingMatches = pendingSubmissionRef.current?.id === next.id;
      if (next.acknowledged && isUpgradeTerminal(next.phase)) {
        // The host is the sole dismissal authority. Acknowledged outcomes are
        // the only terminal operations hidden by the browser.
        updateOperation(undefined);
        if (pendingMatches) {
          updatePendingSubmission(undefined);
          setTriggerError(null);
          resetReconciliation();
        } else if (!pendingSubmissionRef.current) {
          setTriggerError(null);
          resetReconciliation();
        } else {
          setConnectionLost(false);
        }
        return;
      }

      notifyUntrackedSuccess(next);
      updateOperation(next);
      setConnectionLost(false);

      if (pendingMatches) {
        updatePendingSubmission(undefined);
        setTriggerError(null);
        resetReconciliation();
      } else if (!pendingSubmissionRef.current) {
        setTriggerError(null);
        resetReconciliation();
      }
    },
    [notifyUntrackedSuccess, resetReconciliation, updateOperation, updatePendingSubmission],
  );

  const reconcilePendingSubmission = useCallback(
    (executorAvailable: boolean) => {
      if (!pendingSubmissionRef.current) {
        return;
      }
      if (!executorAvailable) {
        beginReconciliation(true);
        if (reconciliationExpired() && !isUpgradeActive(operationRef.current)) {
          setManualFallbackReady(true);
        }
        return;
      }
      beginReconciliation(false);
      if (reconciliationExpired()) {
        updatePendingSubmission(undefined);
        if (operationRef.current) {
          setTriggerError(null);
        }
        resetReconciliation();
      }
    },
    [beginReconciliation, reconciliationExpired, resetReconciliation, updatePendingSubmission],
  );

  const reconcileMissingActiveOperation = useCallback(() => {
    if (!isUpgradeActive(operationRef.current)) {
      return false;
    }
    // Never infer that an active operation ended from a missing/unreachable
    // status response. Keep the exact last host operation locked until the
    // host reports a terminal state.
    beginReconciliation(true);
    return true;
  }, [beginReconciliation]);

  const pollStatus = useCallback(
    async (signal: AbortSignal, pollingAuthSessionIdentity: string, requestEpoch: number) => {
      try {
        const response = await instanceUpdateClient.getUpgradeStatus(
          {},
          { signal, timeoutMs: STATUS_REQUEST_TIMEOUT_MS },
        );
        if (
          signal.aborted ||
          authSessionIdentityRef.current !== pollingAuthSessionIdentity ||
          statusRequestEpochRef.current !== requestEpoch
        ) {
          return;
        }

        resolvedStatusSessionIdentityRef.current = pollingAuthSessionIdentity;
        setResolvedStatusSessionIdentity(pollingAuthSessionIdentity);

        if (response.operation) {
          acceptServerOperation(response.operation);
          if (pendingSubmissionRef.current) {
            reconcilePendingSubmission(response.executorAvailable);
          }
          return;
        }

        if (!response.executorAvailable) {
          if (reconcileMissingActiveOperation()) {
            return;
          }
          reconcilePendingSubmission(false);
          if (!pendingSubmissionRef.current) {
            setConnectionLost(false);
          }
          return;
        }

        if (reconcileMissingActiveOperation()) {
          return;
        }

        // A reachable host with no operation is authoritative for terminal
        // visibility, but an ambiguous trigger still gets a bounded window in
        // which its exact caller-generated ID can appear.
        if (operationRef.current && isUpgradeTerminal(operationRef.current.phase)) {
          updateOperation(undefined);
        }
        setConnectionLost(false);
        reconcilePendingSubmission(true);
        if (!pendingSubmissionRef.current) {
          resetReconciliation();
        }
      } catch (error) {
        if (
          signal.aborted ||
          authSessionIdentityRef.current !== pollingAuthSessionIdentity ||
          statusRequestEpochRef.current !== requestEpoch
        ) {
          return;
        }
        onPollErrorRef.current?.(error);
        if (reconcileMissingActiveOperation()) {
          return;
        }
        reconcilePendingSubmission(false);
      }
    },
    [
      acceptServerOperation,
      reconcileMissingActiveOperation,
      reconcilePendingSubmission,
      resetReconciliation,
      updateOperation,
    ],
  );

  useEffect(() => {
    if (!enabled) return;

    let alive = true;
    let timer: number | undefined;
    let controller: AbortController | undefined;

    const run = async () => {
      controller = new AbortController();
      await pollStatus(controller.signal, authSessionIdentity, statusRequestEpochRef.current);
      if (alive) {
        const awaitingInitialStatus = resolvedStatusSessionIdentityRef.current !== authSessionIdentity;
        const pollIntervalMs =
          isUpgradeActive(operationRef.current) ||
          reconcilingRef.current ||
          pendingSubmissionRef.current ||
          awaitingInitialStatus
            ? ACTIVE_POLL_INTERVAL_MS
            : IDLE_POLL_INTERVAL_MS;
        timer = window.setTimeout(run, pollIntervalMs);
      }
    };

    void run();
    return () => {
      alive = false;
      controller?.abort();
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [authSessionIdentity, enabled, pollRevision, pollStatus]);

  const triggerUpgrade = useCallback(
    async (targetVersion: string) => {
      // Ignore every status request that began before this mutation. Its
      // snapshot can only describe the pre-trigger host state.
      statusRequestEpochRef.current += 1;
      const operationId = createOperationID();
      submittedOperationIDsRef.current.add(operationId);
      updatePendingSubmission({ id: operationId, targetVersion });
      reconciliationDeadlineRef.current = null;
      setManualFallbackReady(false);
      updateReconciling(false);
      setConnectionLost(false);
      setTriggerError(null);
      setTriggering(true);
      try {
        const response = await instanceUpdateClient.triggerUpgrade(
          { operationId, targetVersion },
          { timeoutMs: TRIGGER_REQUEST_TIMEOUT_MS },
        );
        if (!response.operation || response.operation.id !== operationId) {
          throw new Error("Host updater did not return the requested operation");
        }
        acceptServerOperation(response.operation);
      } catch (error) {
        setTriggerError(getErrorMessage(error, "Failed to start upgrade"));
        if (isDefinitiveTriggerRejection(error)) {
          updatePendingSubmission(undefined);
          resetReconciliation();
        } else {
          beginReconciliation(false);
        }
      } finally {
        // Also invalidate a poll that may have started while the mutation was
        // in flight, then wake a fresh authoritative poll.
        statusRequestEpochRef.current += 1;
        setTriggering(false);
        // Wake an idle poll so an ambiguous response is reconciled by the
        // exact operation ID without relying on target/version heuristics.
        setPollRevision((revision) => revision + 1);
      }
    },
    [acceptServerOperation, beginReconciliation, resetReconciliation, updatePendingSubmission, updateReconciling],
  );

  const acknowledgeOperation = useCallback(async () => {
    const terminalOperation = operationRef.current;
    if (!terminalOperation?.id || !isUpgradeTerminal(terminalOperation.phase)) {
      return;
    }

    statusRequestEpochRef.current += 1;
    const expectedOutcomeRevision = terminalOperation.outcomeRevision;
    try {
      const response = await instanceUpdateClient.acknowledgeUpgrade(
        { operationId: terminalOperation.id, expectedOutcomeRevision },
        { timeoutMs: ACKNOWLEDGE_REQUEST_TIMEOUT_MS },
      );
      const acknowledged = response.operation;
      if (
        !acknowledged ||
        acknowledged.id !== terminalOperation.id ||
        acknowledged.outcomeRevision !== expectedOutcomeRevision ||
        !acknowledged.acknowledged ||
        !isUpgradeTerminal(acknowledged.phase)
      ) {
        throw new Error("Host did not acknowledge the current upgrade outcome");
      }

      const current = operationRef.current;
      if (current?.id === terminalOperation.id && current.outcomeRevision === expectedOutcomeRevision) {
        updateOperation(undefined);
      }
    } finally {
      statusRequestEpochRef.current += 1;
      setPollRevision((revision) => revision + 1);
    }
  }, [updateOperation]);

  const useManualFallback = useCallback(() => {
    // Manual fallback is available only when submission itself is ambiguous
    // and no active host operation is known. Known active operations remain
    // locked until the host reports their outcome.
    if (!manualFallbackReady || isUpgradeActive(operationRef.current)) {
      return;
    }
    updatePendingSubmission(undefined);
    setTriggerError(null);
    resetReconciliation();
  }, [manualFallbackReady, resetReconciliation, updatePendingSubmission]);

  const reloadFleet = useCallback(() => {
    window.location.reload();
  }, []);

  return {
    acknowledgeOperation,
    connectionLost,
    manualFallbackReady,
    operation,
    operationStatusPending,
    reconciling,
    reloadFleet,
    triggerError,
    triggering,
    trackedTargetVersion: pendingSubmission?.targetVersion,
    triggerUpgrade,
    useManualFallback,
  };
}
