import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import Updates from "./Updates";
import { instanceUpdateClient } from "@/protoFleet/api/clients";
import type {
  GetUpdateStatusResponse,
  ReleaseInfo,
  SetReleaseChannelResponse,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import {
  GetUpdateStatusResponseSchema,
  ReleaseChannel,
  ReleaseInfoSchema,
  SetReleaseChannelResponseSchema,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

const permissionsMock = vi.hoisted(() => ({
  current: ["instance:update", "fleet:read"],
  isAuthenticated: true,
  sessionExpiry: new Date(1_000),
  setPermissions: vi.fn<(permissions: string[]) => void>(),
}));
const authErrorsMock = vi.hoisted(() => ({
  handleAuthErrors: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate" data-to={to} />,
}));

vi.mock("@/protoFleet/store", () => {
  // Stable identity, mirroring the real hook's memoization: the page's fetch
  // effect depends on handleAuthErrors.
  authErrorsMock.handleAuthErrors.mockImplementation(
    ({ error, onError }: { error: unknown; onError?: (error: unknown) => void }) => onError?.(error),
  );
  return {
    useHasPermission: vi.fn((permission: string) => permissionsMock.current.includes(permission)),
    usePermissions: () => permissionsMock.current,
    useSetPermissions: () => permissionsMock.setPermissions,
    useAuthErrors: () => authErrorsMock,
    useFleetStore: {
      getState: () => ({
        auth: {
          isAuthenticated: permissionsMock.isAuthenticated,
          permissions: permissionsMock.current,
          sessionExpiry: permissionsMock.sessionExpiry,
        },
      }),
    },
  };
});

vi.mock("@/protoFleet/api/clients", () => ({
  instanceUpdateClient: {
    getUpdateStatus: vi.fn(),
    setReleaseChannel: vi.fn(),
  },
}));

vi.mock("@/shared/utils/utility", () => ({
  copyToClipboard: vi.fn(),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: {
    success: "success",
    error: "error",
  },
}));

const INSTALL_COMMAND = "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.3.0";
const RELEASE_NOTES_URL = "https://github.com/block/proto-fleet/releases/tag/v1.3.0";
const DISMISSED_UPDATE_TAG_KEY = "dismissedUpdateTag";
const SET_CHANNEL_RESPONSE = create(SetReleaseChannelResponseSchema);

const buildReleaseInfo = (overrides?: Partial<ReleaseInfo>): ReleaseInfo =>
  create(ReleaseInfoSchema, {
    version: "v1.3.0",
    releaseNotesUrl: RELEASE_NOTES_URL,
    prerelease: false,
    ...overrides,
  });

const buildStatus = (overrides?: Partial<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
  create(GetUpdateStatusResponseSchema, {
    currentVersion: "v1.2.0",
    channel: ReleaseChannel.STABLE,
    statusAvailable: true,
    updateAvailable: true,
    installCommand: INSTALL_COMMAND,
    latestEligible: buildReleaseInfo(),
    ...overrides,
  });

const mockUseHasPermission = vi.mocked(useHasPermission);
const mockGetUpdateStatus = vi.mocked(instanceUpdateClient.getUpdateStatus);
const mockSetReleaseChannel = vi.mocked(instanceUpdateClient.setReleaseChannel);
const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

const RC_CHECKBOX_NAME = "Include release candidates";
const RELEASE_CHANNEL_SAVE_TIMEOUT_MS = 30_000;
const PERMISSION_REVOKED_MESSAGE = "You no longer have permission to update this instance";

const createDeferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
};

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  permissionsMock.current = ["instance:update", "fleet:read"];
  permissionsMock.isAuthenticated = true;
  permissionsMock.sessionExpiry = new Date(1_000);
  permissionsMock.setPermissions.mockImplementation((permissions) => {
    permissionsMock.current = permissions;
  });
  mockUseHasPermission.mockImplementation((permission) => permissionsMock.current.includes(permission));
});

describe("Updates", () => {
  it("renders the current version, latest release, notes link, and copy control regardless of callout dismissal", async () => {
    // The nav callout's dismissal must not hide the release on this page.
    localStorage.setItem(DISMISSED_UPDATE_TAG_KEY, "v1.3.0");
    mockGetUpdateStatus.mockResolvedValue(buildStatus());

    const { findByText, getByText, getByRole } = render(<Updates />);

    expect(await findByText("v1.2.0")).toBeInTheDocument();
    expect(getByText("v1.3.0")).toBeInTheDocument();
    const link = getByRole("link", { name: "Release notes" });
    expect(link).toHaveAttribute("href", RELEASE_NOTES_URL);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(getByText(INSTALL_COMMAND)).toBeInTheDocument();
    expect(getByRole("button", { name: "Copy install command" })).toBeInTheDocument();
  });

  it("omits the release notes link when the server provides no URL", async () => {
    // The server blanks non-https notes URLs; the release still renders.
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({
        latestEligible: buildReleaseInfo({
          version: "v1.3.0",
          releaseNotesUrl: "",
          prerelease: false,
        }),
      }),
    );

    const { findByText, queryByRole } = render(<Updates />);

    expect(await findByText("v1.3.0")).toBeInTheDocument();
    expect(queryByRole("link", { name: "Release notes" })).not.toBeInTheDocument();
  });

  it("copies the install command and shows a success toast", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus());
    mockCopyToClipboard.mockResolvedValue(undefined);

    const { findByRole } = render(<Updates />);
    fireEvent.click(await findByRole("button", { name: "Copy install command" }));

    expect(mockCopyToClipboard).toHaveBeenCalledWith(INSTALL_COMMAND);
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Install command copied to clipboard",
        status: "success",
      }),
    );
  });

  it("shows an error toast when copying the install command fails", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus());
    mockCopyToClipboard.mockRejectedValue(new Error("copy failed"));

    const { findByRole } = render(<Updates />);
    fireEvent.click(await findByRole("button", { name: "Copy install command" }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Failed to copy install command",
        status: "error",
      }),
    );
  });

  it("renders the up-to-date state when no update is available", async () => {
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({ updateAvailable: false, installCommand: "", latestEligible: undefined }),
    );

    const { findByText, queryByRole } = render(<Updates />);

    expect(await findByText("You're on the latest version")).toBeInTheDocument();
    expect(queryByRole("button", { name: "Copy install command" })).not.toBeInTheDocument();
  });

  it("renders an unavailable state when release discovery has not succeeded", async () => {
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({
        statusAvailable: false,
        updateAvailable: false,
        latestEligible: undefined,
        installCommand: "",
      }),
    );

    const { findByText, queryByText } = render(<Updates />);

    expect(await findByText("Update status unavailable")).toBeInTheDocument();
    expect(queryByText("You're on the latest version")).not.toBeInTheDocument();
  });

  it("renders the error state when the status RPC fails on load", async () => {
    mockGetUpdateStatus.mockRejectedValue(new Error("release registry unreachable"));

    const { findByText, getByText } = render(<Updates />);

    expect(await findByText("Unable to load update status")).toBeInTheDocument();
    expect(getByText("release registry unreachable")).toBeInTheDocument();
  });

  it("saves a channel change and toasts success", async () => {
    // The success path refetches; the second response carries the persisted
    // new channel.
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE_AND_RC }));
    mockSetReleaseChannel.mockResolvedValue(SET_CHANNEL_RESPONSE);

    const { findByRole, getByRole } = render(<Updates />);
    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(mockSetReleaseChannel).toHaveBeenCalledWith(
      { channel: ReleaseChannel.STABLE_AND_RC },
      { timeoutMs: RELEASE_CHANNEL_SAVE_TIMEOUT_MS },
    );
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Release channel updated",
        status: "success",
      }),
    );
    await waitFor(() => expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeChecked());
  });

  it("saves a switch back to stable when the checkbox is unchecked", async () => {
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE_AND_RC }))
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockResolvedValue(SET_CHANNEL_RESPONSE);

    const { findByRole, getByRole } = render(<Updates />);
    const checkbox = await findByRole("checkbox", { name: RC_CHECKBOX_NAME });
    expect(checkbox).toBeChecked();
    fireEvent.click(checkbox);

    expect(mockSetReleaseChannel).toHaveBeenCalledWith(
      { channel: ReleaseChannel.STABLE },
      { timeoutMs: RELEASE_CHANNEL_SAVE_TIMEOUT_MS },
    );
    await waitFor(() => expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked());
  });

  it("refetches the update status after a successful channel change", async () => {
    // Each channel offers a different eligible release; the page must not
    // keep showing the old channel's offer after the switch.
    const rcStatus = buildStatus({
      channel: ReleaseChannel.STABLE_AND_RC,
      installCommand: "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.4.0-rc.1",
      latestEligible: buildReleaseInfo({
        version: "v1.4.0-rc.1",
        releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
        prerelease: true,
      }),
    });
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(rcStatus);
    mockSetReleaseChannel.mockResolvedValue(SET_CHANNEL_RESPONSE);

    const { findByText, findByRole } = render(<Updates />);
    expect(await findByText("v1.3.0")).toBeInTheDocument();

    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(await findByText("v1.4.0-rc.1")).toBeInTheDocument();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);
  });

  it("ignores an older status request that resolves after the latest request", async () => {
    const staleRequest = createDeferred<GetUpdateStatusResponse>();
    const latestRequest = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockReturnValueOnce(staleRequest.promise).mockReturnValueOnce(latestRequest.promise);

    const { rerender, findByText, getByRole, queryByText } = render(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));

    // Losing and regaining permission replaces the in-flight request just as
    // another status refresh would; the older response must not win later.
    mockUseHasPermission.mockReturnValue(false);
    rerender(<Updates />);
    mockUseHasPermission.mockReturnValue(true);
    rerender(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));

    await act(async () => {
      latestRequest.resolve(buildStatus({ channel: ReleaseChannel.STABLE }));
      await latestRequest.promise;
    });
    expect(await findByText("v1.3.0")).toBeInTheDocument();
    expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked();

    await act(async () => {
      staleRequest.resolve(
        buildStatus({
          channel: ReleaseChannel.STABLE_AND_RC,
          latestEligible: buildReleaseInfo({
            version: "v1.4.0-rc.1",
            releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
            prerelease: true,
          }),
        }),
      );
      await staleRequest.promise;
    });
    expect(queryByText("v1.4.0-rc.1")).not.toBeInTheDocument();
    expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked();
  });

  it("ignores an older status request that rejects after the latest request succeeds", async () => {
    const staleRequest = createDeferred<GetUpdateStatusResponse>();
    const latestRequest = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockReturnValueOnce(staleRequest.promise).mockReturnValueOnce(latestRequest.promise);

    const { rerender, findByText, getByRole, queryByText } = render(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));

    mockUseHasPermission.mockReturnValue(false);
    rerender(<Updates />);
    mockUseHasPermission.mockReturnValue(true);
    rerender(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));

    await act(async () => {
      latestRequest.resolve(buildStatus({ channel: ReleaseChannel.STABLE }));
      await latestRequest.promise;
    });
    expect(await findByText("v1.3.0")).toBeInTheDocument();

    await act(async () => {
      staleRequest.reject(new Error("stale registry failure"));
      await staleRequest.promise.catch(() => undefined);
    });
    expect(queryByText("Unable to load update status")).not.toBeInTheDocument();
    expect(queryByText("stale registry failure")).not.toBeInTheDocument();
    expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked();
  });

  it("preserves global auth handling when a status request rejects after unmount", async () => {
    const request = createDeferred<GetUpdateStatusResponse>();
    const sessionError = new ConnectError("session expired", Code.Unauthenticated);
    mockGetUpdateStatus.mockReturnValue(request.promise);

    const page = render(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));
    page.unmount();

    await act(async () => {
      request.reject(sessionError);
      await request.promise.catch(() => undefined);
    });

    expect(authErrorsMock.handleAuthErrors).toHaveBeenCalledWith(
      expect.objectContaining({
        error: sessionError,
      }),
    );
    expect(permissionsMock.setPermissions).not.toHaveBeenCalled();
    expect(mockPushToast).not.toHaveBeenCalled();
  });

  it("invalidates revoked permission without toasting when a status request outlives the page", async () => {
    const request = createDeferred<GetUpdateStatusResponse>();
    const permissionError = new ConnectError("permission revoked", Code.PermissionDenied);
    mockGetUpdateStatus.mockReturnValue(request.promise);

    const page = render(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));
    page.unmount();

    await act(async () => {
      request.reject(permissionError);
      await request.promise.catch(() => undefined);
    });

    expect(authErrorsMock.handleAuthErrors).toHaveBeenCalledWith(
      expect.objectContaining({
        error: permissionError,
      }),
    );
    expect(permissionsMock.setPermissions).toHaveBeenCalledWith(["fleet:read"]);
    expect(mockPushToast).not.toHaveBeenCalled();
  });

  it("does not apply a delayed status auth failure to a replacement session", async () => {
    const request = createDeferred<GetUpdateStatusResponse>();
    const sessionError = new ConnectError("old session expired", Code.Unauthenticated);
    mockGetUpdateStatus.mockReturnValue(request.promise);

    const page = render(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));
    page.unmount();
    permissionsMock.sessionExpiry = new Date(2_000);

    await act(async () => {
      request.reject(sessionError);
      await request.promise.catch(() => undefined);
    });

    expect(authErrorsMock.handleAuthErrors).not.toHaveBeenCalled();
    expect(permissionsMock.setPermissions).not.toHaveBeenCalled();
    expect(mockPushToast).not.toHaveBeenCalled();
  });

  it("disables channel and copy controls throughout the save and refetch", async () => {
    const save = createDeferred<SetReleaseChannelResponse>();
    const refetch = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockGetUpdateStatus.mockReturnValueOnce(refetch.promise);
    mockSetReleaseChannel.mockReturnValue(save.promise);

    const { findByRole, getByRole } = render(<Updates />);
    const checkbox = await findByRole("checkbox", { name: RC_CHECKBOX_NAME });
    const copyButton = getByRole("button", { name: "Copy install command" });
    fireEvent.click(checkbox);

    await waitFor(() => expect(mockSetReleaseChannel).toHaveBeenCalledTimes(1));
    expect(checkbox).toBeDisabled();
    expect(copyButton).toBeDisabled();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    await act(async () => {
      save.resolve(SET_CHANNEL_RESPONSE);
      await save.promise;
    });
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));
    expect(checkbox).toBeDisabled();
    expect(copyButton).toBeDisabled();

    await act(async () => {
      refetch.resolve(buildStatus({ channel: ReleaseChannel.STABLE_AND_RC }));
      await refetch.promise;
    });
    await waitFor(() => expect(checkbox).not.toBeDisabled());
    expect(copyButton).not.toBeDisabled();
    expect(checkbox).toBeChecked();
  });

  it("waits for an in-flight save before a remounted page loads status", async () => {
    const save = createDeferred<SetReleaseChannelResponse>();
    const rcStatus = buildStatus({
      channel: ReleaseChannel.STABLE_AND_RC,
      installCommand: "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.4.0-rc.1",
      latestEligible: buildReleaseInfo({
        version: "v1.4.0-rc.1",
        releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
        prerelease: true,
      }),
    });
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(rcStatus);
    mockSetReleaseChannel.mockReturnValue(save.promise);

    const firstPage = render(<Updates />);
    fireEvent.click(await firstPage.findByRole("checkbox", { name: RC_CHECKBOX_NAME }));
    await waitFor(() => expect(mockSetReleaseChannel).toHaveBeenCalledTimes(1));
    firstPage.unmount();

    const remountedPage = render(<Updates />);
    await act(async () => Promise.resolve());
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    await act(async () => {
      save.resolve(SET_CHANNEL_RESPONSE);
      await save.promise;
    });
    expect(await remountedPage.findByText("v1.4.0-rc.1")).toBeInTheDocument();
    expect(remountedPage.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeChecked();
    expect(mockPushToast).not.toHaveBeenCalledWith({
      message: "Release channel updated",
      status: "success",
    });
  });

  it("loads status after a timed-out save releases the remount barrier", async () => {
    const save = createDeferred<SetReleaseChannelResponse>();
    const rcStatus = buildStatus({ channel: ReleaseChannel.STABLE_AND_RC });
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(rcStatus);
    mockSetReleaseChannel.mockReturnValue(save.promise);

    const firstPage = render(<Updates />);
    fireEvent.click(await firstPage.findByRole("checkbox", { name: RC_CHECKBOX_NAME }));
    await waitFor(() =>
      expect(mockSetReleaseChannel).toHaveBeenCalledWith(
        { channel: ReleaseChannel.STABLE_AND_RC },
        { timeoutMs: RELEASE_CHANNEL_SAVE_TIMEOUT_MS },
      ),
    );
    firstPage.unmount();

    const remountedPage = render(<Updates />);
    await act(async () => Promise.resolve());
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    await act(async () => {
      save.reject(new ConnectError("release channel save timed out", Code.DeadlineExceeded));
      await save.promise.catch(() => undefined);
    });

    expect(await remountedPage.findByText("v1.3.0")).toBeInTheDocument();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);
    expect(remountedPage.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeChecked();
  });

  it("reconciles status after an ambiguous non-auth save failure", async () => {
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE })).mockResolvedValueOnce(
      buildStatus({
        channel: ReleaseChannel.STABLE_AND_RC,
        latestEligible: buildReleaseInfo({
          version: "v1.4.0-rc.1",
          releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
          prerelease: true,
        }),
      }),
    );
    mockSetReleaseChannel.mockRejectedValue(new Error("response lost after save"));

    const { findByRole, findByText, getByRole } = render(<Updates />);
    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "response lost after save",
        status: "error",
      }),
    );
    expect(await findByText("v1.4.0-rc.1")).toBeInTheDocument();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);
    expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeChecked();
  });

  it("reports a refresh failure separately after a successful save", async () => {
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockRejectedValueOnce(new Error("refresh failed after save"));
    mockSetReleaseChannel.mockResolvedValue(SET_CHANNEL_RESPONSE);

    const { findByRole, findByText } = render(<Updates />);
    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(await findByText("Unable to load update status")).toBeInTheDocument();
    expect(await findByText("refresh failed after save")).toBeInTheDocument();
    expect(mockPushToast).toHaveBeenCalledWith({
      message: "Release channel updated",
      status: "success",
    });
    expect(mockPushToast).not.toHaveBeenCalledWith({
      message: "Failed to update release channel",
      status: "error",
    });
  });

  it("does not refetch after an auth-related save failure", async () => {
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockRejectedValue(new ConnectError("session expired", Code.Unauthenticated));

    const { findByRole } = render(<Updates />);
    const checkbox = await findByRole("checkbox", { name: RC_CHECKBOX_NAME });
    fireEvent.click(checkbox);

    await waitFor(() => expect(mockSetReleaseChannel).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(checkbox).not.toBeDisabled());
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);
    expect(mockPushToast).not.toHaveBeenCalled();
  });

  it("reports a permission-denied save and redirects away from stale controls", async () => {
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockRejectedValue(new ConnectError("permission revoked", Code.PermissionDenied));

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: PERMISSION_REVOKED_MESSAGE,
        status: "error",
      }),
    );
    expect(permissionsMock.setPermissions).toHaveBeenCalledWith(["fleet:read"]);
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    page.rerender(<Updates />);
    expect(page.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/network");
  });

  it("invalidates revoked permission without toasting after a save outlives the page", async () => {
    const save = createDeferred<SetReleaseChannelResponse>();
    const permissionError = new ConnectError("permission revoked", Code.PermissionDenied);
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockReturnValue(save.promise);

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("checkbox", { name: RC_CHECKBOX_NAME }));
    await waitFor(() => expect(mockSetReleaseChannel).toHaveBeenCalledTimes(1));
    page.unmount();

    await act(async () => {
      save.reject(permissionError);
      await save.promise.catch(() => undefined);
    });

    expect(authErrorsMock.handleAuthErrors).toHaveBeenCalledWith(
      expect.objectContaining({
        error: permissionError,
      }),
    );
    expect(permissionsMock.setPermissions).toHaveBeenCalledWith(["fleet:read"]);
    expect(mockPushToast).not.toHaveBeenCalled();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);
  });

  it("does not apply a delayed save permission failure to a replacement session", async () => {
    const save = createDeferred<SetReleaseChannelResponse>();
    const permissionError = new ConnectError("old permission revoked", Code.PermissionDenied);
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockReturnValue(save.promise);

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("checkbox", { name: RC_CHECKBOX_NAME }));
    await waitFor(() => expect(mockSetReleaseChannel).toHaveBeenCalledTimes(1));
    page.unmount();
    permissionsMock.sessionExpiry = new Date(2_000);

    await act(async () => {
      save.reject(permissionError);
      await save.promise.catch(() => undefined);
    });

    expect(authErrorsMock.handleAuthErrors).not.toHaveBeenCalled();
    expect(permissionsMock.setPermissions).not.toHaveBeenCalled();
    expect(mockPushToast).not.toHaveBeenCalled();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);
  });

  it("invalidates stale client permission when the status load is denied", async () => {
    mockGetUpdateStatus.mockRejectedValueOnce(new ConnectError("permission revoked", Code.PermissionDenied));

    const page = render(<Updates />);

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: PERMISSION_REVOKED_MESSAGE,
        status: "error",
      }),
    );
    expect(permissionsMock.setPermissions).toHaveBeenCalledWith(["fleet:read"]);
    expect(page.queryByText("Unable to load update status")).not.toBeInTheDocument();

    page.rerender(<Updates />);
    expect(page.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/network");
  });

  it("toasts an error and leaves the checkbox unchecked when saving the channel fails", async () => {
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockRejectedValue(new Error("registry rejected the channel"));

    const { findByRole, getByRole } = render(<Updates />);
    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(mockSetReleaseChannel).toHaveBeenCalledWith(
      { channel: ReleaseChannel.STABLE_AND_RC },
      { timeoutMs: RELEASE_CHANNEL_SAVE_TIMEOUT_MS },
    );
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "registry rejected the channel",
        status: "error",
      }),
    );
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));
    // The checkbox is controlled by the persisted channel, which never moved.
    expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked();
  });

  it("redirects and does not fire the status RPC without the instance:update permission", async () => {
    permissionsMock.current = ["fleet:read"];

    const { getByTestId } = render(<Updates />);

    expect(mockUseHasPermission).toHaveBeenCalledWith("instance:update");
    expect(getByTestId("navigate")).toHaveAttribute("data-to", "/settings/network");
    // Flush a microtask turn so an incorrectly-enabled fetch would have had a chance to fire.
    await Promise.resolve();
    expect(mockGetUpdateStatus).not.toHaveBeenCalled();
  });

  it("redirects to preferences when neither instance:update nor fleet:read is allowed", async () => {
    permissionsMock.current = [];

    const { getByTestId } = render(<Updates />);

    expect(getByTestId("navigate")).toHaveAttribute("data-to", "/settings/preferences");
    await Promise.resolve();
    expect(mockGetUpdateStatus).not.toHaveBeenCalled();
  });
});
