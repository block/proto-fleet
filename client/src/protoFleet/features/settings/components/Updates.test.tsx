import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Updates from "./Updates";
import { updatesClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { ReleaseChannel } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

vi.mock("react-router-dom", () => ({
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate" data-to={to} />,
}));

vi.mock("@/protoFleet/store", () => {
  // Stable identity, mirroring the real hook's memoization: the page's fetch
  // effect depends on handleAuthErrors.
  const authErrors = {
    handleAuthErrors: ({ error, onError }: { error: unknown; onError?: (error: unknown) => void }) => onError?.(error),
  };
  return {
    useHasPermission: vi.fn(() => true),
    useAuthErrors: () => authErrors,
  };
});

vi.mock("@/protoFleet/api/clients", () => ({
  updatesClient: {
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

const buildStatus = (overrides?: Partial<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
  ({
    currentVersion: "v1.2.0",
    channel: ReleaseChannel.STABLE,
    updateAvailable: true,
    installCommand: INSTALL_COMMAND,
    latestEligible: {
      version: "v1.3.0",
      releaseNotesUrl: RELEASE_NOTES_URL,
      prerelease: false,
    },
    ...overrides,
  }) as unknown as GetUpdateStatusResponse;

const mockUseHasPermission = vi.mocked(useHasPermission);
const mockGetUpdateStatus = vi.mocked(updatesClient.getUpdateStatus);
const mockSetReleaseChannel = vi.mocked(updatesClient.setReleaseChannel);
const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

const SELECTED_SEGMENT_CLASS = "text-emphasis-200";

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockUseHasPermission.mockReturnValue(true);
});

describe("Updates", () => {
  it("renders the current version, latest release, notes link, and copy control regardless of callout dismissal", async () => {
    // The nav callout's dismissal must not hide the release on this page.
    localStorage.setItem("dismissedUpdateTag", "v1.3.0");
    mockGetUpdateStatus.mockResolvedValue(buildStatus());

    const { findByText, getByText, getByRole } = render(<Updates />);

    expect(await findByText("v1.2.0")).toBeInTheDocument();
    expect(getByText("v1.3.0")).toBeInTheDocument();
    const link = getByRole("link", { name: "Release notes" });
    expect(link).toHaveAttribute("href", RELEASE_NOTES_URL);
    expect(link).toHaveAttribute("target", "_blank");
    expect(getByText(INSTALL_COMMAND)).toBeInTheDocument();
    expect(getByRole("button", { name: "Copy install command" })).toBeInTheDocument();
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

  it("renders the up-to-date state when no release snapshot exists yet", async () => {
    // updateAvailable true but latestEligible unset: treat as no eligible release.
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ latestEligible: undefined, installCommand: "" }));

    const { findByText } = render(<Updates />);

    expect(await findByText("You're on the latest version")).toBeInTheDocument();
  });

  it("renders the error state when the status RPC fails on load", async () => {
    mockGetUpdateStatus.mockRejectedValue(new Error("release registry unreachable"));

    const { findByText, getByText } = render(<Updates />);

    expect(await findByText("Unable to load update status")).toBeInTheDocument();
    expect(getByText("release registry unreachable")).toBeInTheDocument();
  });

  it("saves a channel change and toasts success", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockResolvedValue({} as never);

    const { findByRole, getByRole } = render(<Updates />);
    fireEvent.mouseDown(await findByRole("button", { name: "Stable + RC" }));

    expect(mockSetReleaseChannel).toHaveBeenCalledWith({ channel: ReleaseChannel.STABLE_AND_RC });
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Release channel updated",
        status: "success",
      }),
    );
    await waitFor(() => expect(getByRole("button", { name: "Stable + RC" })).toHaveClass(SELECTED_SEGMENT_CLASS));
  });

  it("toasts an error and reverts the control when saving the channel fails", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockRejectedValue(new Error("registry rejected the channel"));

    const { findByRole, getByRole } = render(<Updates />);
    fireEvent.mouseDown(await findByRole("button", { name: "Stable + RC" }));

    expect(mockSetReleaseChannel).toHaveBeenCalledWith({ channel: ReleaseChannel.STABLE_AND_RC });
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "registry rejected the channel",
        status: "error",
      }),
    );
    // The control snaps back to the persisted channel.
    await waitFor(() => expect(getByRole("button", { name: "Stable" })).toHaveClass(SELECTED_SEGMENT_CLASS));
    expect(getByRole("button", { name: "Stable + RC" })).not.toHaveClass(SELECTED_SEGMENT_CLASS);
  });

  it("redirects and does not fire the status RPC without the instance:update permission", async () => {
    mockUseHasPermission.mockReturnValue(false);

    const { getByTestId } = render(<Updates />);

    expect(mockUseHasPermission).toHaveBeenCalledWith("instance:update");
    expect(getByTestId("navigate")).toHaveAttribute("data-to", "/settings/network");
    // Flush a microtask turn so an incorrectly-enabled fetch would have had a chance to fire.
    await Promise.resolve();
    expect(mockGetUpdateStatus).not.toHaveBeenCalled();
  });
});
