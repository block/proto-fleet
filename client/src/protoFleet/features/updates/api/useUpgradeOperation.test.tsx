import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
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
const TRACKED_OPERATION_KEY = "protoFleet:tracked-upgrade-operation";
const ACKNOWLEDGED_OPERATION_KEY = "protoFleet:acknowledged-upgrade-operation";
const AUTH_SESSION_IDENTITY = "operator-a:1";

type TestUpgradeOperationOptions = Omit<
  Parameters<typeof useUpgradeOperation>[0],
  "authSessionIdentity" | "currentVersionUnavailable"
> & {
  currentVersionUnavailable?: boolean;
};

const useTestUpgradeOperation = (options: TestUpgradeOperationOptions, authSessionIdentity = AUTH_SESSION_IDENTITY) =>
  useUpgradeOperation({ authSessionIdentity, currentVersionUnavailable: false, ...options });

type MessageOverrides<T> = Omit<Partial<T>, "$typeName" | "$unknown">;

const timestamp = (seconds: number) => create(TimestampSchema, { seconds: BigInt(seconds) });

const operation = (phase: UpgradePhase, overrides?: MessageOverrides<UpgradeOperation>) =>
  create(UpgradeOperationSchema, {
    id: "operation-1",
    targetVersion: "v1.3.0",
    phase,
    message: "Preparing upgrade",
    startedAt: timestamp(100),
    updatedAt: timestamp(100),
    ...overrides,
  });

const status = (executorAvailable = true, currentOperation?: UpgradeOperation) =>
  create(GetUpgradeStatusResponseSchema, {
    executorAvailable,
    operation: currentOperation,
  });

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
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useUpgradeOperation", () => {
  it("recovers a durable active operation without a capability gate", async () => {
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    window.sessionStorage.setItem(
      TRACKED_OPERATION_KEY,
      JSON.stringify({ id: activeOperation.id, targetVersion: activeOperation.targetVersion }),
    );
    mockGetUpgradeStatus.mockResolvedValue(status(true, activeOperation));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    expect(result.current.reconciling).toBe(true);
    await waitFor(() => expect(result.current.operation?.id).toBe("operation-1"));
    expect(result.current.reconciling).toBe(false);
    expect(result.current.connectionLost).toBe(false);
    expect(mockGetUpgradeStatus).toHaveBeenCalledWith(
      {},
      expect.objectContaining({ signal: expect.any(AbortSignal), timeoutMs: 10_000 }),
    );
  });

  it("removes a malformed tracked-operation record", () => {
    window.sessionStorage.setItem(TRACKED_OPERATION_KEY, "{not-json");
    mockGetUpgradeStatus.mockResolvedValue(status());

    renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("does not overlap a pending poll and aborts it on cleanup", async () => {
    vi.useFakeTimers();
    const request = deferred<ReturnType<typeof status>>();
    mockGetUpgradeStatus.mockReturnValue(request.promise);

    const hook = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());
    expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(120_000));
    expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1);
    const signal = mockGetUpgradeStatus.mock.calls[0]?.[1]?.signal;

    hook.unmount();
    expect(signal?.aborted).toBe(true);
  });

  it("keeps operation status pending until the first authoritative response", async () => {
    const request = deferred<ReturnType<typeof status>>();
    mockGetUpgradeStatus.mockReturnValue(request.promise);

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    expect(result.current.operationStatusPending).toBe(true);
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));

    await act(async () => {
      request.resolve(status());
      await request.promise;
    });

    expect(result.current.operationStatusPending).toBe(false);
  });

  it("restarts polling when the authenticated session generation changes", async () => {
    const previousRequest = deferred<ReturnType<typeof status>>();
    const onPollError = vi.fn();
    mockGetUpgradeStatus.mockReturnValueOnce(previousRequest.promise).mockResolvedValue(status());

    const { rerender, result } = renderHook(
      ({ authSessionIdentity }: { authSessionIdentity: string }) =>
        useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0", onPollError }, authSessionIdentity),
      { initialProps: { authSessionIdentity: AUTH_SESSION_IDENTITY } },
    );

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    const previousSignal = mockGetUpgradeStatus.mock.calls[0]?.[1]?.signal;
    const abortSpy = vi.spyOn(AbortController.prototype, "abort").mockImplementation(() => undefined);

    rerender({ authSessionIdentity: "operator-a:2" });

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(2));
    expect(abortSpy).toHaveBeenCalled();
    expect(previousSignal?.aborted).toBe(false);
    await waitFor(() => expect(result.current.operationStatusPending).toBe(false));

    await act(async () => {
      previousRequest.reject(new Error("previous session expired"));
      await Promise.resolve();
    });

    expect(onPollError).not.toHaveBeenCalled();
    abortSpy.mockRestore();
  });

  it("starts the exact target and tracks the returned operation", async () => {
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockResolvedValue(
      create(TriggerUpgradeResponseSchema, {
        operation: activeOperation,
      }),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(mockTriggerUpgrade).toHaveBeenCalledWith({ targetVersion: "v1.3.0" }, { timeoutMs: 30_000 });
    expect(result.current.operation?.phase).toBe(UpgradePhase.PREFLIGHT);
    expect(JSON.parse(window.sessionStorage.getItem(TRACKED_OPERATION_KEY) ?? "{}")).toEqual({
      id: "operation-1",
      targetVersion: "v1.3.0",
    });
  });

  it("keeps an unknown phase locked until explicit host confirmation", async () => {
    vi.useFakeTimers();
    const unknownOperation = operation(UpgradePhase.UNSPECIFIED);
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, unknownOperation));
    mockTriggerUpgrade.mockResolvedValue(
      create(TriggerUpgradeResponseSchema, {
        operation: unknownOperation,
      }),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.operation?.phase).toBe(UpgradePhase.UNSPECIFIED);
    expect(isUpgradeActive(result.current.operation)).toBe(true);
    expect(result.current.triggerError).toBeNull();
    expect(JSON.parse(window.sessionStorage.getItem(TRACKED_OPERATION_KEY) ?? "{}")).toEqual({
      id: "operation-1",
      targetVersion: "v1.3.0",
    });

    await act(async () => vi.advanceTimersByTimeAsync(17_000));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.manualFallbackReady).toBe(true);
    expect(result.current.operation?.phase).toBe(UpgradePhase.UNSPECIFIED);

    act(() => result.current.useManualFallback());

    expect(result.current.reconciling).toBe(false);
    expect(result.current.operation).toBeUndefined();
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
    expect(JSON.parse(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY) ?? "{}")).toEqual({
      authSessionIdentity: AUTH_SESSION_IDENTITY,
      id: "operation-1",
      phase: UpgradePhase.UNSPECIFIED,
      revision: "100:0",
    });

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.operation).toBeUndefined();
  });

  it.each([UpgradePhase.FAILED, UpgradePhase.SUCCEEDED])(
    "surfaces terminal phase %s after manually unlocking an unknown operation",
    async (terminalPhase) => {
      vi.useFakeTimers();
      const unknownOperation = operation(UpgradePhase.UNSPECIFIED);
      const terminalOperation = operation(terminalPhase);
      mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, unknownOperation));
      mockTriggerUpgrade.mockResolvedValue(
        create(TriggerUpgradeResponseSchema, {
          operation: unknownOperation,
        }),
      );
      const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
      await act(async () => Promise.resolve());

      await act(async () => result.current.triggerUpgrade("v1.3.0"));
      await act(async () => vi.advanceTimersByTimeAsync(17_000));
      act(() => result.current.useManualFallback());

      mockGetUpgradeStatus.mockReset();
      mockGetUpgradeStatus.mockResolvedValue(status(true, terminalOperation));
      await act(async () => vi.advanceTimersByTimeAsync(2_000));

      expect(result.current.operation?.phase).toBe(terminalPhase);
      expect(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY)).toBeNull();
    },
  );

  it("accepts a terminal operation returned directly by the trigger RPC", async () => {
    const failedOperation = operation(UpgradePhase.FAILED, { message: "Preflight failed" });
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockResolvedValue(
      create(TriggerUpgradeResponseSchema, {
        operation: failedOperation,
      }),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(result.current.operation?.id).toBe("operation-1");
    expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED);
    expect(result.current.reconciling).toBe(false);
  });

  it("reconciles an ambiguous trigger rejection to the durable operation", async () => {
    const activeOperation = operation(UpgradePhase.PREFLIGHT);
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, activeOperation));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await waitFor(() => expect(result.current.operation?.id).toBe("operation-1"));

    expect(result.current.reconciling).toBe(false);
    expect(result.current.triggerError).toBeNull();
  });

  it("unlocks immediately when the server definitively rejects a stale target", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockRejectedValue(
      new ConnectError('target "v1.3.0" is no longer the eligible update', Code.FailedPrecondition),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(result.current.reconciling).toBe(false);
    expect(result.current.trackedTargetVersion).toBeUndefined();
    expect(result.current.triggerError).toContain("no longer the eligible update");
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("reconciles an already-existing operation instead of treating the rejection as safe to unlock", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockRejectedValue(new ConnectError("another upgrade is active", Code.AlreadyExists));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    await act(async () => result.current.triggerUpgrade("v1.3.0"));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.3.0");
  });

  it("reconciles an ambiguous trigger rejection to a completed upgrade", async () => {
    const succeededOperation = operation(UpgradePhase.SUCCEEDED, { message: "Upgrade complete" });
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, succeededOperation));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.SUCCEEDED));

    expect(result.current.operation?.id).toBe("operation-1");
    expect(result.current.reconciling).toBe(false);
    expect(result.current.triggerError).toBeNull();
  });

  it("reconciles an ambiguous trigger rejection to a newer same-target failure", async () => {
    const previousFailure = operation(UpgradePhase.FAILED, { id: "operation-old" });
    const failedOperation = operation(UpgradePhase.FAILED, {
      id: "operation-new",
      updatedAt: timestamp(101),
      recoveryCommand: "./run-fleet.sh --skip-build",
    });
    window.sessionStorage.setItem(
      ACKNOWLEDGED_OPERATION_KEY,
      JSON.stringify({
        authSessionIdentity: AUTH_SESSION_IDENTITY,
        id: previousFailure.id,
        phase: previousFailure.phase,
        revision: "100:0",
      }),
    );
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, previousFailure))
      .mockResolvedValue(status(true, failedOperation));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await waitFor(() => expect(result.current.operation?.id).toBe("operation-new"));

    expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED);
    expect(result.current.operation?.recoveryCommand).toBe("./run-fleet.sh --skip-build");
    expect(result.current.reconciling).toBe(false);
  });

  it("does not let a stale terminal failure resolve an ambiguous trigger", async () => {
    const staleFailure = operation(UpgradePhase.FAILED, {
      id: "operation-old",
      targetVersion: "v1.3.0",
    });
    window.sessionStorage.setItem(
      ACKNOWLEDGED_OPERATION_KEY,
      JSON.stringify({
        authSessionIdentity: AUTH_SESSION_IDENTITY,
        id: staleFailure.id,
        phase: staleFailure.phase,
        revision: "100:0",
      }),
    );
    mockGetUpgradeStatus.mockResolvedValue(status(true, staleFailure));
    mockTriggerUpgrade.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(2));

    expect(result.current.operation).toBeUndefined();
    expect(result.current.reconciling).toBe(true);
    expect(result.current.trackedTargetVersion).toBe("v1.3.0");
  });

  it("ends bounded reconciliation and preserves an actionable trigger error", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus.mockResolvedValue(status());
    mockTriggerUpgrade.mockRejectedValue(new Error("host did not confirm"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    expect(result.current.reconciling).toBe(true);

    await act(async () => vi.advanceTimersByTimeAsync(17_000));

    expect(result.current.reconciling).toBe(false);
    expect(result.current.triggerError).toContain("host did not confirm");
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("does not unlock an unknown trigger outcome while the executor is unreachable", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(false));
    mockTriggerUpgrade.mockRejectedValue(new Error("host did not confirm"));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());

    await act(async () => result.current.triggerUpgrade("v1.3.0"));
    await act(async () => vi.advanceTimersByTimeAsync(30_000));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.manualFallbackReady).toBe(true);
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).not.toBeNull();

    act(() => result.current.useManualFallback());

    expect(result.current.reconciling).toBe(false);
    expect(result.current.triggerError).toBeNull();
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("keeps a recovered operation ID locked until the unreachable host is explicitly confirmed", async () => {
    vi.useFakeTimers();
    window.sessionStorage.setItem(
      TRACKED_OPERATION_KEY,
      JSON.stringify({ id: "operation-1", targetVersion: "v1.3.0" }),
    );
    mockGetUpgradeStatus.mockResolvedValue(status(false));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    expect(result.current.reconciling).toBe(true);

    await act(async () => vi.advanceTimersByTimeAsync(17_000));

    expect(result.current.reconciling).toBe(true);
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.manualFallbackReady).toBe(true);
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).not.toBeNull();

    act(() => result.current.useManualFallback());

    expect(result.current.reconciling).toBe(false);
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("unlocks a recovered operation ID after an authoritative reachable miss", async () => {
    window.sessionStorage.setItem(
      TRACKED_OPERATION_KEY,
      JSON.stringify({ id: "operation-1", targetVersion: "v1.3.0" }),
    );
    mockGetUpgradeStatus.mockResolvedValue(status(true));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    expect(result.current.reconciling).toBe(true);

    await waitFor(() => expect(result.current.reconciling).toBe(false));

    expect(result.current.connectionLost).toBe(false);
    expect(result.current.manualFallbackReady).toBe(false);
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("keeps progress through disconnect and accepts the terminal success", async () => {
    vi.useFakeTimers();
    const activeOperation = operation(UpgradePhase.ACTIVATING);
    const succeededOperation = operation(UpgradePhase.SUCCEEDED, { message: "Upgrade complete" });
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, activeOperation))
      .mockRejectedValueOnce(new Error("Fleet restarting"))
      .mockResolvedValue(status(true, succeededOperation));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.operation?.phase).toBe(UpgradePhase.SUCCEEDED);
    expect(result.current.connectionLost).toBe(false);
  });

  it.each([
    ["an unreachable executor", status(false)],
    ["a reachable executor with no operation", status(true)],
  ])("offers explicit fallback when an active operation is lost by %s", async (_scenario, missingStatus) => {
    vi.useFakeTimers();
    const activeOperation = operation(UpgradePhase.ACTIVATING);
    mockGetUpgradeStatus.mockResolvedValueOnce(status(true, activeOperation)).mockResolvedValue(missingStatus);
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.reconciling).toBe(true);
    expect(result.current.connectionLost).toBe(true);
    expect(result.current.manualFallbackReady).toBe(false);

    await act(async () => vi.advanceTimersByTimeAsync(16_000));
    expect(result.current.manualFallbackReady).toBe(true);
    expect(result.current.operation?.phase).toBe(UpgradePhase.ACTIVATING);

    act(() => result.current.useManualFallback());
    expect(result.current.reconciling).toBe(false);
    expect(result.current.operation).toBeUndefined();
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("does not replay an untracked historical success", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.SUCCEEDED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.3.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    expect(result.current.operation).toBeUndefined();
  });

  it("requests one authoritative offer refresh for an untracked success revision", async () => {
    vi.useFakeTimers();
    const onUntrackedSuccess = vi.fn();
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.SUCCEEDED)));
    const { result } = renderHook(() =>
      useTestUpgradeOperation({
        enabled: true,
        currentVersion: "v1.2.0",
        onUntrackedSuccess,
      }),
    );

    await act(async () => Promise.resolve());
    expect(onUntrackedSuccess).toHaveBeenCalledTimes(1);
    expect(result.current.operation).toBeUndefined();

    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(onUntrackedSuccess).toHaveBeenCalledTimes(1);
  });

  it("keeps a tracked activation failure visible when Fleet reports its target version", async () => {
    window.sessionStorage.setItem(
      TRACKED_OPERATION_KEY,
      JSON.stringify({ id: "operation-1", targetVersion: "v1.3.0" }),
    );
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.3.0" }));

    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).not.toBeNull();
  });

  it("keeps an untracked activation failure visible when Fleet reports its target version", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.3.0" }));

    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));
  });

  it("suppresses a stale failure after manual recovery installed a newer release", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.4.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    expect(result.current.operation).toBeUndefined();
  });

  it("treats a stable release as newer than its failed release candidate", async () => {
    mockGetUpgradeStatus.mockResolvedValue(
      status(true, operation(UpgradePhase.FAILED, { targetVersion: "v1.3.0-rc.4" })),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.3.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    expect(result.current.operation).toBeUndefined();
  });

  it("retains a failed operation when the current version is not canonical", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "dev" }));

    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));
  });

  it("rechecks an unresolved failure as soon as the current version loads", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    const initialProps: { currentVersion?: string } = {};
    const { rerender, result } = renderHook(
      ({ currentVersion }: { currentVersion?: string }) => useTestUpgradeOperation({ enabled: true, currentVersion }),
      { initialProps },
    );

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    expect(result.current.operation).toBeUndefined();

    rerender({ currentVersion: "v1.2.0" });

    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));
  });

  it("shows an untracked failure after current-version loading definitively fails", async () => {
    const failedOperation = operation(UpgradePhase.FAILED, {
      hostLogPath: "/var/lib/proto-fleet-updater/logs/operation-1.log",
      recoveryCommand: "./run-fleet.sh --skip-build",
    });
    mockGetUpgradeStatus.mockResolvedValue(status(true, failedOperation));
    const { rerender, result } = renderHook(
      ({ currentVersionUnavailable }: { currentVersionUnavailable: boolean }) =>
        useTestUpgradeOperation({ enabled: true, currentVersionUnavailable }),
      { initialProps: { currentVersionUnavailable: false } },
    );

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    expect(result.current.operation).toBeUndefined();

    rerender({ currentVersionUnavailable: true });

    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));
    expect(result.current.operation?.recoveryCommand).toBe("./run-fleet.sh --skip-build");
    expect(result.current.operation?.hostLogPath).toContain("operation-1.log");
  });

  it("scopes an acknowledged terminal failure to the authenticated session", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    mockAcknowledgeUpgrade.mockRejectedValue(new ConnectError("host unreachable", Code.Unavailable));
    const firstSession = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(firstSession.result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    await act(async () => firstSession.result.current.acknowledgeOperation().catch(() => undefined));

    expect(firstSession.result.current.operation).toBeUndefined();
    const acknowledgedRecord = JSON.parse(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY) ?? "{}");
    expect(acknowledgedRecord).toEqual({
      authSessionIdentity: AUTH_SESSION_IDENTITY,
      id: "operation-1",
      phase: UpgradePhase.FAILED,
      revision: "100:0",
    });
    firstSession.unmount();

    const sameSession = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(2));
    expect(sameSession.result.current.operation).toBeUndefined();
    sameSession.unmount();

    const nextSession = renderHook(() =>
      useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }, "operator-a:2"),
    );
    await waitFor(() => expect(nextSession.result.current.operation?.phase).toBe(UpgradePhase.FAILED));
    expect(JSON.parse(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY) ?? "{}")).toEqual(acknowledgedRecord);
  });

  it("resurfaces acknowledged failure details when startup reconciliation advances the revision", async () => {
    const originalFailure = operation(UpgradePhase.FAILED, { recoveryCommand: "old recovery" });
    mockGetUpgradeStatus.mockResolvedValue(status(true, originalFailure));
    const firstSession = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(firstSession.result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    mockAcknowledgeUpgrade.mockResolvedValue(
      create(AcknowledgeUpgradeResponseSchema, {
        operation: operation(UpgradePhase.FAILED, { acknowledged: true, recoveryCommand: "old recovery" }),
      }),
    );
    await act(async () => firstSession.result.current.acknowledgeOperation());
    firstSession.unmount();

    mockGetUpgradeStatus.mockReset();
    mockGetUpgradeStatus.mockResolvedValue(
      status(
        true,
        operation(UpgradePhase.FAILED, {
          updatedAt: timestamp(101),
          recoveryCommand: "new recovery",
        }),
      ),
    );
    const reconciledSession = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(reconciledSession.result.current.operation?.recoveryCommand).toBe("new recovery"));
    expect(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY)).toBeNull();
  });

  it("records the dismissal durably on the host", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    mockAcknowledgeUpgrade.mockResolvedValue(
      create(AcknowledgeUpgradeResponseSchema, {
        operation: operation(UpgradePhase.FAILED, { acknowledged: true }),
      }),
    );
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    await act(async () => result.current.acknowledgeOperation());

    expect(mockAcknowledgeUpgrade).toHaveBeenCalledWith({ operationId: "operation-1" }, { timeoutMs: 15_000 });
    expect(result.current.operation).toBeUndefined();
  });

  it("treats a host that no longer reports the operation as already dismissed", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    mockAcknowledgeUpgrade.mockRejectedValue(new ConnectError("operation is not current", Code.NotFound));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    await act(async () => result.current.acknowledgeOperation());

    expect(result.current.operation).toBeUndefined();
  });

  it("surfaces an unrecorded host dismissal while keeping the local dismissal applied", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    mockAcknowledgeUpgrade.mockRejectedValue(new ConnectError("host unreachable", Code.Unavailable));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    let acknowledgeError: unknown;
    await act(async () => result.current.acknowledgeOperation().catch((error: unknown) => (acknowledgeError = error)));

    expect(acknowledgeError).toBeInstanceOf(ConnectError);
    expect(result.current.operation).toBeUndefined();
  });

  it("never surfaces an operation the host reports as acknowledged", async () => {
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED, { acknowledged: true })));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(1));
    expect(result.current.operation).toBeUndefined();
    expect(result.current.reconciling).toBe(false);
  });

  it("clears a displayed failure once another session dismisses it on the host", async () => {
    vi.useFakeTimers();
    mockGetUpgradeStatus
      .mockResolvedValueOnce(status(true, operation(UpgradePhase.FAILED)))
      .mockResolvedValue(status(true, operation(UpgradePhase.FAILED, { acknowledged: true })));

    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await act(async () => Promise.resolve());
    expect(result.current.operation?.phase).toBe(UpgradePhase.FAILED);

    await act(async () => vi.advanceTimersByTimeAsync(60_000));

    expect(result.current.operation).toBeUndefined();
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
  });

  it("forwards poll errors for page-level auth handling", async () => {
    const onPollError = vi.fn();
    const pollError = new Error("permission revoked");
    mockGetUpgradeStatus.mockRejectedValue(pollError);

    renderHook(() =>
      useTestUpgradeOperation({
        enabled: true,
        currentVersion: "v1.2.0",
        onPollError,
      }),
    );

    await waitFor(() => expect(onPollError).toHaveBeenCalledWith(pollError));
  });
});
