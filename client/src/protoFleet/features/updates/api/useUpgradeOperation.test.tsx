import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import {
  GetUpgradeStatusResponseSchema,
  TriggerUpgradeResponseSchema,
  type UpgradeOperation,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { isUpgradeActive, useUpgradeOperation } from "@/protoFleet/features/updates/api/useUpgradeOperation";

vi.mock("@/protoFleet/api/clients", () => ({
  instanceUpdateClient: {
    getUpgradeStatus: vi.fn(),
    triggerUpgrade: vi.fn(),
  },
}));

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

const operation = (phase: UpgradePhase, overrides?: MessageOverrides<UpgradeOperation>) =>
  create(UpgradeOperationSchema, {
    id: "operation-1",
    targetVersion: "v1.3.0",
    phase,
    message: "Preparing upgrade",
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

    rerender({ authSessionIdentity: "operator-a:2" });

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalledTimes(2));
    expect(previousSignal?.aborted).toBe(true);
    await waitFor(() => expect(result.current.operationStatusPending).toBe(false));

    await act(async () => {
      previousRequest.reject(new Error("previous session expired"));
      await Promise.resolve();
    });

    expect(onPollError).not.toHaveBeenCalled();
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
    expect(JSON.parse(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY) ?? "{}").id).toBe("operation-1");

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    expect(result.current.operation).toBeUndefined();
  });

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

  it("does not let a stale terminal failure resolve an ambiguous trigger", async () => {
    const staleFailure = operation(UpgradePhase.FAILED, {
      id: "operation-old",
      targetVersion: "v1.3.0",
    });
    mockGetUpgradeStatus.mockResolvedValueOnce(status()).mockResolvedValue(status(true, staleFailure));
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

  it("suppresses a stale failure after manual recovery installed its target", async () => {
    window.sessionStorage.setItem(
      TRACKED_OPERATION_KEY,
      JSON.stringify({ id: "operation-1", targetVersion: "v1.3.0" }),
    );
    mockGetUpgradeStatus.mockResolvedValue(status(true, operation(UpgradePhase.FAILED)));
    const { result } = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.3.0" }));

    await waitFor(() => expect(mockGetUpgradeStatus).toHaveBeenCalled());
    expect(result.current.operation).toBeUndefined();
    expect(window.sessionStorage.getItem(TRACKED_OPERATION_KEY)).toBeNull();
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
    const firstSession = renderHook(() => useTestUpgradeOperation({ enabled: true, currentVersion: "v1.2.0" }));
    await waitFor(() => expect(firstSession.result.current.operation?.phase).toBe(UpgradePhase.FAILED));

    act(() => firstSession.result.current.acknowledgeOperation());

    expect(firstSession.result.current.operation).toBeUndefined();
    const acknowledgedRecord = JSON.parse(window.sessionStorage.getItem(ACKNOWLEDGED_OPERATION_KEY) ?? "{}");
    expect(acknowledgedRecord).toEqual({
      authSessionIdentity: AUTH_SESSION_IDENTITY,
      id: "operation-1",
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
