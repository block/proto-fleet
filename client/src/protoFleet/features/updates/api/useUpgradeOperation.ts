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
const PENDING_SUBMISSION_STORAGE_KEY = "proto-fleet-upgrade-pending-submission-v1";
const OPERATION_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const MAX_TARGET_VERSION_LENGTH = 256;
const MIN_PROTOBUF_TIMESTAMP_SECONDS = -62_135_596_800n;
const MAX_PROTOBUF_TIMESTAMP_SECONDS = 253_402_300_799n;
const MAX_PROTOBUF_TIMESTAMP_NANOS = 999_999_999;

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
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.PermissionDenied,
  Code.Unauthenticated,
  Code.Unimplemented,
]);
// AlreadyExists confirms a conflicting host mutation or recovery lock, not an
// idle updater. Keep the caller correlation until status authoritatively
// identifies the host operation or the operator confirms manual fallback.

const isDefinitiveTriggerRejection = (error: unknown) =>
  error instanceof ConnectError && DEFINITIVE_TRIGGER_REJECTION_CODES.has(error.code);

interface PendingSubmission {
  id: string;
  targetVersion: string;
}

interface StoredPendingSubmission extends PendingSubmission {
  authSessionIdentity: string;
  version: 1;
}

const isValidPendingSubmission = (value: unknown): value is StoredPendingSubmission => {
  if (!value || typeof value !== "object") {
    return false;
  }
  const candidate = value as Partial<StoredPendingSubmission>;
  return (
    candidate.version === 1 &&
    typeof candidate.authSessionIdentity === "string" &&
    candidate.authSessionIdentity.length > 0 &&
    typeof candidate.id === "string" &&
    OPERATION_ID_PATTERN.test(candidate.id) &&
    candidate.id !== "00000000-0000-0000-0000-000000000000" &&
    typeof candidate.targetVersion === "string" &&
    candidate.targetVersion.length > 0 &&
    candidate.targetVersion.length <= MAX_TARGET_VERSION_LENGTH &&
    candidate.targetVersion.trim() === candidate.targetVersion
  );
};

const removeStoredPendingSubmission = () => {
  try {
    window.sessionStorage.removeItem(PENDING_SUBMISSION_STORAGE_KEY);
  } catch {
    // Cleanup is best effort. A stale record remains scoped to its exact auth
    // session and will be ignored if that session no longer owns the page.
  }
};

const loadPendingSubmission = (authSessionIdentity: string): PendingSubmission | undefined => {
  try {
    const raw = window.sessionStorage.getItem(PENDING_SUBMISSION_STORAGE_KEY);
    if (!raw) {
      return undefined;
    }
    const stored: unknown = JSON.parse(raw);
    if (!isValidPendingSubmission(stored) || stored.authSessionIdentity !== authSessionIdentity) {
      removeStoredPendingSubmission();
      return undefined;
    }
    return { id: stored.id, targetVersion: stored.targetVersion };
  } catch {
    // A malformed or inaccessible record is never trusted for correlation.
    removeStoredPendingSubmission();
    return undefined;
  }
};

const persistPendingSubmission = (authSessionIdentity: string, pending: PendingSubmission) => {
  const stored: StoredPendingSubmission = {
    authSessionIdentity,
    id: pending.id,
    targetVersion: pending.targetVersion,
    version: 1,
  };
  const serialized = JSON.stringify(stored);
  try {
    window.sessionStorage.setItem(PENDING_SUBMISSION_STORAGE_KEY, serialized);
    if (window.sessionStorage.getItem(PENDING_SUBMISSION_STORAGE_KEY) !== serialized) {
      throw new Error("browser storage did not retain the pending upgrade");
    }
  } catch (error) {
    removeStoredPendingSubmission();
    throw error;
  }
};

const clearPendingSubmission = (authSessionIdentity: string, pending: PendingSubmission) => {
  try {
    const raw = window.sessionStorage.getItem(PENDING_SUBMISSION_STORAGE_KEY);
    if (!raw) {
      return;
    }
    const stored: unknown = JSON.parse(raw);
    if (
      isValidPendingSubmission(stored) &&
      stored.authSessionIdentity === authSessionIdentity &&
      stored.id === pending.id &&
      stored.targetVersion === pending.targetVersion
    ) {
      window.sessionStorage.removeItem(PENDING_SUBMISSION_STORAGE_KEY);
    }
  } catch {
    // Keep the UI fail-closed for this page instance. A later load will ignore
    // a malformed record or retry cleanup after authoritative reconciliation.
  }
};

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

const isValidOperationStartedAt = (
  startedAt: UpgradeOperation["startedAt"],
): startedAt is NonNullable<UpgradeOperation["startedAt"]> =>
  Boolean(
    startedAt &&
    typeof startedAt.seconds === "bigint" &&
    startedAt.seconds >= MIN_PROTOBUF_TIMESTAMP_SECONDS &&
    startedAt.seconds <= MAX_PROTOBUF_TIMESTAMP_SECONDS &&
    Number.isInteger(startedAt.nanos) &&
    startedAt.nanos >= 0 &&
    startedAt.nanos <= MAX_PROTOBUF_TIMESTAMP_NANOS &&
    (startedAt.seconds !== MIN_PROTOBUF_TIMESTAMP_SECONDS || startedAt.nanos !== 0),
  );

const operationStartedAtMatches = (
  actual: UpgradeOperation["startedAt"],
  expected: NonNullable<UpgradeOperation["startedAt"]>,
) => Boolean(actual && actual.seconds === expected.seconds && actual.nanos === expected.nanos);

export const getUpgradeOperationIncarnationKey = (operation: UpgradeOperation) => {
  if (!operation.id || !isValidOperationStartedAt(operation.startedAt)) {
    return undefined;
  }
  return `${operation.id}:${operation.startedAt.seconds}:${operation.startedAt.nanos}`;
};

export const getUpgradeOperationOutcomeKey = (operation: UpgradeOperation) => {
  const incarnationKey = getUpgradeOperationIncarnationKey(operation);
  if (!incarnationKey || !isUpgradeTerminal(operation.phase) || operation.outcomeRevision <= 0n) {
    return undefined;
  }
  return `${incarnationKey}:${operation.outcomeRevision}`;
};

export function useUpgradeOperation({
  authSessionIdentity,
  enabled,
  onUntrackedSuccess,
  onPollError,
}: UseUpgradeOperationOptions): UseUpgradeOperationResult {
  const initialPendingSubmissionRef = useRef<PendingSubmission | undefined>(undefined);
  const initialPendingSessionIdentityRef = useRef<string | null>(null);
  if (initialPendingSessionIdentityRef.current === null) {
    initialPendingSessionIdentityRef.current = authSessionIdentity;
    initialPendingSubmissionRef.current = loadPendingSubmission(authSessionIdentity);
  }
  const [operation, setOperation] = useState<UpgradeOperation>();
  const [pendingSubmission, setPendingSubmission] = useState<PendingSubmission | undefined>(
    initialPendingSubmissionRef.current,
  );
  const [triggering, setTriggering] = useState(false);
  const [reconciling, setReconciling] = useState(false);
  const [connectionLost, setConnectionLost] = useState(false);
  const [manualFallbackReady, setManualFallbackReady] = useState(false);
  const [triggerError, setTriggerError] = useState<string | null>(null);
  const [pollRevision, setPollRevision] = useState(0);
  const [resolvedStatusSessionIdentity, setResolvedStatusSessionIdentity] = useState<string | null>(null);

  const operationRef = useRef(operation);
  const pendingSubmissionRef = useRef(initialPendingSubmissionRef.current);
  const pendingSubmissionSessionIdentityRef = useRef(authSessionIdentity);
  const reconcilingRef = useRef(reconciling);
  const reconciliationDeadlineRef = useRef<number | null>(null);
  const statusRequestEpochRef = useRef(0);
  const mutationGenerationRef = useRef(0);
  const authSessionIdentityRef = useRef(authSessionIdentity);
  const resolvedStatusSessionIdentityRef = useRef<string | null>(null);
  const onPollErrorRef = useRef(onPollError);
  const onUntrackedSuccessRef = useRef(onUntrackedSuccess);
  const submittedOperationIncarnationsRef = useRef(new Set<string>());
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
    const previous = pendingSubmissionRef.current;
    if (!next && previous) {
      clearPendingSubmission(pendingSubmissionSessionIdentityRef.current, previous);
    }
    pendingSubmissionSessionIdentityRef.current = authSessionIdentityRef.current;
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

  useEffect(() => {
    if (pendingSubmissionSessionIdentityRef.current === authSessionIdentity) {
      return;
    }

    // A pending command belongs to exactly one authenticated session. Purge a
    // previous session's record before this hook can poll or mutate as the new
    // session, then restore only a record whose full identity matches.
    const restored = loadPendingSubmission(authSessionIdentity);
    pendingSubmissionSessionIdentityRef.current = authSessionIdentity;
    pendingSubmissionRef.current = restored;
    setPendingSubmission(restored);
    submittedOperationIncarnationsRef.current.clear();
    refreshedUntrackedSuccessesRef.current.clear();
    mutationGenerationRef.current += 1;
    statusRequestEpochRef.current += 1;
    resolvedStatusSessionIdentityRef.current = null;
    setResolvedStatusSessionIdentity(null);
    updateOperation(undefined);
    setTriggering(false);
    setTriggerError(null);
    resetReconciliation();
  }, [authSessionIdentity, resetReconciliation, updateOperation]);

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
    if (next.phase !== UpgradePhase.SUCCEEDED) {
      return;
    }
    const incarnationKey = getUpgradeOperationIncarnationKey(next);
    if (incarnationKey && submittedOperationIncarnationsRef.current.has(incarnationKey)) {
      return;
    }
    const outcomeKey = getUpgradeOperationOutcomeKey(next);
    if (outcomeKey && refreshedUntrackedSuccessesRef.current.has(outcomeKey)) {
      return;
    }
    // Missing or invalid host identity fails open: refresh again instead of
    // allowing a malformed key to suppress a distinct successful outcome.
    if (outcomeKey) {
      refreshedUntrackedSuccessesRef.current.add(outcomeKey);
    }
    onUntrackedSuccessRef.current?.(next);
  }, []);

  const acceptServerOperation = useCallback(
    (next: UpgradeOperation) => {
      if (!next.id) {
        return;
      }

      const pendingMatches =
        pendingSubmissionRef.current?.id === next.id &&
        pendingSubmissionRef.current.targetVersion === next.targetVersion;
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
        // Retain caller correlation while the matching host ID and target are
        // active. This record is a lock/reconciliation hint, not proof that
        // this browser submitted the reported incarnation; only a correlated
        // trigger response establishes that ownership.
        if (isUpgradeTerminal(next.phase)) {
          updatePendingSubmission(undefined);
        }
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
          const pendingMatches =
            pendingSubmissionRef.current?.id === response.operation.id &&
            pendingSubmissionRef.current.targetVersion === response.operation.targetVersion;
          acceptServerOperation(response.operation);
          if (pendingSubmissionRef.current && !pendingMatches) {
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
      const nextPendingSubmission = { id: operationId, targetVersion };
      setTriggerError(null);
      try {
        // Persist and verify the exact command identity before the mutation can
        // leave the browser. Reloads can then retain the correlation lock while
        // the detached server request finishes.
        persistPendingSubmission(authSessionIdentityRef.current, nextPendingSubmission);
      } catch {
        setTriggerError("Fleet couldn't safely track this upgrade request in this browser. No upgrade was started.");
        return;
      }
      const initiatingAuthSessionIdentity = authSessionIdentityRef.current;
      const mutationGeneration = ++mutationGenerationRef.current;
      const isCurrentMutation = () =>
        authSessionIdentityRef.current === initiatingAuthSessionIdentity &&
        mutationGenerationRef.current === mutationGeneration;
      updatePendingSubmission(nextPendingSubmission);
      reconciliationDeadlineRef.current = null;
      setManualFallbackReady(false);
      updateReconciling(false);
      setConnectionLost(false);
      setTriggering(true);
      try {
        const response = await instanceUpdateClient.triggerUpgrade(
          { operationId, targetVersion },
          { timeoutMs: TRIGGER_REQUEST_TIMEOUT_MS },
        );
        if (!isCurrentMutation()) {
          return;
        }
        if (
          !response.operation ||
          response.operation.id !== operationId ||
          response.operation.targetVersion !== targetVersion
        ) {
          throw new Error("Host updater did not return the requested operation");
        }
        const submittedIncarnationKey = getUpgradeOperationIncarnationKey(response.operation);
        if (submittedIncarnationKey) {
          submittedOperationIncarnationsRef.current.add(submittedIncarnationKey);
        }
        acceptServerOperation(response.operation);
      } catch (error) {
        if (!isCurrentMutation()) {
          return;
        }
        setTriggerError(getErrorMessage(error, "Couldn't start upgrade"));
        if (isDefinitiveTriggerRejection(error)) {
          updatePendingSubmission(undefined);
          resetReconciliation();
        } else {
          beginReconciliation(false);
        }
      } finally {
        if (isCurrentMutation()) {
          // Also invalidate a poll that may have started while the mutation was
          // in flight, then wake a fresh authoritative poll.
          statusRequestEpochRef.current += 1;
          setTriggering(false);
          // Wake an idle poll so an ambiguous response is reconciled by the
          // exact operation ID without relying on target/version heuristics.
          setPollRevision((revision) => revision + 1);
        }
      }
    },
    [acceptServerOperation, beginReconciliation, resetReconciliation, updatePendingSubmission, updateReconciling],
  );

  const acknowledgeOperation = useCallback(async () => {
    const terminalOperation = operationRef.current;
    if (!terminalOperation?.id || !isUpgradeTerminal(terminalOperation.phase)) {
      return;
    }
    const expectedStartedAt = terminalOperation.startedAt;
    if (!isValidOperationStartedAt(expectedStartedAt)) {
      throw new Error("Host did not provide a valid start time for this upgrade outcome");
    }

    const initiatingAuthSessionIdentity = authSessionIdentityRef.current;
    const mutationGeneration = ++mutationGenerationRef.current;
    const isCurrentMutation = () =>
      authSessionIdentityRef.current === initiatingAuthSessionIdentity &&
      mutationGenerationRef.current === mutationGeneration;
    statusRequestEpochRef.current += 1;
    const expectedOutcomeRevision = terminalOperation.outcomeRevision;
    try {
      const response = await instanceUpdateClient.acknowledgeUpgrade(
        { operationId: terminalOperation.id, expectedOutcomeRevision, expectedStartedAt },
        { timeoutMs: ACKNOWLEDGE_REQUEST_TIMEOUT_MS },
      );
      if (!isCurrentMutation()) {
        return;
      }
      const acknowledged = response.operation;
      if (
        !acknowledged ||
        acknowledged.id !== terminalOperation.id ||
        !operationStartedAtMatches(acknowledged.startedAt, expectedStartedAt) ||
        acknowledged.outcomeRevision !== expectedOutcomeRevision ||
        !acknowledged.acknowledged ||
        !isUpgradeTerminal(acknowledged.phase)
      ) {
        throw new Error("Host did not acknowledge the current upgrade outcome");
      }

      const current = operationRef.current;
      if (
        current?.id === terminalOperation.id &&
        operationStartedAtMatches(current.startedAt, expectedStartedAt) &&
        current.outcomeRevision === expectedOutcomeRevision
      ) {
        updateOperation(undefined);
      }
    } finally {
      if (isCurrentMutation()) {
        statusRequestEpochRef.current += 1;
        setPollRevision((revision) => revision + 1);
      }
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

  const trackedTargetVersion =
    pendingSubmission &&
    pendingSubmissionSessionIdentityRef.current === authSessionIdentity &&
    (operation?.id !== pendingSubmission.id || operation.targetVersion !== pendingSubmission.targetVersion)
      ? pendingSubmission.targetVersion
      : undefined;

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
    trackedTargetVersion,
    triggerUpgrade,
    useManualFallback,
  };
}
