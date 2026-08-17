import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import Updates from "./Updates";
import { instanceUpdateClient } from "@/protoFleet/api/clients";
import type {
  GetUpdateStatusResponse,
  ReleaseInfo,
  SetReleaseChannelResponse,
  UpgradeOperation,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import {
  GetUpdateStatusResponseSchema,
  ReleaseChannel,
  ReleaseInfoSchema,
  SetReleaseChannelResponseSchema,
  UpgradeOperationSchema,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { useUpgradeOperation } from "@/protoFleet/features/updates/api/useUpgradeOperation";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

const permissionsMock = vi.hoisted(() => ({
  current: ["instance:update", "fleet:read"],
  isAuthenticated: true,
  sessionExpiry: new Date(1_000),
  sessionGeneration: 1,
  setPermissions: vi.fn<(permissions: string[]) => void>(),
  username: "operator-a",
}));
const authErrorsMock = vi.hoisted(() => ({
  handleAuthErrors: vi.fn(),
}));
interface UpgradeHookMockState {
  acknowledgeOperation: ReturnType<typeof vi.fn>;
  connectionLost: boolean;
  manualFallbackReady: boolean;
  operation?: UpgradeOperation;
  operationStatusPending: boolean;
  reconciling: boolean;
  reloadFleet: ReturnType<typeof vi.fn>;
  triggerError: string | null;
  triggering: boolean;
  trackedTargetVersion?: string;
  triggerUpgrade: ReturnType<typeof vi.fn>;
  useManualFallback: ReturnType<typeof vi.fn>;
}
const upgradeHookMock = vi.hoisted(() => ({
  current: {} as UpgradeHookMockState,
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
    useSessionExpiry: () => permissionsMock.sessionExpiry,
    useSessionGeneration: () => permissionsMock.sessionGeneration,
    useSetPermissions: () => permissionsMock.setPermissions,
    useUsername: () => permissionsMock.username,
    useAuthErrors: () => authErrorsMock,
    useFleetStore: {
      getState: () => ({
        auth: {
          isAuthenticated: permissionsMock.isAuthenticated,
          permissions: permissionsMock.current,
          sessionExpiry: permissionsMock.sessionExpiry,
          sessionGeneration: permissionsMock.sessionGeneration,
          username: permissionsMock.username,
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

vi.mock("@/protoFleet/features/updates/api/useUpgradeOperation", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/protoFleet/features/updates/api/useUpgradeOperation")>();
  return {
    ...actual,
    useUpgradeOperation: vi.fn(() => upgradeHookMock.current),
  };
});

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
const OPERATION_STARTED_AT = create(TimestampSchema, { nanos: 123_456_789, seconds: 1_700_000_000n });
const DIFFERENT_OPERATION_STARTED_AT = create(TimestampSchema, { nanos: 123_456_790, seconds: 1_700_000_000n });

type MessageOverrides<T> = Omit<Partial<T>, "$typeName" | "$unknown">;

const buildReleaseInfo = (overrides?: MessageOverrides<ReleaseInfo>): ReleaseInfo =>
  create(ReleaseInfoSchema, {
    version: "v1.3.0",
    releaseNotesUrl: RELEASE_NOTES_URL,
    prerelease: false,
    ...overrides,
  });

const buildStatus = (overrides?: MessageOverrides<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
  create(GetUpdateStatusResponseSchema, {
    currentVersion: "v1.2.0",
    channel: ReleaseChannel.STABLE,
    statusAvailable: true,
    updateAvailable: true,
    installCommand: INSTALL_COMMAND,
    latestEligible: buildReleaseInfo(),
    ...overrides,
  });

const buildOperation = (phase: UpgradePhase, overrides?: MessageOverrides<UpgradeOperation>): UpgradeOperation =>
  create(UpgradeOperationSchema, {
    id: "operation-1",
    targetVersion: "v1.3.0",
    phase,
    message: "Preparing upgrade",
    ...overrides,
  });

const mockUseHasPermission = vi.mocked(useHasPermission);
const mockUseUpgradeOperation = vi.mocked(useUpgradeOperation);
const mockGetUpdateStatus = vi.mocked(instanceUpdateClient.getUpdateStatus);
const mockSetReleaseChannel = vi.mocked(instanceUpdateClient.setReleaseChannel);
const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

const RC_CHECKBOX_NAME = "Include release candidates";
const UPDATE_STATUS_REQUEST_TIMEOUT_MS = 10_000;
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
  sessionStorage.clear();
  upgradeHookMock.current = {
    acknowledgeOperation: vi.fn().mockResolvedValue(undefined),
    connectionLost: false,
    manualFallbackReady: false,
    operation: undefined,
    operationStatusPending: false,
    reconciling: false,
    reloadFleet: vi.fn(),
    triggerError: null,
    triggering: false,
    trackedTargetVersion: undefined,
    triggerUpgrade: vi.fn().mockResolvedValue(undefined),
    useManualFallback: vi.fn(),
  };
  permissionsMock.current = ["instance:update", "fleet:read"];
  permissionsMock.isAuthenticated = true;
  permissionsMock.sessionExpiry = new Date(1_000);
  permissionsMock.sessionGeneration = 1;
  permissionsMock.username = "operator-a";
  permissionsMock.setPermissions.mockImplementation((permissions) => {
    permissionsMock.current = permissions;
  });
  mockUseHasPermission.mockImplementation((permission) => permissionsMock.current.includes(permission));
});

afterEach(() => {
  cleanup();
});

describe("Updates", () => {
  it("renders the current version, update card, notes link, and manual-install CTA regardless of callout dismissal", async () => {
    // The nav callout's dismissal must not hide the release on this page.
    localStorage.setItem(DISMISSED_UPDATE_TAG_KEY, "v1.3.0");
    mockGetUpdateStatus.mockResolvedValue(buildStatus());

    const { findByText, getByText, getByRole } = render(<Updates />);

    expect(await findByText("v1.2.0")).toBeInTheDocument();
    const link = getByRole("link", { name: "Release notes" });
    expect(link).toHaveAttribute("href", RELEASE_NOTES_URL);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(getByText("Fleet v1.3.0 available")).toBeInTheDocument();
    expect(screen.getByTestId("available-update-lockup")).toBeInTheDocument();
    expect(screen.getByTestId("available-update-animation")).toHaveAttribute(
      "src",
      "/fog-proto-logo-volume-white.html",
    );
    expect(getByText("Use manual install to update this Fleet.")).toBeInTheDocument();
    expect(screen.queryByText(INSTALL_COMMAND)).not.toBeInTheDocument();
    expect(
      within(screen.getByTestId("available-update-lockup")).getByRole("button", { name: "Install manually" }),
    ).toBeEnabled();
    expect(screen.queryByText("Manual update")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Update now" })).not.toBeInTheDocument();
    expect(mockGetUpdateStatus).toHaveBeenCalledWith({}, { timeoutMs: UPDATE_STATUS_REQUEST_TIMEOUT_MS });
  });

  it("confirms the exact offered version before starting a one-click upgrade", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("button", { name: "Update now" }));

    expect(page.getByTestId("upgrade-operation-modal")).toHaveTextContent(
      "Fleet validates this release before restarting.",
    );
    expect(upgradeHookMock.current.triggerUpgrade).not.toHaveBeenCalled();

    fireEvent.click(within(page.getByTestId("upgrade-operation-modal")).getByRole("button", { name: "Update now" }));
    await waitFor(() => expect(upgradeHookMock.current.triggerUpgrade).toHaveBeenCalledWith("v1.3.0"));
  });

  it("keeps an active operation ahead of a newer release offer", async () => {
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.PREFLIGHT, {
      targetVersion: "v1.3.0",
      message: "Validating v1.3.0",
    });
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({
        oneClickAvailable: true,
        installCommand: "install v1.4.0",
        latestEligible: buildReleaseInfo({ version: "v1.4.0" }),
      }),
    );

    const page = render(<Updates />);

    expect(await page.findByTestId("active-update-lockup")).toBeInTheDocument();
    expect(page.getByText("Updating Fleet to v1.3.0")).toBeInTheDocument();
    expect(page.getByText("Validating v1.3.0")).toBeInTheDocument();
    expect(page.getByTestId("update-status-spinner")).toBeInTheDocument();
    expect(page.getByRole("button", { name: "Updating" })).toBeEnabled();
    expect(page.queryByRole("button", { name: "Update now" })).not.toBeInTheDocument();
    expect(page.queryByRole("button", { name: "Install manually" })).not.toBeInTheDocument();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeDisabled();

    expect(page.queryByTestId("upgrade-operation-modal")).not.toBeInTheDocument();
    fireEvent.click(page.getByRole("button", { name: "Updating" }));
    expect(page.getByTestId("upgrade-operation-modal")).toHaveTextContent("Validating v1.3.0");
  });

  it.each([
    {
      caseName: "reused-ID terminal incarnation",
      outcomeRevision: 1n,
      startedAt: DIFFERENT_OPERATION_STARTED_AT,
    },
    {
      caseName: "rewritten terminal outcome revision",
      outcomeRevision: 2n,
      startedAt: OPERATION_STARTED_AT,
    },
  ])("auto-opens a $caseName", async ({ outcomeRevision, startedAt }) => {
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.FAILED, {
      error: "First failure",
      outcomeRevision: 1n,
      startedAt: OPERATION_STARTED_AT,
    });
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));
    const page = render(<Updates />);
    expect(await page.findByText("First failure")).toBeInTheDocument();

    fireEvent.click(page.getByRole("button", { name: "Close dialog" }));
    await waitFor(() => expect(page.queryByText("First failure")).not.toBeInTheDocument());
    await waitFor(() => expect(page.queryByRole("button", { name: "Close dialog" })).not.toBeInTheDocument());

    upgradeHookMock.current = {
      ...upgradeHookMock.current,
      operation: buildOperation(UpgradePhase.FAILED, {
        error: "Later failure",
        outcomeRevision,
        startedAt,
      }),
    };
    page.rerender(<Updates />);

    expect(await page.findByText("Later failure")).toBeInTheDocument();
  });

  it("reopens the same outcome for a new auth session without reopening the stale session operation", async () => {
    const outcome = buildOperation(UpgradePhase.FAILED, {
      error: "Session-scoped failure",
      outcomeRevision: 1n,
      startedAt: OPERATION_STARTED_AT,
    });
    upgradeHookMock.current.operation = outcome;
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));
    const page = render(<Updates />);
    expect(await page.findByText("Session-scoped failure")).toBeInTheDocument();

    fireEvent.click(page.getByRole("button", { name: "Close dialog" }));
    await waitFor(() => expect(page.queryByText("Session-scoped failure")).not.toBeInTheDocument());
    await waitFor(() => expect(page.queryByRole("button", { name: "Close dialog" })).not.toBeInTheDocument());

    permissionsMock.sessionGeneration = 2;
    upgradeHookMock.current = { ...upgradeHookMock.current, operationStatusPending: true };
    page.rerender(<Updates />);
    await act(async () => Promise.resolve());
    expect(page.queryByText("Session-scoped failure")).not.toBeInTheDocument();

    upgradeHookMock.current = { ...upgradeHookMock.current, operation: undefined };
    page.rerender(<Updates />);
    upgradeHookMock.current = {
      ...upgradeHookMock.current,
      operation: buildOperation(UpgradePhase.FAILED, {
        error: "Session-scoped failure",
        outcomeRevision: 1n,
        startedAt: OPERATION_STARTED_AT,
      }),
      operationStatusPending: false,
    };
    page.rerender(<Updates />);

    expect(await page.findByText("Session-scoped failure")).toBeInTheDocument();
  });

  it("keeps manual recovery usable after a failed operation and can acknowledge its durable record", async () => {
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.FAILED, {
      error: "new stack failed to start",
      hostLogPath: "/var/lib/proto-fleet-updater/logs/operation-1.log",
      recoveryCommand: "cd /opt/proto-fleet/deployment && ./run-fleet.sh --skip-build",
    });
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);

    expect(await page.findByText("Run this command on the Fleet host to continue the update.")).toBeInTheDocument();
    expect(page.queryByRole("button", { name: "Install manually" })).not.toBeInTheDocument();
    fireEvent.click(page.getByRole("button", { name: "Close" }));
    expect(upgradeHookMock.current.acknowledgeOperation).toHaveBeenCalledTimes(1);
  });

  it("warns when the dismissal could not be recorded on the host", async () => {
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.FAILED, {
      error: "new stack failed to start",
    });
    upgradeHookMock.current.acknowledgeOperation = vi
      .fn()
      .mockRejectedValue(new ConnectError("host updater is unavailable", Code.Unavailable));
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);

    expect(await page.findByText("new stack failed to start")).toBeInTheDocument();
    fireEvent.click(page.getByRole("button", { name: "Close" }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith(
        expect.objectContaining({ message: expect.stringContaining("host updater is unavailable") }),
      ),
    );
  });

  it.each([Code.Unauthenticated, Code.PermissionDenied])(
    "does not apply a delayed acknowledgement error %s to a replacement session",
    async (code) => {
      const request = createDeferred<void>();
      upgradeHookMock.current.operation = buildOperation(UpgradePhase.FAILED, {
        error: "new stack failed to start",
      });
      upgradeHookMock.current.acknowledgeOperation = vi.fn().mockReturnValue(request.promise);
      mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

      const page = render(<Updates />);

      expect(await page.findByText("new stack failed to start")).toBeInTheDocument();
      fireEvent.click(page.getByRole("button", { name: "Close" }));
      expect(upgradeHookMock.current.acknowledgeOperation).toHaveBeenCalledTimes(1);

      permissionsMock.sessionExpiry = new Date(2_000);
      permissionsMock.sessionGeneration = 2;
      await act(async () => {
        request.reject(new ConnectError("previous session is no longer authorized", code));
        await request.promise.catch(() => undefined);
      });

      expect(authErrorsMock.handleAuthErrors).not.toHaveBeenCalled();
      expect(permissionsMock.setPermissions).not.toHaveBeenCalled();
      expect(mockPushToast).not.toHaveBeenCalled();
    },
  );

  it("closes the outcome dialog when another session dismissed the failure", async () => {
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.FAILED, {
      error: "new stack failed to start",
    });
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);
    expect(await page.findByText("new stack failed to start")).toBeInTheDocument();

    // Polling observed a durably acknowledged operation and removed it.
    upgradeHookMock.current = { ...upgradeHookMock.current, operation: undefined };
    page.rerender(<Updates />);

    // The dialog must disappear, not morph into a confirmation for starting
    // the same upgrade again. Waited separately because the dialog's exit
    // transition keeps its frame in the DOM briefly after the content clears.
    await waitFor(() => expect(page.queryByText("new stack failed to start")).not.toBeInTheDocument());
    await waitFor(() => expect(page.queryByRole("button", { name: "Close dialog" })).not.toBeInTheDocument());
  });

  it("acknowledges the exact successful outcome before reloading Fleet", async () => {
    const acknowledgement = createDeferred<void>();
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.SUCCEEDED, {
      message: "Update complete",
    });
    upgradeHookMock.current.acknowledgeOperation = vi.fn().mockReturnValue(acknowledgement.promise);
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({
        currentVersion: "v1.3.0",
        updateAvailable: false,
        installCommand: "",
        latestEligible: undefined,
        oneClickAvailable: true,
      }),
    );

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("button", { name: "Relaunch" }));

    expect(upgradeHookMock.current.acknowledgeOperation).toHaveBeenCalledTimes(1);
    expect(upgradeHookMock.current.reloadFleet).not.toHaveBeenCalled();
    expect(page.getByText("Upgrade complete")).toBeInTheDocument();

    await act(async () => {
      acknowledgement.resolve();
      await acknowledgement.promise;
    });
    expect(upgradeHookMock.current.reloadFleet).toHaveBeenCalledTimes(1);
  });

  it("keeps a successful outcome visible and does not reload when acknowledgement fails", async () => {
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.SUCCEEDED, {
      message: "Upgrade complete",
    });
    upgradeHookMock.current.acknowledgeOperation = vi
      .fn()
      .mockRejectedValue(new ConnectError("host updater is unavailable", Code.Unavailable));
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({
        currentVersion: "v1.3.0",
        updateAvailable: false,
        installCommand: "",
        latestEligible: undefined,
        oneClickAvailable: true,
      }),
    );

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("button", { name: "Reload Fleet" }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith(
        expect.objectContaining({ message: expect.stringContaining("Fleet wasn't reloaded") }),
      ),
    );
    expect(upgradeHookMock.current.reloadFleet).not.toHaveBeenCalled();
    expect(page.getByText("Upgrade complete")).toBeInTheDocument();
    expect(page.getByRole("button", { name: "Reload Fleet" })).toBeEnabled();
  });

  it("does not carry a pending success acknowledgement into a replacement auth session", async () => {
    const acknowledgement = createDeferred<void>();
    upgradeHookMock.current.operation = buildOperation(UpgradePhase.SUCCEEDED, {
      message: "Upgrade complete",
    });
    upgradeHookMock.current.acknowledgeOperation = vi.fn().mockReturnValue(acknowledgement.promise);
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);
    const reloadButton = await page.findByRole("button", { name: "Reload Fleet" });
    fireEvent.click(reloadButton);
    expect(reloadButton).toBeDisabled();

    permissionsMock.sessionExpiry = new Date(2_000);
    permissionsMock.sessionGeneration = 2;
    page.rerender(<Updates />);

    expect(page.getByRole("button", { name: "Reload Fleet" })).toBeEnabled();
    await act(async () => {
      acknowledgement.resolve();
      await acknowledgement.promise;
    });
    expect(upgradeHookMock.current.reloadFleet).not.toHaveBeenCalled();
  });

  it("locks competing controls while reconciling an ambiguous trigger", async () => {
    upgradeHookMock.current.reconciling = true;
    upgradeHookMock.current.triggerError = "Fleet did not confirm the request";
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);

    expect(await page.findByText("v1.2.0")).toBeInTheDocument();
    expect(page.getByText("No action needed. We're confirming that the host started the update.")).toBeInTheDocument();
    expect(page.queryByRole("button", { name: "Install manually" })).not.toBeInTheDocument();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeDisabled();
    expect(page.queryByRole("button", { name: "View update details" })).not.toBeInTheDocument();
    expect(page.queryByTestId("upgrade-operation-modal")).not.toBeInTheDocument();
  });

  it("keeps install controls locked while fallback refreshes the authoritative offer", async () => {
    const refreshedStatus = createDeferred<GetUpdateStatusResponse>();
    upgradeHookMock.current.reconciling = true;
    upgradeHookMock.current.manualFallbackReady = true;
    upgradeHookMock.current.triggerError = "Fleet did not confirm the request";
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ oneClickAvailable: true }));
    mockGetUpdateStatus.mockReturnValueOnce(refreshedStatus.promise);

    const page = render(<Updates />);
    expect(await page.findByText("Update needs host confirmation")).toBeInTheDocument();
    fireEvent.click(page.getByRole("button", { name: "Use manual install" }));
    expect(upgradeHookMock.current.useManualFallback).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));

    upgradeHookMock.current = {
      ...upgradeHookMock.current,
      manualFallbackReady: false,
      reconciling: false,
      triggerError: null,
    };
    page.rerender(<Updates />);

    expect(page.getByRole("button", { name: "Install manually" })).toBeDisabled();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeDisabled();

    await act(async () => {
      refreshedStatus.resolve(
        buildStatus({
          installCommand: "install v1.4.0",
          latestEligible: buildReleaseInfo({ version: "v1.4.0" }),
          oneClickAvailable: true,
        }),
      );
      await refreshedStatus.promise;
    });

    expect(await page.findByText("Fleet v1.4.0 available")).toBeInTheDocument();
    expect(page.getByRole("button", { name: "Install manually" })).toBeEnabled();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeEnabled();
  });

  it("keeps controls locked while an untracked success refreshes the installed version", async () => {
    const refreshedStatus = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ oneClickAvailable: true }));
    mockGetUpdateStatus.mockReturnValueOnce(refreshedStatus.promise);

    const page = render(<Updates />);
    expect(await page.findByText("Fleet v1.3.0 available")).toBeInTheDocument();
    const hookCalls = mockUseUpgradeOperation.mock.calls;
    const hookOptions = hookCalls[hookCalls.length - 1]?.[0];
    expect(hookOptions?.onUntrackedSuccess).toEqual(expect.any(Function));

    act(() => hookOptions?.onUntrackedSuccess?.(buildOperation(UpgradePhase.SUCCEEDED)));
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));

    expect(page.getByRole("button", { name: "Install manually" })).toBeDisabled();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeDisabled();

    await act(async () => {
      refreshedStatus.resolve(
        buildStatus({
          currentVersion: "v1.3.0",
          updateAvailable: false,
          installCommand: "",
          latestEligible: undefined,
          oneClickAvailable: true,
        }),
      );
      await refreshedStatus.promise;
    });

    expect(await page.findByText("Fleet is up to date")).toBeInTheDocument();
    expect(page.queryByRole("button", { name: "Install manually" })).not.toBeInTheDocument();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeEnabled();
  });

  it("locks competing controls until the initial operation status resolves", async () => {
    upgradeHookMock.current.operationStatusPending = true;
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);

    expect(await page.findByRole("button", { name: "Update now" })).toBeDisabled();
    expect(page.getByRole("button", { name: "Install manually" })).toBeDisabled();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeDisabled();
  });

  it("locks competing controls whenever a persisted operation remains unresolved", async () => {
    upgradeHookMock.current.trackedTargetVersion = "v1.3.0";
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ oneClickAvailable: true }));

    const page = render(<Updates />);

    expect(await page.findByRole("button", { name: "Update now" })).toBeDisabled();
    expect(page.getByRole("button", { name: "Install manually" })).toBeDisabled();
    expect(page.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeDisabled();
  });

  it("keeps an ambiguous request's target when a newer release is now eligible", async () => {
    upgradeHookMock.current.reconciling = true;
    upgradeHookMock.current.trackedTargetVersion = "v1.3.0";
    upgradeHookMock.current.triggerError = "Fleet did not confirm the v1.3.0 request";
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({
        installCommand: "install v1.4.0",
        latestEligible: buildReleaseInfo({ version: "v1.4.0" }),
        oneClickAvailable: true,
      }),
    );

    const page = render(<Updates />);

    expect(await page.findByText("Confirming update with host")).toBeInTheDocument();
    expect(page.getByText("No action needed. We're confirming that the host started the update.")).toBeInTheDocument();
    expect(page.queryByTestId("upgrade-operation-modal")).not.toBeInTheDocument();
  });

  it("refreshes the eligible release after reconciliation finds no operation", async () => {
    const refreshedStatus = createDeferred<GetUpdateStatusResponse>();
    upgradeHookMock.current.reconciling = true;
    upgradeHookMock.current.triggerError = "Fleet did not confirm the request";
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ oneClickAvailable: true }))
      .mockReturnValueOnce(refreshedStatus.promise);

    const page = render(<Updates />);
    expect(await page.findByText("Confirming update with host")).toBeInTheDocument();

    upgradeHookMock.current = { ...upgradeHookMock.current, reconciling: false };
    page.rerender(<Updates />);

    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));
    expect(page.queryByTestId("upgrade-operation-modal")).not.toBeInTheDocument();

    await act(async () => {
      refreshedStatus.resolve(
        buildStatus({
          installCommand: "install v1.4.0",
          latestEligible: buildReleaseInfo({ version: "v1.4.0" }),
          oneClickAvailable: true,
        }),
      );
      await refreshedStatus.promise;
    });

    fireEvent.click(page.getByRole("button", { name: "View update details" }));
    expect(within(page.getByTestId("upgrade-operation-modal")).getByText("Update Fleet to v1.4.0")).toBeInTheDocument();
    expect(
      within(page.getByTestId("upgrade-operation-modal")).getByRole("button", { name: "Update now" }),
    ).toBeEnabled();
  });

  it("keeps a stale modal target disabled after its authoritative refresh fails", async () => {
    const failedRefresh = createDeferred<GetUpdateStatusResponse>();
    upgradeHookMock.current.reconciling = true;
    upgradeHookMock.current.triggerError = "Fleet did not confirm the request";
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ oneClickAvailable: true }))
      .mockReturnValueOnce(failedRefresh.promise);

    const page = render(<Updates />);
    expect(await page.findByText("Confirming update with host")).toBeInTheDocument();

    upgradeHookMock.current = { ...upgradeHookMock.current, reconciling: false };
    page.rerender(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));

    await act(async () => {
      failedRefresh.reject(new Error("release status unavailable"));
      await failedRefresh.promise.catch(() => undefined);
    });

    fireEvent.click(page.getByRole("button", { name: "View update details" }));
    expect(page.getByRole("alert")).toHaveTextContent("Fleet did not confirm the request");
    expect(
      within(page.getByTestId("upgrade-operation-modal")).queryByRole("button", { name: "Update now" }),
    ).not.toBeInTheDocument();
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

    expect(await findByText("Fleet v1.3.0 available")).toBeInTheDocument();
    expect(queryByRole("link", { name: "Release notes" })).not.toBeInTheDocument();
  });

  it("copies the install command and shows a success toast", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus());
    mockCopyToClipboard.mockResolvedValue(undefined);

    const { findByRole } = render(<Updates />);
    fireEvent.click(await findByRole("button", { name: "Install manually" }));
    const modal = await screen.findByTestId("manual-install-modal");
    expect(modal).toHaveTextContent("Install Fleet manually");
    expect(modal).toHaveTextContent("Run this command on the Fleet host to install v1.3.0.");
    expect(within(modal).getByText(INSTALL_COMMAND)).toBeInTheDocument();
    fireEvent.click(within(modal).getByRole("button", { name: "Copy install command" }));

    expect(mockCopyToClipboard).toHaveBeenCalledWith(INSTALL_COMMAND);
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Install command copied to clipboard",
        status: "success",
      }),
    );
  });

  it("disables copying a manual install command while an update state is unresolved", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus());

    const page = render(<Updates />);
    fireEvent.click(await page.findByRole("button", { name: "Install manually" }));
    expect(await screen.findByRole("button", { name: "Copy install command" })).toBeEnabled();

    upgradeHookMock.current.reconciling = true;
    page.rerender(<Updates />);

    expect(screen.getByRole("button", { name: "Copy install command" })).toBeDisabled();
  });

  it("shows an error toast when copying the install command fails", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus());
    mockCopyToClipboard.mockRejectedValue(new Error("copy failed"));

    const { findByRole } = render(<Updates />);
    fireEvent.click(await findByRole("button", { name: "Install manually" }));
    fireEvent.click(await findByRole("button", { name: "Copy install command" }));

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Couldn't copy install command",
        status: "error",
      }),
    );
  });

  it("renders the up-to-date state when no update is available", async () => {
    mockGetUpdateStatus.mockResolvedValue(
      buildStatus({ updateAvailable: false, installCommand: "", latestEligible: undefined }),
    );

    const { findByText, queryByRole } = render(<Updates />);

    expect(await findByText("Fleet is up to date")).toBeInTheDocument();
    expect(screen.getByTestId("current-version-success-icon")).toBeInTheDocument();
    expect(queryByRole("button", { name: "Install manually" })).not.toBeInTheDocument();
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
    expect(queryByText("Fleet is up to date")).not.toBeInTheDocument();
  });

  it("renders the error state when the status RPC fails on load", async () => {
    mockGetUpdateStatus.mockRejectedValue(new Error("release registry unreachable"));

    const { findByText, getByText } = render(<Updates />);

    expect(await findByText("We couldn't load update status")).toBeInTheDocument();
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
        message: "Release channel saved",
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
    expect(await findByText("Fleet v1.3.0 available")).toBeInTheDocument();

    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(await findByText("Fleet v1.4.0-rc.1 available")).toBeInTheDocument();
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
    expect(await findByText("Fleet v1.3.0 available")).toBeInTheDocument();
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
    expect(queryByText("Fleet v1.4.0-rc.1 available")).not.toBeInTheDocument();
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
    expect(await findByText("Fleet v1.3.0 available")).toBeInTheDocument();

    await act(async () => {
      staleRequest.reject(new Error("stale registry failure"));
      await staleRequest.promise.catch(() => undefined);
    });
    expect(queryByText("We couldn't load update status")).not.toBeInTheDocument();
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

  it("restarts a pending status refresh when the authenticated session changes in place", async () => {
    const previousRequest = createDeferred<GetUpdateStatusResponse>();
    const replacementRequest = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockReturnValueOnce(previousRequest.promise).mockReturnValueOnce(replacementRequest.promise);

    const page = render(<Updates />);
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));

    permissionsMock.sessionExpiry = new Date(2_000);
    permissionsMock.sessionGeneration = 2;
    page.rerender(<Updates />);

    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));

    await act(async () => {
      replacementRequest.resolve(buildStatus());
      await replacementRequest.promise;
    });

    expect(await page.findByText("Fleet v1.3.0 available")).toBeInTheDocument();
    expect(page.getByRole("button", { name: "Install manually" })).toBeEnabled();

    await act(async () => {
      previousRequest.reject(new ConnectError("old session expired", Code.Unauthenticated));
      await previousRequest.promise.catch(() => undefined);
    });

    expect(authErrorsMock.handleAuthErrors).not.toHaveBeenCalled();
  });

  it("disables channel and copy controls throughout the save and refetch", async () => {
    const save = createDeferred<SetReleaseChannelResponse>();
    const refetch = createDeferred<GetUpdateStatusResponse>();
    mockGetUpdateStatus.mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockGetUpdateStatus.mockReturnValueOnce(refetch.promise);
    mockSetReleaseChannel.mockReturnValue(save.promise);

    const { findByRole, getByRole } = render(<Updates />);
    const checkbox = await findByRole("checkbox", { name: RC_CHECKBOX_NAME });
    const manualInstallButton = getByRole("button", { name: "Install manually" });
    fireEvent.click(checkbox);

    await waitFor(() => expect(mockSetReleaseChannel).toHaveBeenCalledTimes(1));
    expect(checkbox).toBeDisabled();
    expect(manualInstallButton).toBeDisabled();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1);

    await act(async () => {
      save.resolve(SET_CHANNEL_RESPONSE);
      await save.promise;
    });
    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2));
    expect(checkbox).toBeDisabled();
    expect(manualInstallButton).toBeDisabled();

    await act(async () => {
      refetch.resolve(buildStatus({ channel: ReleaseChannel.STABLE_AND_RC }));
      await refetch.promise;
    });
    await waitFor(() => expect(checkbox).not.toBeDisabled());
    expect(manualInstallButton).not.toBeDisabled();
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
    expect(await remountedPage.findByText("Fleet v1.4.0-rc.1 available")).toBeInTheDocument();
    expect(remountedPage.getByRole("checkbox", { name: RC_CHECKBOX_NAME })).toBeChecked();
    expect(mockPushToast).not.toHaveBeenCalledWith({
      message: "Release channel saved",
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

    expect(await remountedPage.findByText("Fleet v1.3.0 available")).toBeInTheDocument();
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
    expect(await findByText("Fleet v1.4.0-rc.1 available")).toBeInTheDocument();
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

    expect(await findByText("We couldn't load update status")).toBeInTheDocument();
    expect(await findByText("refresh failed after save")).toBeInTheDocument();
    expect(mockPushToast).toHaveBeenCalledWith({
      message: "Release channel saved",
      status: "success",
    });
    expect(mockPushToast).not.toHaveBeenCalledWith({
      message: "We couldn't update the release channel",
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
    expect(page.queryByText("We couldn't load update status")).not.toBeInTheDocument();

    page.rerender(<Updates />);
    expect(page.getByTestId("navigate")).toHaveAttribute("data-to", "/settings/network");
  });

  it("invalidates stale client permission when upgrade polling is denied", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus());

    const page = render(<Updates />);
    await page.findByText("v1.2.0");
    const lastHookCall = mockUseUpgradeOperation.mock.calls[mockUseUpgradeOperation.mock.calls.length - 1];
    const onPollError = lastHookCall?.[0].onPollError;

    act(() => {
      onPollError?.(new ConnectError("permission revoked", Code.PermissionDenied));
    });

    expect(mockPushToast).toHaveBeenCalledWith({
      message: PERMISSION_REVOKED_MESSAGE,
      status: "error",
    });
    expect(permissionsMock.setPermissions).toHaveBeenCalledWith(["fleet:read"]);

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
