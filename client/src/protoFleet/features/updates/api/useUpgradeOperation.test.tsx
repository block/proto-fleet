import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import {
  AcknowledgeUpgradeResponseSchema,
  GetUpgradeStatusResponseSchema,
  TriggerUpgradeResponseSchema,
  type UpgradeOperation,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { isUpgradeActive, useUpgradeOperation } from "@/protoFleet/features/updates/api/useUpgradeOperation";

vi.mock("@/protoFleet/api/clients", () => ({
  instanceUpdateClient: {
    acknowledgeUpgrade: vi.fn(),
    getUpgradeStatus: vi.fn(),
    triggerUpgrade: vi.fn(),
  },
}));

const mockAcknowledgeUpgrade = vi.mocked(instanceUpdateClient.acknowledgeUpgrade);
const mockGetUpgradeStatus = vi.mocked(instanceUpdateClient.getUpgradeStatus);
const mockTriggerUpgrade = vi.mocked(instanceUpdateClient.triggerUpgrade);
const AUTH_SESSION_IDENTITY = "operator-a:1";
const OPERATION_ID = "00000000-0000-4000-8000-000000000001";

type TestUpgradeOperationOptions = Omit<Parameters<typeof useUpgradeOperation>[0], "authSessionIdentity">;

const useTestUpgradeOperation = (options: TestUpgradeOperationOptions, authSessionIdentity = AUTH_SESSION_IDENTITY) =>
  useUpgradeOperation({ authSessionIdentity, ...options });

type MessageOverrides<T> = Omit<Partial<T>, "$typeName" | "$unknown">;

const operation = (phase: UpgradePhase, overrides?: MessageOverrides<UpgradeOperation>) =>
  create(UpgradeOperationSchema, {
    id: OPERATION_ID,
    targetVersion: "v1.3.0",
    phase,
    message: "Preparing upgrade",
    outcomeRevision: 1n,
    ...overrides,
  });

const status = (executorAvailable = true, currentOperation?: UpgradeOperation) =>
  create(GetUpgradeStatusResponseSchema, {
    executorAvailable,
    operation: currentOperation,
  });

const triggerResponse = (currentOperation: UpgradeOperation) =>
  create(TriggerUpgradeResponseSchema, { operation: currentOperation });

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
};

beforeEach(() => {
  vi.clearAllMocks();
  window.sessionStorage.clear();
  vi.spyOn(globalThis.crypto, "getRandomValues").mockImplementation((array) => {
    const bytes = array as Uint8Array;
    bytes.fill(0);
    bytes[15] = 1;
    return array;
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("useUpgradeOperation", () => {
  it("surfaces the host's durable active operation without browser storage", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.PREFLIGHT)));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));

    await waitFor(() => expect(result.current.operation?.id).toBe(OPERATION_ID));
    expect(result.current.reconciling).toBe(false);
    expect(result.current.connectionLost).toBe(false);
    expect(window.sessionStorage.length).toBe(0);
    expect(mockGetUpgradeStatus).toHaveBeenCalledWith(
      {},
      expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 10_000 }),
    );
  });

  it("does not overlap a pending poll and aborts it on cleanup", async () => {
    vi.useFakeTimers();
    const request = deferred<ReturnType<typeof status>>();
    mockGetUpgradeStatus.mockReturnValue(request.promise);

    const hook = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(120_000));
    expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1);
    const signal = mockGetUpgradeStatus.mock.calls[0]?.[1]?.signal;

    hook.unmount();
    expect(signal?.aborted).toBe(true);
  });

  it("keeps status pending until the first authoritative response", async () => {
    const request = deferred<ReturnType<typeof status>>();
    mockGetUpgradeStatus.mockReturnValue(request.promise);

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));

    expect(result.current.operationStatusPending).toBe(true);
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    await act(async () => {
      request.resolve(status());
      await request.promise;
    });
    expect(result.current.operationStatusPending).toBe(false);
  });

  it("restarts polling and ignores stale responses when the auth session changes", async () => {
    const previousRequest = deferred<ReturnType<typeof status>>();
    const onPollError = vi.fn();
    mockGetUpgradeStatus.mockReturnValueOnce(previousRequest.promise).mockResolvedValue(status());

    const { rerender, result } = renderHook(
      ({ authSessionIdentity }: { authSessionIdentity: string }) =>
        useTestUpgradeOperation({ enabled: true, onPollError }, authSessionIdentity),
      { initialProps: { authSessionIdentity: AUTH_SESSION_IDENTITY } },
    );
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    const abortSpy = vi.spyOn(AbortController.prototype, "abort").mockImplementation(() => undefined);
    rerender({ authSessionIdentity: "operator-a:2" });

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.operationStatusPending).toBe(false));
    expect(abortSpy).toHaveBeenCalled();

    await act(async () => {
      previousRequest.reject(new Error("previous session expired"));
      await Promise.resolve();
    });
    expect(onPollError).not.toHaveBeenCalled();
  });

  it("submits a caller-generated operation ID without secure-context randomUUID", async () => {
    const insecureContextCrypto = Object.create(globalThis.crypto) as Crypto;
    Object.defineProperty(insecureContextCrypto, "randomUUID", { configurable: true, value: undefined });
    vi.stubGlobal("crypto", insecureContextCrypto);
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, activeOperation));
    mockTriggerUpgrade.mockResolvedValue(triggerResponse(activeOperation));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(mockTriggerUpgrade).toHaveBeenCalledWith(
      { operationId: OPERATION_ID, targetVersion: "v1.3.0" },
      { timeoutMs: 30_000 },
    );
    expect(result.current.operation?.phase).toBe(UpgradePhase.PREFLIGHT);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(window.sessionStorage.length).toBe(0);
    expect(globalThis.crypto.getRandomValues).toHaveBeenCalled();
  });

  it("ignores a pre-trigger status response that resolves after the trigger", async () => {
    const stalePoll = deferred<ReturnType<typeof status>>();
    const trigger = deferred<ReturnType<typeof triggerResponse>>();
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    const oldAcknowledgedOperation = operation(UpgradePhase.FAILED, {
      acknowledged: true,
      id: "00000000-0000-4000-8000-000000000002",
    });
    mockGetUpgradeStatus.mockReturnValueOnce(stalePoll.promise).mockResolvedValue(status(true, activeOperation));
    mockTriggerUpgrade.mockReturnValue(trigger.promise);
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => {
      const submission = result.current.triggerUpgrade("v1.3.0");
      trigger.resolve(triggerResponse(activeOperation));
      await trigger.promise;
      stalePoll.resolve(status(true, oldAcknowledgedOperation));
      await Promise.all([submission, stalePoll.promise]);
    });

    expect(result.current.operation?.id).toBe(OPERATION_ID);
    expect(result.current.operation?.phase).toBe(UpgradePhase.PREFLIGHT);
    expect(result.current.trackedTargetVersion).toBeUndefined();
  });

  it("treats an exact operation with an unknown phase as host-authoritative and locked", async () => {
    const unknownOperation = operation(UpgradePhase.UNSPECIFIED);
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, unknownOperation));
    mockTriggerUpgrade.mockResolvedValue(triggerResponse(unknownOperation));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(result.current.operation?.phase).toBe(UpgradePhase.UNSPECIFIED);
    expect(isUpgradeActive(result.current.operation)).toBe(true);
    expect(result.current.reconciling).toBe(false);
    expect(result.current.manualFallbackReady).toBe(false);
  });

  it("reconciles an ambiguous response by exact operation ID", async () => {
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, activeOperation));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await waitFor(() => expect(result.current.operation?.id).toBe(OPERATION_ID));

    expect(result.current.reconciling).toBe(false);
    expect(result.current.triggerError).toBeNull();
    expect(result.current.trackedTargetVersion).toBeUndefined();
  });

  it("does not let a different same-target operation resolve an ambiguous response", async () => {
    const previousOperation = operation(UpgradePhase.FAILED, { id: "operation-old" });
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, previousOperation));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await waitFor(() => expect(result.current.operation?.id).toBe("operation-old"));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.3.0");
    expect(result.current.triggerError).toContain("response lost");
  });

  it("unlocks immediately after a definitive trigger rejection", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockRejectedValue(
      new ConnectError('target "v1.3.0" is no longer eligible', Code.FailedPrecondition),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(result.current.reconciling).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(result.current.triggerError).toContain("no longer eligible");
  });

  it("ends bounded reconciliation after a reachable exact-operation miss", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockRejectedValue(new Error("host did not confirm"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    expect(result.current.reconciling).toBe(true);

    await act(async () => vi.advanceTimersByTimeAsync(17_000));

    expect(result.current.reconciling).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(result.current.triggerError).toContain("host did not confirm");
    expect(result.current.manualFallbackReady).toBe(false);
  });

  it("offers explicit fallback for an ambiguous submission while the executor is unreachable", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(false));
    mockTriggerUpgrade.mockRejectedValue(new Error("host did not confirm"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await act(async () => vi.advanceTimersByTimeAsync(17_000));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.manualFallbackReady).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.3.0");

    act(() => result.current.useManualFallback());
    expect(result.current.reconciling).toBe(false);
    expect(result.current.connectionLost).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(window.sessionStorage.length).toBe(0);
  });

  it("keeps an exact active operation locked through disconnect until a terminal response", async () => {
    vi.useFakeTimers();
    const activeOperation = operation(UpgradePhase.ACTIVATING);
    const succeededOperation = operation(UpgradePhase.SUCCEEDED, { message: "Upgrade complete" });
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, activeOperation))
      .mockRejectedValueOnce(new Error("Fleet restarting"))
      .mockResolvedValue(status(true, succeededOperation));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.reconciling).toBe(true);
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);
    expect(result.current.manualFallbackReady).toBe(false);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.operation?.phase).toBe(UpgradePhase.SUCCEEDED);
    expect(result.current.connectionLost).toBe(false);
    expect(result.current.reconciling).toBe(false);
  });

  it("does not unlock a known active operation on repeated reachable misses", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, operation(UpgradePhase.ACTIVATING)))
      .mockResolvedValue(status());
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());

    await act(async () => vi.advanceTimersByTimeAsync(30_000));

    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);
    expect(result.current.reconciling).toBe(true);
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.manualFallbackReady).toBe(false);
    act(() => result.current.useManualFallback());
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);
  });

  it("surfaces every unacknowledged terminal failure without version inference", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));

    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));
  });

  it("refreshes once for each untracked success outcome revision and still surfaces it", async () => {
    vi.useFakeTimers();
    const onUntrackedSuccess = vi.fn();
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.SUCCEEDED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, onUntrackedSuccess }));
    await act(async () => Promise.resolve());

    expect(onUntrackedSuccess).toHaveBeenCalledTimes(1);
    expect(result.current.operation?.phase).toBe(UpgradePhase.SUCCEEDED);

    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(onUntrackedSuccess).toHaveBeenCalledTimes(1);

    mockGetUpgradeStatus.mockResolvedValue(
      status(true, operation(UpgradePhase.SUCCEEDED, { outcomeRevision: 2n, message: "Recovered" })),
    );
    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(onUntrackedSuccess).toHaveBeenCalledTimes(2);
  });

  it("hides a terminal outcome only after the host reports it acknowledged", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, operation(UpgradePhase.FAILED)))
      .mockResolvedValue(status(true, operation(UpgradePhase.FAILED, { acknowledged: true })));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED);

    await act(async () => vi.advanceTimersByTimeAsync(60_000));

    expect(result.current.operation).toBeUndefined();
  });

  it("acknowledges the exact outcome revision and clears it after host confirmation", async () => {
    const failedOperation = operation(UpgradePhase.FAILED, { outcomeRevision: 7n });
    const acknowledgedOperation = operation(UpgradePhase.FAILED, { acknowledged: true, outcomeRevision: 7n });
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, failedOperation))
      .mockResolvedValue(status(true, acknowledgedOperation));
    mockAcknowledgeUpgrade.mockResolvedValue(
      create(AcknowledgeUpgradeResponseSchema, {
        operation: acknowledgedOperation,
      }),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    await act(async () => result.current.acknowledgeOperation());

    expect(mockAcknowledgeUpgrade).toHaveBeenCalledWith(
      { operationId: OPERATION_ID, expectedOutcomeRevision: 7n },
      { timeoutMs: 25_000 },
    );
    expect(result.current.operation).toBeUndefined();
  });

  it("keeps the outcome visible when acknowledgement fails", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    mockAcknowledgeUpgrade.mockRejectedValue(new ConnectError("host unreachable", Code.Unavailable));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    let acknowledgeError: unknown;
    await act(async () => result.current.acknowledgeOperation().catch((error: unknown) => (acknowledgeError = error)));

    expect(acknowledgeError).toBeInstanceOf(ConnectError);
    expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED);
  });

  it("keeps the newer outcome visible if the acknowledgement response does not match its revision", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    mockAcknowledgeUpgrade.mockResolvedValue(
      create(AcknowledgeUpgradeResponseSchema, {
        operation: operation(UpgradePhase.FAILED, { acknowledged: true, outcomeRevision: 2n }),
      }),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    let acknowledgeError: unknown;
    await act(async () => result.current.acknowledgeOperation().catch((error: unknown) => (acknowledgeError = error)));

    expect(acknowledgeError).toBeInstanceOf(Error);
    expect(result.current.operation?.outcomeRevision).toBe(1n);
  });

  it("forwards poll errors for page-level auth handling", async () => {
    const onPollError = vi.fn();
    const pollError = new Error("permission revoked");
    mockGetUpgradeStatus.mockRejectedValue(pollError);

    renderHook(() => useTestUpgradeOperation({ enabled: true, onPollError }));

    await waitFor(() => expect(onPollError).toHaveBeenCalledWith(pollError));
  });
});
