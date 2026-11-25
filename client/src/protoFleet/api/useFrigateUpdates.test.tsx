import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  FrigateUpdateState,
  type UpdateJob,
  UpdateJobSchema,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import {
  FRIGATE_UPDATE_ACTIVE_POLL_MS,
  FRIGATE_UPDATE_IDLE_POLL_MS,
  isFrigateUpdateActive,
  useFrigateUpdateActions,
  useFrigateUpdates,
} from "@/protoFleet/api/useFrigateUpdates";

const mocks = vi.hoisted(() => ({
  listUpdates: vi.fn(),
  getSupport: vi.fn(),
  startUpdate: vi.fn(),
  retryUpdate: vi.fn(),
  cancelUpdate: vi.fn(),
  handleAuthErrors: vi.fn(({ onError, error }: { onError?: (error: unknown) => void; error: unknown }) =>
    onError?.(error),
  ),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  frigateUpdateClient: {
    listUpdates: mocks.listUpdates,
    getSupport: mocks.getSupport,
    startUpdate: mocks.startUpdate,
    retryUpdate: mocks.retryUpdate,
    cancelUpdate: mocks.cancelUpdate,
  },
}));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mocks.handleAuthErrors }),
}));

const job = (state: FrigateUpdateState, jobId = "job-1"): UpdateJob => create(UpdateJobSchema, { jobId, state });

function setVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", { configurable: true, value: state });
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listUpdates.mockReset();
  vi.useFakeTimers({ shouldAdvanceTime: false });
  setVisibility("visible");
});
afterEach(() => {
  vi.useRealTimers();
});

describe("isFrigateUpdateActive", () => {
  it.each([
    [FrigateUpdateState.STAGING, true],
    [FrigateUpdateState.STAGED, true],
    [FrigateUpdateState.STARTING, true],
    [FrigateUpdateState.RUNNING, true],
    [FrigateUpdateState.SUCCEEDED, false],
    [FrigateUpdateState.FAILED, false],
    [FrigateUpdateState.CANCELLED, false],
    [FrigateUpdateState.VERIFIED, false],
    [FrigateUpdateState.ABANDONED, false],
  ])("state %s active=%s", (state, active) => {
    expect(isFrigateUpdateActive(job(state))).toBe(active);
  });

  it("is false without a job", () => {
    expect(isFrigateUpdateActive(undefined)).toBe(false);
  });
});

describe("useFrigateUpdates", () => {
  it("loads the container's jobs newest first", async () => {
    mocks.listUpdates.mockResolvedValue({
      jobs: [job(FrigateUpdateState.SUCCEEDED, "new"), job(FrigateUpdateState.FAILED, "old")],
    });
    const { result } = renderHook(() => useFrigateUpdates("ct1", true));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.loading).toBe(false);

    expect(mocks.listUpdates).toHaveBeenCalledWith({ deviceIdentifier: "ct1", pageSize: 10 }, expect.any(Object));
    expect(result.current.latest?.jobId).toBe("new");
    expect(result.current.jobs).toHaveLength(2);
  });

  it("does not poll when disabled", async () => {
    renderHook(() => useFrigateUpdates("ct1", false));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FRIGATE_UPDATE_IDLE_POLL_MS * 2);
    });
    expect(mocks.listUpdates).not.toHaveBeenCalled();
  });

  it("polls every 5 seconds while an update is active", async () => {
    mocks.listUpdates.mockResolvedValue({ jobs: [job(FrigateUpdateState.RUNNING)] });
    renderHook(() => useFrigateUpdates("ct1", true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    // The first response switches the hook to the active interval and restarts the poll.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    mocks.listUpdates.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FRIGATE_UPDATE_ACTIVE_POLL_MS * 3);
    });

    expect(mocks.listUpdates).toHaveBeenCalledTimes(3);
  });

  it("polls every 30 seconds when idle", async () => {
    mocks.listUpdates.mockResolvedValue({ jobs: [job(FrigateUpdateState.SUCCEEDED)] });
    renderHook(() => useFrigateUpdates("ct1", true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    mocks.listUpdates.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FRIGATE_UPDATE_IDLE_POLL_MS - 1);
    });
    expect(mocks.listUpdates).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(mocks.listUpdates).toHaveBeenCalledTimes(1);
  });

  it("stops polling while the page is hidden and reads again when it is shown", async () => {
    mocks.listUpdates.mockResolvedValue({ jobs: [job(FrigateUpdateState.SUCCEEDED)] });
    renderHook(() => useFrigateUpdates("ct1", true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    act(() => setVisibility("hidden"));
    mocks.listUpdates.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FRIGATE_UPDATE_IDLE_POLL_MS * 3);
    });
    expect(mocks.listUpdates).not.toHaveBeenCalled();

    act(() => setVisibility("visible"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(mocks.listUpdates).toHaveBeenCalledTimes(1);
  });

  it("keeps the last jobs on screen when a later read fails", async () => {
    mocks.listUpdates.mockResolvedValueOnce({ jobs: [job(FrigateUpdateState.RUNNING)] });
    const { result } = renderHook(() => useFrigateUpdates("ct1", true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.latest?.state).toBe(FrigateUpdateState.RUNNING);

    mocks.listUpdates.mockRejectedValue(new ConnectError("controller unreachable", Code.Unavailable));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FRIGATE_UPDATE_ACTIVE_POLL_MS);
    });

    expect(result.current.error).toBe("controller unreachable");
    expect(result.current.latest?.state).toBe(FrigateUpdateState.RUNNING);
  });

  it("treats a permission denial as no updates rather than an error", async () => {
    mocks.listUpdates.mockRejectedValue(new ConnectError("denied", Code.PermissionDenied));
    const { result } = renderHook(() => useFrigateUpdates("ct1", true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current.error).toBeUndefined();
    expect(result.current.jobs).toEqual([]);
    expect(result.current.loading).toBe(false);
  });

  it("reads again on refresh", async () => {
    mocks.listUpdates.mockResolvedValue({ jobs: [] });
    const { result } = renderHook(() => useFrigateUpdates("ct1", true));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    mocks.listUpdates.mockClear();

    act(() => result.current.refresh());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(mocks.listUpdates).toHaveBeenCalledTimes(1);
  });
});

describe("useFrigateUpdateActions", () => {
  it("sends the caller's idempotency key with start, retry and cancel", async () => {
    mocks.startUpdate.mockResolvedValue({ job: job(FrigateUpdateState.STAGING) });
    mocks.retryUpdate.mockResolvedValue({ job: job(FrigateUpdateState.RUNNING) });
    mocks.cancelUpdate.mockResolvedValue({ job: job(FrigateUpdateState.CANCELLED) });
    const { result } = renderHook(() => useFrigateUpdateActions());

    await result.current.startUpdate({ deviceIdentifier: "ct1", packageId: "pkg", idempotencyKey: "k1" });
    await result.current.retryUpdate("job-1", "k2");
    await result.current.cancelUpdate("job-1", "k3");

    expect(mocks.startUpdate.mock.calls[0][0]).toEqual({
      deviceIdentifier: "ct1",
      packageId: "pkg",
      stageOnly: false,
      idempotencyKey: "k1",
    });
    expect(mocks.retryUpdate.mock.calls[0][0]).toEqual({ jobId: "job-1", idempotencyKey: "k2" });
    expect(mocks.cancelUpdate.mock.calls[0][0]).toEqual({ jobId: "job-1", idempotencyKey: "k3" });
  });

  it("asks for support with and without a package", async () => {
    mocks.getSupport.mockResolvedValue({ eligible: true, instances: [] });
    const { result } = renderHook(() => useFrigateUpdateActions());

    await result.current.getSupport("ct1");
    await result.current.getSupport("ct1", "pkg");

    expect(mocks.getSupport.mock.calls[0][0]).toEqual({ deviceIdentifier: "ct1", packageId: "" });
    expect(mocks.getSupport.mock.calls[1][0]).toEqual({ deviceIdentifier: "ct1", packageId: "pkg" });
  });
});
