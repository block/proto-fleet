import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { GetUpdateStatusResponseSchema, ReleaseInfoSchema } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { useAvailableUpdate } from "@/protoFleet/features/updates/api/useAvailableUpdate";

const authMock = vi.hoisted(() => ({
  permissions: ["instance:update", "fleet:read"],
  isAuthenticated: true,
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => authMock.permissions.includes(permission),
  useIsAuthenticated: () => authMock.isAuthenticated,
  useFleetStore: {
    getState: () => ({
      auth: {
        permissions: authMock.permissions,
        isAuthenticated: authMock.isAuthenticated,
      },
    }),
  },
}));

vi.mock("@/protoFleet/api/clients", () => ({
  instanceUpdateClient: {
    getUpdateStatus: vi.fn(),
  },
}));

const mockGetUpdateStatus = vi.mocked(instanceUpdateClient.getUpdateStatus);
const UPDATE_STATUS_POLL_INTERVAL_MS = 15 * 60 * 1000;

type StatusOverrides = Partial<Pick<GetUpdateStatusResponse, "statusAvailable" | "updateAvailable">>;

const buildStatus = (version: string, overrides: StatusOverrides = {}): GetUpdateStatusResponse =>
  create(GetUpdateStatusResponseSchema, {
    currentVersion: "v1.2.0",
    statusAvailable: true,
    updateAvailable: true,
    // The shell indicator must not depend on or retain this command.
    installCommand: "",
    latestEligible: create(ReleaseInfoSchema, { version }),
    ...overrides,
  });

describe("useAvailableUpdate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authMock.permissions = ["instance:update", "fleet:read"];
    authMock.isAuthenticated = true;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns the eligible version without depending on an install command", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus("v1.3.0"));

    const { result } = renderHook(() => useAvailableUpdate());

    await waitFor(() => expect(result.current).toBe("v1.3.0"));
  });

  it("stays hidden when polling is disabled or permission is absent", async () => {
    const { result, rerender } = renderHook(({ enabled }) => useAvailableUpdate({ enabled }), {
      initialProps: { enabled: false },
    });

    expect(result.current).toBeNull();
    expect(mockGetUpdateStatus).not.toHaveBeenCalled();

    authMock.permissions = ["fleet:read"];
    rerender({ enabled: true });
    await act(async () => Promise.resolve());

    expect(result.current).toBeNull();
    expect(mockGetUpdateStatus).not.toHaveBeenCalled();
  });

  it("clears the indicator when status is unavailable or current", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus("v1.3.0", { statusAvailable: false, updateAvailable: false }));

    const { result } = renderHook(() => useAvailableUpdate());

    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));
    expect(result.current).toBeNull();
  });

  it("clears a visible indicator after a successful poll reports the instance is current", async () => {
    vi.useFakeTimers();
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus("v1.3.0"))
      .mockResolvedValueOnce(buildStatus("v1.3.0", { updateAvailable: false }));

    const { result } = renderHook(() => useAvailableUpdate());
    await act(async () => vi.advanceTimersByTimeAsync(0));

    expect(result.current).toBe("v1.3.0");
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(UPDATE_STATUS_POLL_INTERVAL_MS));

    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);
    expect(result.current).toBeNull();
  });

  it("pauses scheduled polling while disabled and fetches immediately when re-enabled", async () => {
    vi.useFakeTimers();
    mockGetUpdateStatus.mockResolvedValue(buildStatus("v1.3.0"));

    const { result, rerender } = renderHook(({ enabled }) => useAvailableUpdate({ enabled }), {
      initialProps: { enabled: true },
    });
    await act(async () => vi.advanceTimersByTimeAsync(0));

    expect(result.current).toBe("v1.3.0");
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    rerender({ enabled: false });
    expect(result.current).toBeNull();

    await act(async () => vi.advanceTimersByTimeAsync(UPDATE_STATUS_POLL_INTERVAL_MS * 2));
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    rerender({ enabled: true });
    await act(async () => vi.advanceTimersByTimeAsync(0));

    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);
  });

  it("hides a cached indicator when a later status request fails", async () => {
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus("v1.3.0"));

    const { result, rerender } = renderHook(({ enabled }) => useAvailableUpdate({ enabled }), {
      initialProps: { enabled: true },
    });
    await waitFor(() => expect(result.current).toBe("v1.3.0"));

    rerender({ enabled: false });
    mockGetUpdateStatus.mockRejectedValueOnce(new Error("status unavailable"));
    rerender({ enabled: true });

    await waitFor(() => expect(result.current).toBeNull());
  });

  it("ignores an older request that resolves after a newer poll", async () => {
    let resolveFirstRequest: (status: GetUpdateStatusResponse) => void = () => undefined;
    mockGetUpdateStatus
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveFirstRequest = resolve;
          }),
      )
      .mockResolvedValueOnce(buildStatus("v1.4.0"));

    const { result, rerender } = renderHook(({ enabled }) => useAvailableUpdate({ enabled }), {
      initialProps: { enabled: true },
    });
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));

    rerender({ enabled: false });
    await act(async () => Promise.resolve());
    rerender({ enabled: true });
    await waitFor(() => expect(result.current).toBe("v1.4.0"));

    await act(async () => resolveFirstRequest(buildStatus("v1.3.0")));

    expect(result.current).toBe("v1.4.0");
  });
});
