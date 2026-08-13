import { useCallback, useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { type UpgradeOperation, UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const ACTIVE_POLL_INTERVAL_MS = 2_000;
const IDLE_POLL_INTERVAL_MS = 60_000;
const STATUS_REQUEST_TIMEOUT_MS = 10_000;
const ACKNOWLEDGE_REQUEST_TIMEOUT_MS = 10_000;
const TRIGGER_REQUEST_TIMEOUT_MS = 30_000;
const TRIGGER_RECONCILIATION_TIMEOUT_MS = 15_000;
const TRACKED_OPERATION_KEY = "protoFleet:tracked-upgrade-operation";
const ACKNOWLEDGED_OPERATION_KEY = "protoFleet:acknowledged-upgrade-operation";
const CANONICAL_RELEASE_PATTERN = /^v(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$/;
const DEFINITIVE_TRIGGER_REJECTION_CODES = new Set<Code>([
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.Unimplemented,
]);

const isDefinitiveTriggerRejection = (error: unknown) =>
  error instanceof ConnectError && DEFINITIVE_TRIGGER_REJECTION_CODES.has(error.code);

interface CanonicalRelease {
  core: [number, number, number];
  rc?: number;
}

const parseCanonicalRelease = (version?: string): CanonicalRelease | undefined => {
  const match = version?.match(CANONICAL_RELEASE_PATTERN);
  if (!match) return undefined;
  const parts = match.slice(1).map((part) => (part === undefined ? undefined : Number(part)));
  if (parts.some((part) => part !== undefined && !Number.isSafeInteger(part))) return undefined;
  return {
    core: [parts[0]!, parts[1]!, parts[2]!],
    ...(parts[3] === undefined ? {} : { rc: parts[3] }),
  };
};

const isReleaseNewer = (currentVersion: string | undefined, targetVersion: string) => {
  const current = parseCanonicalRelease(currentVersion);
  const target = parseCanonicalRelease(targetVersion);
  if (!current || !target) return false;
  for (let index = 0; index < current.core.length; index += 1) {
    if (current.core[index] !== target.core[index]) {
      return current.core[index] > target.core[index];
    }
  }
  if (current.rc === undefined) return target.rc !== undefined;
  if (target.rc === undefined) return false;
  return current.rc > target.rc;
};

interface TrackedOperation {
  id?: string;
  targetVersion: string;
}

interface AcknowledgedOperation {
  authSessionIdentity: string;
  id: string;
  phase: UpgradePhase;
  revision: string;
}

type AcknowledgedOperationInput = Pick<AcknowledgedOperation, "id" | "phase" | "revision">;

const ACKNOWLEDGEABLE_PHASES = new Set<UpgradePhase>([
  UpgradePhase.UNSPECIFIED,
  UpgradePhase.SUCCEEDED,
  UpgradePhase.FAILED,
]);

interface UseUpgradeOperationOptions {
  authSessionIdentity: string;
  currentVersion?: string;
  currentVersionUnavailable: boolean;
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

const operationRevision = (operation: UpgradeOperation) => {
  const updatedAt = operation.updatedAt;
  if (updatedAt) {
    return `${updatedAt.seconds}:${updatedAt.nanos}`;
  }
  return JSON.stringify([operation.message, operation.error, operation.recoveryCommand, operation.hostLogPath]);
};

const readTrackedOperation = (): TrackedOperation | undefined => {
  try {
    const raw = window.sessionStorage.getItem(TRACKED_OPERATION_KEY);
    if (!raw) return undefined;
    const value = JSON.parse(raw) as Partial<TrackedOperation>;
    if (typeof value.targetVersion !== "string" || !value.targetVersion) {
      window.sessionStorage.removeItem(TRACKED_OPERATION_KEY);
      return undefined;
    }
    return {
      targetVersion: value.targetVersion,
      ...(typeof value.id === "string" && value.id ? { id: value.id } : {}),
    };
  } catch {
    try {
      window.sessionStorage.removeItem(TRACKED_OPERATION_KEY);
    } catch {
      // Storage is best-effort; the host remains authoritative.
    }
    return undefined;
  }
};

const readAcknowledgedOperation = (authSessionIdentity: string): AcknowledgedOperationInput | null => {
  try {
    const raw = window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<AcknowledgedOperation>;
    if (
      typeof value.id === "string" &&
      value.id &&
      value.authSessionIdentity === authSessionIdentity &&
      typeof value.phase === "number" &&
      ACKNOWLEDGEABLE_PHASES.has(value.phase) &&
      typeof value.revision === "string"
    ) {
      return { id: value.id, phase: value.phase, revision: value.revision };
    }
    return null;
  } catch {
    try {
      window.sessionStorage.removeItem(ACKNOWLEDGED_OPERATION_KEY);
    } catch {
      // Storage is best-effort; the host remains authoritative.
    }
    return null;
  }
};

export function useUpgradeOperation({
  authSessionIdentity,
  currentVersion,
  currentVersionUnavailable,
  enabled,
  onUntrackedSuccess,
  onPollError,
}: UseUpgradeOperationOptions): UseUpgradeOperationResult {
  const [operation, setOperation] = useState<UpgradeOperation>();
  const [triggering, setTriggering] = useState(false);
  const [trackedOperation, setTrackedOperation] = useState<TrackedOperation | undefined>(readTrackedOperation);
  const recoveredTrackedOperation = Boolean(trackedOperation);
  const recoveredUnknownOutcome = Boolean(trackedOperation && !trackedOperation.id);
  const [reconciling, setReconciling] = useState(recoveredTrackedOperation);
  const [connectionLost, setConnectionLost] = useState(false);
  const [manualFallbackReady, setManualFallbackReady] = useState(false);
  const [triggerError, setTriggerError] = useState<string | null>(
    recoveredUnknownOutcome ? "Fleet did not confirm whether the previous upgrade request started" : null,
  );
  const [pollRevision, setPollRevision] = useState(0);
  const [resolvedStatusSessionIdentity, setResolvedStatusSessionIdentity] = useState<string | null>(null);

  const operationRef = useRef(operation);
  const trackedOperationRef = useRef(trackedOperation);
  const acknowledgedOperationRef = useRef<AcknowledgedOperationInput | null>(null);
  const acknowledgedOperationSessionRef = useRef<string | null>(null);
  const reconcilingRef = useRef(reconciling);
  const currentVersionRef = useRef(currentVersion);
  const currentVersionUnavailableRef = useRef(currentVersionUnavailable);
  const onPollErrorRef = useRef(onPollError);
  const onUntrackedSuccessRef = useRef(onUntrackedSuccess);
  const reconciliationDeadlineRef = useRef<number | null>(null);
  const lastObservedOperationIDRef = useRef<string | null | undefined>(undefined);
  const triggerBaselineOperationIDRef = useRef<string | null | undefined>(undefined);
  const refreshedUntrackedSuccessRef = useRef<string | null>(null);
  const authSessionIdentityRef = useRef(authSessionIdentity);
  const resolvedStatusSessionIdentityRef = useRef<string | null>(null);

  currentVersionRef.current = currentVersion;
  currentVersionUnavailableRef.current = currentVersionUnavailable;
  onPollErrorRef.current = onPollError;
  onUntrackedSuccessRef.current = onUntrackedSuccess;
  authSessionIdentityRef.current = authSessionIdentity;
  if (acknowledgedOperationSessionRef.current !== authSessionIdentity) {
    acknowledgedOperationSessionRef.current = authSessionIdentity;
    acknowledgedOperationRef.current = readAcknowledgedOperation(authSessionIdentity);
  }

  const operationStatusPending = enabled && resolvedStatusSessionIdentity !== authSessionIdentity;

  useEffect(() => {
    if (trackedOperationRef.current) {
      reconciliationDeadlineRef.current = Date.now() + TRIGGER_RECONCILIATION_TIMEOUT_MS;
    }
  }, []);

  const updateOperation = useCallback((next: UpgradeOperation | undefined) => {
    operationRef.current = next;
    setOperation(next);
  }, []);

  const updateReconciling = useCallback((next: boolean) => {
    reconcilingRef.current = next;
    setReconciling(next);
  }, []);

  const updateTrackedOperation = useCallback((next: TrackedOperation | undefined) => {
    trackedOperationRef.current = next;
    setTrackedOperation(next);
    try {
      if (next) {
        window.sessionStorage.setItem(TRACKED_OPERATION_KEY, JSON.stringify(next));
      } else {
        window.sessionStorage.removeItem(TRACKED_OPERATION_KEY);
      }
    } catch {
      // Browser storage is an optimization for route/reload recovery. The
      // host operation remains durable when storage is unavailable.
    }
  }, []);

  const updateAcknowledgedOperation = useCallback((next: AcknowledgedOperationInput | null) => {
    acknowledgedOperationRef.current = next;
    try {
      if (next) {
        window.sessionStorage.setItem(
          ACKNOWLEDGED_OPERATION_KEY,
          JSON.stringify({ authSessionIdentity: authSessionIdentityRef.current, ...next }),
        );
      } else {
        window.sessionStorage.removeItem(ACKNOWLEDGED_OPERATION_KEY);
      }
    } catch {
      // See updateTrackedOperation: storage failure cannot affect host state.
    }
  }, []);

  const finishExpiredReconciliation = useCallback(() => {
    const deadline = reconciliationDeadlineRef.current;
    if (!reconcilingRef.current || deadline === null || Date.now() < deadline) {
      return false;
    }
    reconciliationDeadlineRef.current = null;
    setManualFallbackReady(false);
    updateReconciling(false);
    if (!trackedOperationRef.current?.id) {
      updateTrackedOperation(undefined);
    }
    return true;
  }, [updateReconciling, updateTrackedOperation]);

  const allowManualFallbackAfterTimeout = useCallback(() => {
    const deadline = reconciliationDeadlineRef.current;
    if (reconcilingRef.current && deadline !== null && Date.now() >= deadline) {
      setManualFallbackReady(true);
    }
  }, []);

  const reconcileMissingActiveOperation = useCallback(() => {
    if (!isUpgradeActive(operationRef.current)) {
      return false;
    }
    if (!reconcilingRef.current) {
      reconciliationDeadlineRef.current = Date.now() + TRIGGER_RECONCILIATION_TIMEOUT_MS;
      setManualFallbackReady(false);
      updateReconciling(true);
    }
    setConnectionLost(true);
    allowManualFallbackAfterTimeout();
    return true;
  }, [allowManualFallbackAfterTimeout, updateReconciling]);

  const resolveRecoveredOperationMiss = useCallback(() => {
    if (!reconcilingRef.current || !trackedOperationRef.current?.id) {
      return false;
    }
    reconciliationDeadlineRef.current = null;
    setManualFallbackReady(false);
    updateTrackedOperation(undefined);
    updateReconciling(false);
    setConnectionLost(false);
    setTriggerError(null);
    return true;
  }, [updateReconciling, updateTrackedOperation]);

  const clearSurfacedOperation = useCallback(() => {
    updateTrackedOperation(undefined);
    updateOperation(undefined);
    reconciliationDeadlineRef.current = null;
    setManualFallbackReady(false);
    updateReconciling(false);
    setConnectionLost(false);
    setTriggerError(null);
  }, [updateOperation, updateReconciling, updateTrackedOperation]);

  const acceptServerOperation = useCallback(
    (next: UpgradeOperation, fromTriggerResponse = false) => {
      if (!next.id) {
        return false;
      }
      if (next.acknowledged && isUpgradeTerminal(next.phase)) {
        // The host durably recorded a dismissal (possibly from another
        // session), so the outcome must not resurface anywhere.
        if (operationRef.current?.id === next.id) {
          clearSurfacedOperation();
        } else if (trackedOperationRef.current?.id === next.id) {
          updateTrackedOperation(undefined);
        }
        return false;
      }
      const acknowledged = acknowledgedOperationRef.current;
      const revision = operationRevision(next);
      const acknowledgedExactRevision =
        acknowledged?.id === next.id && acknowledged.phase === next.phase && acknowledged.revision === revision;
      const acknowledgedTransition = acknowledged?.id === next.id && !acknowledgedExactRevision;
      if (acknowledgedExactRevision) {
        return false;
      }
      const active = isUpgradeActive(next);
      const tracked = trackedOperationRef.current;
      const reconciledTerminal =
        reconcilingRef.current &&
        !tracked?.id &&
        tracked?.targetVersion === next.targetVersion &&
        isUpgradeTerminal(next.phase) &&
        triggerBaselineOperationIDRef.current !== undefined &&
        triggerBaselineOperationIDRef.current !== next.id;
      const trackedMatches = tracked?.id
        ? tracked.id === next.id
        : (fromTriggerResponse && tracked?.targetVersion === next.targetVersion) ||
          reconciledTerminal ||
          acknowledgedTransition;

      if (active) {
        // A durable active operation is authoritative, including one started
        // by another operator. It takes precedence over a newer release offer.
        updateAcknowledgedOperation(null);
        updateTrackedOperation({ id: next.id, targetVersion: next.targetVersion });
        updateOperation(next);
        if (next.phase === UpgradePhase.UNSPECIFIED) {
          if (!reconcilingRef.current) {
            reconciliationDeadlineRef.current = Date.now() + TRIGGER_RECONCILIATION_TIMEOUT_MS;
            setManualFallbackReady(false);
            updateReconciling(true);
          }
          setConnectionLost(false);
          setTriggerError(null);
          allowManualFallbackAfterTimeout();
          return true;
        }
        reconciliationDeadlineRef.current = null;
        setManualFallbackReady(false);
        updateReconciling(false);
        setConnectionLost(false);
        setTriggerError(null);
        return true;
      }

      if (!isUpgradeTerminal(next.phase)) {
        return false;
      }

      if (tracked && !trackedMatches) {
        return false;
      }

      if (next.phase === UpgradePhase.SUCCEEDED && !trackedMatches) {
        const successRevision = `${next.id}:${revision}`;
        if (refreshedUntrackedSuccessRef.current !== successRevision) {
          refreshedUntrackedSuccessRef.current = successRevision;
          onUntrackedSuccessRef.current?.(next);
        }
        return false;
      }

      // A strictly newer release proves a later recovery. An exact target
      // match does not: activation can expose the target version before a
      // later service failure records the operation as failed.
      if (next.phase === UpgradePhase.FAILED && isReleaseNewer(currentVersionRef.current, next.targetVersion)) {
        if (acknowledgedTransition) updateAcknowledgedOperation(null);
        if (trackedMatches) updateTrackedOperation(undefined);
        if (operationRef.current?.id === next.id) updateOperation(undefined);
        reconciliationDeadlineRef.current = null;
        setManualFallbackReady(false);
        updateReconciling(false);
        setConnectionLost(false);
        return false;
      }

      const unresolvedFailure =
        next.phase === UpgradePhase.FAILED &&
        (currentVersionUnavailableRef.current || Boolean(currentVersionRef.current));
      if (!trackedMatches && !unresolvedFailure) {
        return false;
      }

      if (acknowledgedTransition) updateAcknowledgedOperation(null);
      updateTrackedOperation({ id: next.id, targetVersion: next.targetVersion });
      updateOperation(next);
      reconciliationDeadlineRef.current = null;
      setManualFallbackReady(false);
      updateReconciling(false);
      setConnectionLost(false);
      setTriggerError(null);
      return true;
    },
    [
      allowManualFallbackAfterTimeout,
      clearSurfacedOperation,
      updateAcknowledgedOperation,
      updateOperation,
      updateReconciling,
      updateTrackedOperation,
    ],
  );

  const pollStatus = useCallback(
    async (signal: AbortSignal, pollingAuthSessionIdentity: string) => {
      try {
        const response = await instanceUpdateClient.getUpgradeStatus(
          {},
          { signal, timeoutMs: STATUS_REQUEST_TIMEOUT_MS },
        );
        if (signal.aborted || authSessionIdentityRef.current !== pollingAuthSessionIdentity) {
          return;
        }

        resolvedStatusSessionIdentityRef.current = pollingAuthSessionIdentity;
        setResolvedStatusSessionIdentity(pollingAuthSessionIdentity);
        lastObservedOperationIDRef.current = response.operation?.id || null;

        if (response.operation && acceptServerOperation(response.operation)) {
          return;
        }

        if (!response.executorAvailable) {
          if (reconcileMissingActiveOperation()) {
            return;
          }
          if (isUpgradeActive(operationRef.current) || reconcilingRef.current || trackedOperationRef.current) {
            setConnectionLost(true);
          }
          allowManualFallbackAfterTimeout();
          return;
        }

        if (reconcileMissingActiveOperation()) {
          return;
        }
        setConnectionLost(false);
        if (resolveRecoveredOperationMiss()) {
          return;
        }
        finishExpiredReconciliation();
      } catch (error) {
        if (signal.aborted || authSessionIdentityRef.current !== pollingAuthSessionIdentity) return;
        onPollErrorRef.current?.(error);
        if (reconcileMissingActiveOperation()) {
          return;
        }
        if (isUpgradeActive(operationRef.current) || reconcilingRef.current || trackedOperationRef.current) {
          setConnectionLost(true);
        }
        allowManualFallbackAfterTimeout();
      }
    },
    [
      acceptServerOperation,
      allowManualFallbackAfterTimeout,
      finishExpiredReconciliation,
      reconcileMissingActiveOperation,
      resolveRecoveredOperationMiss,
    ],
  );

  useEffect(() => {
    if (!enabled) return;

    let alive = true;
    let timer: number | undefined;
    let controller: AbortController | undefined;

    const run = async () => {
      controller = new AbortController();
      await pollStatus(controller.signal, authSessionIdentity);
      if (alive) {
        const awaitingTrackedOperation = Boolean(trackedOperationRef.current && !operationRef.current);
        const awaitingInitialStatus = resolvedStatusSessionIdentityRef.current !== authSessionIdentity;
        const pollIntervalMs =
          isUpgradeActive(operationRef.current) ||
          reconcilingRef.current ||
          awaitingTrackedOperation ||
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
  }, [authSessionIdentity, currentVersion, currentVersionUnavailable, enabled, pollRevision, pollStatus]);

  const triggerUpgrade = useCallback(
    async (targetVersion: string) => {
      triggerBaselineOperationIDRef.current = lastObservedOperationIDRef.current;
      updateTrackedOperation({ targetVersion });
      reconciliationDeadlineRef.current = null;
      setManualFallbackReady(false);
      updateReconciling(false);
      setConnectionLost(false);
      setTriggerError(null);
      setTriggering(true);
      try {
        const response = await instanceUpdateClient.triggerUpgrade(
          { targetVersion },
          { timeoutMs: TRIGGER_REQUEST_TIMEOUT_MS },
        );
        if (!response.operation) {
          throw new Error("Host updater did not return an operation");
        }
        if (!acceptServerOperation(response.operation, true)) {
          throw new Error(
            "Fleet couldn't confirm the upgrade state. Fleet will check the host before unlocking other install options.",
          );
        }
      } catch (error) {
        setTriggerError(getErrorMessage(error, "Failed to start upgrade"));
        if (isDefinitiveTriggerRejection(error)) {
          updateTrackedOperation(undefined);
          reconciliationDeadlineRef.current = null;
          setManualFallbackReady(false);
          updateReconciling(false);
          setConnectionLost(false);
        } else {
          reconciliationDeadlineRef.current = Date.now() + TRIGGER_RECONCILIATION_TIMEOUT_MS;
          updateReconciling(true);
        }
      } finally {
        setTriggering(false);
        // An idle status poll may already be scheduled far in the future.
        // Wake it after trigger admission settles so success and ambiguous
        // responses both transition immediately to the active cadence.
        setPollRevision((revision) => revision + 1);
      }
    },
    [acceptServerOperation, updateReconciling, updateTrackedOperation],
  );

  const acknowledgeOperation = useCallback(async () => {
    const currentOperation = operationRef.current;
    const terminalOperation =
      currentOperation && isUpgradeTerminal(currentOperation.phase) ? currentOperation : undefined;
    if (terminalOperation) {
      // Local suppression applies immediately and keeps this tab clean even
      // when the durable host acknowledgement below cannot be recorded.
      updateAcknowledgedOperation({
        id: terminalOperation.id,
        phase: terminalOperation.phase,
        revision: operationRevision(terminalOperation),
      });
    }
    clearSurfacedOperation();
    if (!terminalOperation?.id) {
      return;
    }
    try {
      await instanceUpdateClient.acknowledgeUpgrade(
        { operationId: terminalOperation.id },
        { timeoutMs: ACKNOWLEDGE_REQUEST_TIMEOUT_MS },
      );
    } catch (error) {
      if (error instanceof ConnectError && (error.code === Code.NotFound || error.code === Code.FailedPrecondition)) {
        // The host no longer reports this operation (or cannot persist
        // acknowledgements); there is nothing left to dismiss durably.
        return;
      }
      throw error;
    }
  }, [clearSurfacedOperation, updateAcknowledgedOperation]);

  const useManualFallback = useCallback(() => {
    if (!manualFallbackReady) return;
    if (operationRef.current?.phase === UpgradePhase.UNSPECIFIED) {
      updateAcknowledgedOperation({
        id: operationRef.current.id,
        phase: operationRef.current.phase,
        revision: operationRevision(operationRef.current),
      });
    }
    clearSurfacedOperation();
  }, [clearSurfacedOperation, manualFallbackReady, updateAcknowledgedOperation]);

  const reloadFleet = useCallback(() => {
    updateTrackedOperation(undefined);
    window.location.reload();
  }, [updateTrackedOperation]);

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
    trackedTargetVersion: trackedOperation?.targetVersion,
    triggerUpgrade,
    useManualFallback,
  };
}
