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
    expect(window.sessionStorage.length).toBe(1);
    expect(globalThis.crypto.getRandomValues).toHaveBeenCalled();
  });

  it("retains an observed active operation's correlation through a later remount and updater outage", async () => {
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(false));
    mockTriggerUpgrade.mockResolvedValue(triggerResponse(activeOperation));
    const first = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => first.result.current.triggerUpgrade("v1.3.0"));
    expect(first.result.current.operation?.phase).toBe(UpgradePhase.PREFLIGHT);
    expect(window.sessionStorage.length).toBe(1);
    first.unmount();

    const restored = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(restored.result.current.connectionLost).toBe(true));

    expect(restored.result.current.operation).toBeUndefined();
    expect(restored.result.current.trackedTargetVersion).toBe("v1.3.0");
    expect(restored.result.current.reconciling).toBe(true);
    expect(restored.result.current.manualFallbackReady).toBe(false);
    expect(window.sessionStorage.length).toBe(1);
  });

  it("clears persisted correlation when the exact active operation becomes terminal", async () => {
    const terminalPoll = deferred<ReturnType<typeof status>>();
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    const succeededOperation = operation(UpgradePhase.SUCCEEDED, { message: "Upgrade complete" });
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockReturnValue(terminalPoll.promise);
    mockTriggerUpgrade.mockResolvedValue(triggerResponse(activeOperation));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    expect(result.current.operation?.phase).toBe(UpgradePhase.PREFLIGHT);
    expect(window.sessionStorage.length).toBe(1);
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(2));

    await act(async () => {
      terminalPoll.resolve(status(true, succeededOperation));
      await terminalPoll.promise;
    });

    expect(result.current.operation?.phase).toBe(UpgradePhase.SUCCEEDED);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("fails closed before the trigger RPC when the pending command cannot be persisted", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status());
    vi.spyOn(Object.getPrototypeOf(window.sessionStorage) as Storage, "setItem").mockImplementation(() => {
      throw new DOMException("storage disabled", "SecurityError");
    });
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(mockTriggerUpgrade).not.toHaveBeenCalled();
    expect(result.current.triggering).toBe(false);
    expect(result.current.reconciling).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(result.current.triggerError).toContain("No upgrade was started");
  });

  it("restores an in-flight submission after remount and keeps its exact correlation through an outage", async () => {
    vi.useFakeTimers();
    const trigger = deferred<ReturnType<typeof triggerResponse>>();
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status())
      .mockResolvedValueOnce(status(false))
      .mockResolvedValue(status(true, activeOperation));
    mockTriggerUpgrade.mockReturnValue(trigger.promise);

    const first = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    let submission!: Promise<void>;
    act(() => {
      submission = first.result.current.triggerUpgrade("v1.3.0");
    });
    expect(mockTriggerUpgrade).toHaveBeenCalledWith(
      { operationId: OPERATION_ID, targetVersion: "v1.3.0" },
      { timeoutMs: 30_000 },
    );
    expect(window.sessionStorage.length).toBe(1);
    first.unmount();

    const restored = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    expect(restored.result.current.trackedTargetVersion).toBe("v1.3.0");
    expect(restored.result.current.reconciling).toBe(true);
    expect(restored.result.current.connectionLost).toBe(true);
    expect(window.sessionStorage.length).toBe(1);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(restored.result.current.operation?.id).toBe(OPERATION_ID);
    expect(restored.result.current.trackedTargetVersion).toBeUndefined();
    expect(restored.result.current.reconciling).toBe(false);
    expect(window.sessionStorage.length).toBe(1);

    await act(async () => {
      trigger.reject(new Error("original response lost during reload"));
      await submission;
    });
  });

  it("clears a restored submission after a bounded reachable exact-ID miss", async () => {
    vi.useFakeTimers();
    const trigger = deferred<ReturnType<typeof triggerResponse>>();
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockReturnValue(trigger.promise);

    const first = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    let submission!: Promise<void>;
    act(() => {
      submission = first.result.current.triggerUpgrade("v1.3.0");
    });
    expect(window.sessionStorage.length).toBe(1);
    first.unmount();

    const restored = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await act(async () => Promise.resolve());
    expect(restored.result.current.trackedTargetVersion).toBe("v1.3.0");
    expect(restored.result.current.reconciling).toBe(true);

    await act(async () => vi.advanceTimersByTimeAsync(17_000));
    expect(restored.result.current.trackedTargetVersion).toBeUndefined();
    expect(restored.result.current.reconciling).toBe(false);
    expect(window.sessionStorage.length).toBe(0);

    await act(async () => {
      trigger.reject(new Error("original response lost during reload"));
      await submission;
    });
  });

  it("purges a pending submission instead of exposing it to a replacement auth session", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(false));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { rerender, result } = renderHook(
      ({ authSessionIdentity }: { authSessionIdentity: string }) =>
        useTestUpgradeOperation({ enabled: true }, authSessionIdentity),
      { initialProps: { authSessionIdentity: AUTH_SESSION_IDENTITY } },
    );
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    expect(result.current.trackedTargetVersion).toBe("v1.3.0");
    expect(window.sessionStorage.length).toBe(1);

    const previousPollCount = mockGetUpgradeStatus.mock.calls.length;
    rerender({ authSessionIdentity: "operator-b:2" });

    await waitFor(() => expect(mockGetUpgradeStatus.mock.calls.length).toBeGreaterThan(previousPollCount));
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(result.current.triggerError).toBeNull();
    expect(window.sessionStorage.length).toBe(0);
  });

  it("ignores an old trigger completion after a replacement auth session submits a new command", async () => {
    const oldTrigger = deferred<ReturnType<typeof triggerResponse>>();
    const newTrigger = deferred<ReturnType<typeof triggerResponse>>();
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockReturnValueOnce(oldTrigger.promise).mockReturnValueOnce(newTrigger.promise);
    const { rerender, result } = renderHook(
      ({ authSessionIdentity }: { authSessionIdentity: string }) =>
        useTestUpgradeOperation({ enabled: true }, authSessionIdentity),
      { initialProps: { authSessionIdentity: AUTH_SESSION_IDENTITY } },
    );
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    let oldSubmission!: Promise<void>;
    act(() => {
      oldSubmission = result.current.triggerUpgrade("v1.3.0");
    });
    expect(result.current.triggering).toBe(true);

    const previousPollCount = mockGetUpgradeStatus.mock.calls.length;
    rerender({ authSessionIdentity: "operator-b:2" });
    await waitFor(() => expect(mockGetUpgradeStatus.mock.calls.length).toBeGreaterThan(previousPollCount));
    expect(result.current.triggering).toBe(false);

    let newSubmission!: Promise<void>;
    act(() => {
      newSubmission = result.current.triggerUpgrade("v1.4.0");
    });
    expect(result.current.triggering).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.4.0");

    await act(async () => {
      oldTrigger.resolve(triggerResponse(operation(UpgradePhase.PREFLIGHT)));
      await oldSubmission;
    });

    expect(result.current.operation).toBeUndefined();
    expect(result.current.triggering).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.4.0");
    expect(JSON.parse(window.sessionStorage.getItem(window.sessionStorage.key(0) ?? "") ?? "{}")).toMatchObject({
      authSessionIdentity: "operator-b:2",
      targetVersion: "v1.4.0",
    });

    await act(async () => {
      newTrigger.resolve(triggerResponse(operation(UpgradePhase.PREFLIGHT, { targetVersion: "v1.4.0" })));
      await newSubmission;
    });
    expect(result.current.operation?.targetVersion).toBe("v1.4.0");
    expect(result.current.triggering).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(window.sessionStorage.length).toBe(1);
  });

  it("does not let an older same-session rejection clear a newer submission", async () => {
    let operationSequence = 0;
    vi.mocked(globalThis.crypto.getRandomValues).mockImplementation((array) => {
      const bytes = array as Uint8Array;
      bytes.fill(0);
      bytes[15] = ++operationSequence;
      return array;
    });
    const oldTrigger = deferred<ReturnType<typeof triggerResponse>>();
    const newTrigger = deferred<ReturnType<typeof triggerResponse>>();
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockReturnValueOnce(oldTrigger.promise).mockReturnValueOnce(newTrigger.promise);
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    let oldSubmission!: Promise<void>;
    let newSubmission!: Promise<void>;
    act(() => {
      oldSubmission = result.current.triggerUpgrade("v1.3.0");
      newSubmission = result.current.triggerUpgrade("v1.4.0");
    });
    expect(result.current.triggering).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.4.0");

    await act(async () => {
      oldTrigger.reject(new ConnectError("old offer expired", Code.FailedPrecondition));
      await oldSubmission;
    });

    expect(result.current.triggering).toBe(true);
    expect(result.current.triggerError).toBeNull();
    expect(result.current.trackedTargetVersion).toBe("v1.4.0");
    expect(JSON.parse(window.sessionStorage.getItem(window.sessionStorage.key(0) ?? "") ?? "{}")).toMatchObject({
      id: "00000000-0000-4000-8000-000000000002",
      targetVersion: "v1.4.0",
    });

    await act(async () => {
      newTrigger.resolve(
        triggerResponse(
          operation(UpgradePhase.PREFLIGHT, {
            id: "00000000-0000-4000-8000-000000000002",
            targetVersion: "v1.4.0",
          }),
        ),
      );
      await newSubmission;
    });
    expect(result.current.operation?.id).toBe("00000000-0000-4000-8000-000000000002");
    expect(result.current.triggering).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(window.sessionStorage.length).toBe(1);
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
    expect(window.sessionStorage.length).toBe(0);
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
    expect(window.sessionStorage.length).toBe(0);
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

  it("ignores an acknowledgement completion from a replaced auth session", async () => {
    const oldOperation = operation(UpgradePhase.FAILED, { message: "Old session outcome" });
    const newOperation = operation(UpgradePhase.FAILED, { message: "New session outcome" });
    const oldAcknowledgementResponse = create(AcknowledgeUpgradeResponseSchema, {
      operation: operation(UpgradePhase.FAILED, { acknowledged: true, message: "Old session outcome" }),
    });
    const acknowledgement = deferred<typeof oldAcknowledgementResponse>();
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, oldOperation))
      .mockResolvedValue(status(true, newOperation));
    mockAcknowledgeUpgrade.mockReturnValue(acknowledgement.promise);
    const { rerender, result } = renderHook(
      ({ authSessionIdentity }: { authSessionIdentity: string }) =>
        useTestUpgradeOperation({ enabled: true }, authSessionIdentity),
      { initialProps: { authSessionIdentity: AUTH_SESSION_IDENTITY } },
    );
    await waitFor(() => expect(result.current.operation?.message).toBe("Old session outcome"));

    let oldAcknowledgement!: Promise<void>;
    act(() => {
      oldAcknowledgement = result.current.acknowledgeOperation();
    });
    rerender({ authSessionIdentity: "operator-b:2" });
    await waitFor(() => expect(result.current.operation?.message).toBe("New session outcome"));
    const replacementSessionPollCount = mockGetUpgradeStatus.mock.calls.length;

    await act(async () => {
      acknowledgement.resolve(oldAcknowledgementResponse);
      await oldAcknowledgement;
    });

    expect(result.current.operation?.message).toBe("New session outcome");
    expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(replacementSessionPollCount);
  });

  it("forwards poll errors for page-level auth handling", async () => {
    const onPollError = vi.fn();
    const pollError = new Error("permission revoked");
    mockGetUpgradeStatus.mockRejectedValue(pollError);

    renderHook(() => useTestUpgradeOperation({ enabled: true, onPollError }));

    await waitFor(() => expect(onPollError).toHaveBeenCalledWith(pollError));
  });
});
