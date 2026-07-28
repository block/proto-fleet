import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Updates from "./Updates";
import { updatesClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { ReleaseChannel } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { DISMISSED_UPDATE_TAG_KEY } from "@/protoFleet/features/updates/components/UpdateCallout";
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

const RC_CHECKBOX_NAME = "Include release candidates";

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockUseHasPermission.mockReturnValue(true);
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
        latestEligible: {
          version: "v1.3.0",
          releaseNotesUrl: "",
          prerelease: false,
        } as GetUpdateStatusResponse["latestEligible"],
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
    // The success path refetches; the second response carries the persisted
    // new channel.
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE_AND_RC }));
    mockSetReleaseChannel.mockResolvedValue({} as never);

    const { findByRole, getByRole } = render(<Updates />);
    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(mockSetReleaseChannel).toHaveBeenCalledWith({ channel: ReleaseChannel.STABLE_AND_RC });
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
    mockSetReleaseChannel.mockResolvedValue({} as never);

    const { findByRole, getByRole } = render(<Updates />);
    const checkbox = await findByRole("checkbox", { name: RC_CHECKBOX_NAME });
    expect(checkbox).toBeChecked();
    fireEvent.click(checkbox);

    expect(mockSetReleaseChannel).toHaveBeenCalledWith({ channel: ReleaseChannel.STABLE });
    await waitFor(() => expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked());
  });

  it("refetches the update status after a successful channel change", async () => {
    // Each channel offers a different eligible release; the page must not
    // keep showing the old channel's offer after the switch.
    const rcStatus = buildStatus({
      channel: ReleaseChannel.STABLE_AND_RC,
      installCommand: "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.4.0-rc.1",
      latestEligible: {
        version: "v1.4.0-rc.1",
        releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
        prerelease: true,
      } as GetUpdateStatusResponse["latestEligible"],
    });
    mockGetUpdateStatus
      .mockResolvedValueOnce(buildStatus({ channel: ReleaseChannel.STABLE }))
      .mockResolvedValueOnce(rcStatus);
    mockSetReleaseChannel.mockResolvedValue({} as never);

    const { findByText, findByRole } = render(<Updates />);
    expect(await findByText("v1.3.0")).toBeInTheDocument();

    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(await findByText("v1.4.0-rc.1")).toBeInTheDocument();
    expect(mockGetUpdateStatus).toHaveBeenCalledTimes(2);
  });

  it("toasts an error and leaves the checkbox unchecked when saving the channel fails", async () => {
    mockGetUpdateStatus.mockResolvedValue(buildStatus({ channel: ReleaseChannel.STABLE }));
    mockSetReleaseChannel.mockRejectedValue(new Error("registry rejected the channel"));

    const { findByRole, getByRole } = render(<Updates />);
    fireEvent.click(await findByRole("checkbox", { name: RC_CHECKBOX_NAME }));

    expect(mockSetReleaseChannel).toHaveBeenCalledWith({ channel: ReleaseChannel.STABLE_AND_RC });
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "registry rejected the channel",
        status: "error",
      }),
    );
    // The checkbox is controlled by the persisted channel, which never moved.
    expect(getByRole("checkbox", { name: RC_CHECKBOX_NAME })).not.toBeChecked();
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
