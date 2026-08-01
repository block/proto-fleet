import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import {
  GetUpdateStatusResponseSchema,
  ReleaseChannel,
  ReleaseInfoSchema,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { UPDATE_STATUS_INVALIDATED_EVENT } from "@/protoFleet/api/updateStatusEvents";
import { useUpdateStatus } from "@/protoFleet/features/updates/api/useUpdateStatus";

const authMock = vi.hoisted(() => ({
  permissions: ["instance:update", "fleet:read"],
  isAuthenticated: true,
  sessionExpiry: new Date(1_000),
  setPermissions: vi.fn<(permissions: string[]) => void>(),
  handleAuthErrors: vi.fn(),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (permission: string) => authMock.permissions.includes(permission),
  useIsAuthenticated: () => authMock.isAuthenticated,
  usePermissions: () => authMock.permissions,
  useSessionExpiry: () => authMock.sessionExpiry,
  useSetPermissions: () => authMock.setPermissions,
  useAuthErrors: () => ({ handleAuthErrors: authMock.handleAuthErrors }),
  useFleetStore: {
    getState: () => ({
      auth: {
        permissions: authMock.permissions,
        isAuthenticated: authMock.isAuthenticated,
        sessionExpiry: authMock.sessionExpiry,
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

const buildStatus = (version: string, channel = ReleaseChannel.STABLE): GetUpdateStatusResponse =>
  create(GetUpdateStatusResponseSchema, {
    currentVersion: "v1.2.0",
    channel,
    statusAvailable: true,
    updateAvailable: true,
    installCommand: `curl -fsSL https://fleet.example.com/install.sh | sh -s -- ${version}`,
    latestEligible: create(ReleaseInfoSchema, {
      version,
      releaseNotesUrl: `https://github.com/block/proto-fleet/releases/tag/${version}`,
      prerelease: channel === ReleaseChannel.STABLE_AND_RC,
    }),
  });

const createDeferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
};

describe("useUpdateStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authMock.permissions = ["instance:update", "fleet:read"];
    authMock.isAuthenticated = true;
    authMock.sessionExpiry = new Date(1_000);
    authMock.setPermissions.mockImplementation((permissions) => {
      authMock.permissions = permissions;
    });
  });

  it("clears status and stale client permission when the poll is denied", async () => {
    const permissionError = new ConnectError("permission revoked", Code.PermissionDenied);
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus("v1.3.0")).mockRejectedValueOnce(permissionError);

    const { result } = renderHook(() => useUpdateStatus());
    await waitFor(() => expect(result.current.status?.latestEligible?.version).toBe("v1.3.0"));

    act(() => {
      window.dispatchEvent(new CustomEvent(UPDATE_STATUS_INVALIDATED_EVENT));
    });

    await waitFor(() => expect(result.current.status).toBeNull());
    expect(authMock.setPermissions).toHaveBeenCalledWith(["fleet:read"]);
    expect(authMock.handleAuthErrors).toHaveBeenCalledWith({ error: permissionError });
  });

  it("clears privileged status and uses the shared auth path when the session expires", async () => {
    const sessionError = new ConnectError("session expired", Code.Unauthenticated);
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus("v1.3.0")).mockRejectedValueOnce(sessionError);

    const { result } = renderHook(() => useUpdateStatus());
    await waitFor(() => expect(result.current.status?.latestEligible?.version).toBe("v1.3.0"));

    act(() => {
      window.dispatchEvent(new CustomEvent(UPDATE_STATUS_INVALIDATED_EVENT));
    });

    await waitFor(() => expect(result.current.status).toBeNull());
    expect(authMock.handleAuthErrors).toHaveBeenCalledWith({ error: sessionError });
    expect(authMock.setPermissions).not.toHaveBeenCalled();
  });

  it("clears cached status as soon as update permission is removed", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus("v1.3.0"));

    const { result, rerender } = renderHook(() => useUpdateStatus());
    await waitFor(() => expect(result.current.status?.latestEligible?.version).toBe("v1.3.0"));

    authMock.permissions = ["fleet:read"];
    rerender();

    expect(result.current.status).toBeNull();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);
  });

  it("hides the old offer while a channel-change refresh is in flight", async () => {
    const refresh = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus("v1.4.0-rc.1", ReleaseChannel.STABLE_AND_RC))
      .mockReturnValueOnce(refresh.promise);

    const { result } = renderHook(() => useUpdateStatus());
    await waitFor(() => expect(result.current.status?.latestEligible?.version).toBe("v1.4.0-rc.1"));

    act(() => {
      window.dispatchEvent(new CustomEvent(UPDATE_STATUS_INVALIDATED_EVENT));
    });

    expect(result.current.status).toBeNull();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);

    await act(async () => {
      refresh.resolve(buildStatus("v1.3.0"));
      await refresh.promise;
    });
    expect(result.current.status?.latestEligible?.version).toBe("v1.3.0");
  });

  it("ignores a response that arrives from a replaced auth session", async () => {
    const oldRequest = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockReturnValueOnce(oldRequest.promise).mockResolvedValueOnce(buildStatus("v1.3.0"));

    const { result, rerender } = renderHook(() => useUpdateStatus());
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));

    authMock.sessionExpiry = new Date(2_000);
    rerender();
    await waitFor(() => expect(result.current.status?.latestEligible?.version).toBe("v1.3.0"));

    await act(async () => {
      oldRequest.resolve(buildStatus("v1.4.0-rc.1", ReleaseChannel.STABLE_AND_RC));
      await oldRequest.promise;
    });
    expect(result.current.status?.latestEligible?.version).toBe("v1.3.0");
  });
});
