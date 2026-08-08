import { useCallback, useEffect, useRef, useState } from "react";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { type UpgradeOperation, UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const ACTIVE_POLL_INTERVAL_MS = 2_000;
const IDLE_POLL_INTERVAL_MS = 60_000;
const STATUS_REQUEST_TIMEOUT_MS = 10_000;
const TRIGGER_REQUEST_TIMEOUT_MS = 30_000;
const TRIGGER_RECONCILIATION_TIMEOUT_MS = 15_000;
const TRACKED_OPERATION_KEY = "protoFleet:tracked-upgrade-operation";
const ACKNOWLEDGED_OPERATION_KEY = "protoFleet:acknowledged-upgrade-operation";

interface TrackedOperation {
  id?: string;
  targetVersion: string;
}

interface AcknowledgedOperation {
  authSessionIdentity: string;
  id: string;
}

interface UseUpgradeOperationOptions {
  authSessionIdentity: string;
  currentVersion?: string;
  currentVersionUnavailable: boolean;
  enabled: boolean;
  onPollError?: (error: unknown) => void;
}

interface UseUpgradeOperationResult {
  acknowledgeOperation: () => void;
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

const readAcknowledgedOperation = (authSessionIdentity: string): string | null => {
  try {
    const raw = window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<AcknowledgedOperation>;
    if (typeof value.id === "string" && value.id && value.authSessionIdentity === authSessionIdentity) {
      return value.id;
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
  const acknowledgedOperationRef = useRef<string | null>(null);
  const acknowledgedOperationSessionRef = useRef<string | null>(null);
  const reconcilingRef = useRef(reconciling);
  const currentVersionRef = useRef(currentVersion);
  const currentVersionUnavailableRef = useRef(currentVersionUnavailable);
  const onPollErrorRef = useRef(onPollError);
  const reconciliationDeadlineRef = useRef<number | null>(null);
  const authSessionIdentityRef = useRef(authSessionIdentity);
  const resolvedStatusSessionIdentityRef = useRef<string | null>(null);

  currentVersionRef.current = currentVersion;
  currentVersionUnavailableRef.current = currentVersionUnavailable;
  onPollErrorRef.current = onPollError;
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

  const updateAcknowledgedOperation = useCallback((next: string | null) => {
    acknowledgedOperationRef.current = next;
    try {
      if (next) {
        window.sessionStorage.setItem(
          ACKNOWLEDGED_OPERATION_KEY,
          JSON.stringify({ authSessionIdentity: authSessionIdentityRef.current, id: next }),
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

  const acceptServerOperation = useCallback(
    (next: UpgradeOperation, fromTriggerResponse = false) => {
      if (!next.id) {
        return false;
      }
      if (next.phase === UpgradePhase.UNSPECIFIED && acknowledgedOperationRef.current === next.id) {
        return false;
      }
      const active = isUpgradeActive(next);
      const tracked = trackedOperationRef.current;
      const reconciledSuccess =
        reconcilingRef.current &&
        !tracked?.id &&
        tracked?.targetVersion === next.targetVersion &&
        next.phase === UpgradePhase.SUCCEEDED;
      const trackedMatches = tracked?.id
        ? tracked.id === next.id
        : (fromTriggerResponse && tracked?.targetVersion === next.targetVersion) || reconciledSuccess;

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

      if (acknowledgedOperationRef.current === next.id) {
        return false;
      }

      if (tracked && !trackedMatches) {
        return false;
      }

      // A manual recovery may have installed the failed target successfully.
      // Do not replay the updater's stale failure once Fleet reports that exact
      // version as current.
      if (next.phase === UpgradePhase.FAILED && currentVersionRef.current === next.targetVersion) {
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
        (currentVersionUnavailableRef.current ||
          Boolean(currentVersionRef.current && currentVersionRef.current !== next.targetVersion));
      if (!trackedMatches && !unresolvedFailure) {
        return false;
      }

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
        if (signal.aborted) return;
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
        reconciliationDeadlineRef.current = Date.now() + TRIGGER_RECONCILIATION_TIMEOUT_MS;
        updateReconciling(true);
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

  const acknowledgeOperation = useCallback(() => {
    if (operationRef.current && isUpgradeTerminal(operationRef.current.phase)) {
      updateAcknowledgedOperation(operationRef.current.id);
    }
    updateTrackedOperation(undefined);
    updateOperation(undefined);
    reconciliationDeadlineRef.current = null;
    setManualFallbackReady(false);
    updateReconciling(false);
    setConnectionLost(false);
    setTriggerError(null);
  }, [updateAcknowledgedOperation, updateOperation, updateReconciling, updateTrackedOperation]);

  const useManualFallback = useCallback(() => {
    if (!manualFallbackReady) return;
    if (operationRef.current?.phase === UpgradePhase.UNSPECIFIED) {
      updateAcknowledgedOperation(operationRef.current.id);
    }
    updateTrackedOperation(undefined);
    updateOperation(undefined);
    reconciliationDeadlineRef.current = null;
    setManualFallbackReady(false);
    updateReconciling(false);
    setConnectionLost(false);
    setTriggerError(null);
  }, [manualFallbackReady, updateAcknowledgedOperation, updateOperation, updateReconciling, updateTrackedOperation]);

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
