import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import UpdateCallout from "./UpdateCallout";
import { updatesClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { useUpdateStatus } from "@/protoFleet/features/updates/api/useUpdateStatus";
import { useHasPermission } from "@/protoFleet/store";
import { pushToast } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

vi.mock("@/protoFleet/features/updates/api/useUpdateStatus", () => ({
  useUpdateStatus: vi.fn(),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  updatesClient: {
    getUpdateStatus: vi.fn(),
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

const localStorageMock = vi.hoisted(() => {
  const state = { dismissedTag: undefined as string | undefined };
  return {
    state,
    setDismissedTag: vi.fn((value: string) => {
      state.dismissedTag = value;
    }),
  };
});

vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: vi.fn(() => [localStorageMock.state.dismissedTag, localStorageMock.setDismissedTag]),
}));

const INSTALL_COMMAND = "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.3.0";
const RELEASE_NOTES_URL = "https://github.com/block/proto-fleet/releases/tag/v1.3.0";

const buildStatus = (overrides?: Partial<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
  ({
    currentVersion: "v1.2.0",
    updateAvailable: true,
    installCommand: INSTALL_COMMAND,
    latestEligible: {
      version: "v1.3.0",
      releaseNotesUrl: RELEASE_NOTES_URL,
      prerelease: false,
    },
    ...overrides,
  }) as unknown as GetUpdateStatusResponse;

const mockUseUpdateStatus = vi.mocked(useUpdateStatus);
const mockUseHasPermission = vi.mocked(useHasPermission);
const mockGetUpdateStatus = vi.mocked(updatesClient.getUpdateStatus);
const mockCopyToClipboard = vi.mocked(copyToClipboard);
const mockPushToast = vi.mocked(pushToast);

const mockUpdateAvailable = (overrides?: Partial<ReturnType<typeof useUpdateStatus>>) => {
  mockUseUpdateStatus.mockReturnValue({
    status: buildStatus(),
    hasUpdatePermission: true,
    ...overrides,
  });
};

beforeEach(() => {
  vi.clearAllMocks();
  localStorageMock.state.dismissedTag = undefined;
});

describe("UpdateCallout", () => {
  it("renders nothing when the user lacks the instance:update permission", () => {
    mockUpdateAvailable({ hasUpdatePermission: false });

    const { container } = render(<UpdateCallout />);

    expect(container).toBeEmptyDOMElement();
  });

  it("does not fire the update status RPC without the instance:update permission", async () => {
    mockUseHasPermission.mockReturnValue(false);
    const { useUpdateStatus: realUseUpdateStatus } = await vi.importActual<typeof import("../api/useUpdateStatus")>(
      "@/protoFleet/features/updates/api/useUpdateStatus",
    );
    const Probe = () => {
      realUseUpdateStatus();
      return null;
    };

    const { unmount } = render(<Probe />);

    expect(mockUseHasPermission).toHaveBeenCalledWith("instance:update");
    // Flush a microtask turn so an incorrectly-enabled fetch would have had a chance to fire.
    await Promise.resolve();
    expect(mockGetUpdateStatus).not.toHaveBeenCalled();
    unmount();
  });

  it("fires the update status RPC when the instance:update permission is held", async () => {
    mockUseHasPermission.mockReturnValue(true);
    mockGetUpdateStatus.mockResolvedValue(buildStatus());
    const { useUpdateStatus: realUseUpdateStatus } = await vi.importActual<typeof import("../api/useUpdateStatus")>(
      "@/protoFleet/features/updates/api/useUpdateStatus",
    );
    const Probe = () => {
      realUseUpdateStatus();
      return null;
    };

    const { unmount } = render(<Probe />);

    await waitFor(() => expect(mockGetUpdateStatus).toHaveBeenCalledTimes(1));
    unmount();
  });

  it("renders the version, release notes link, and copy control when an update is available", () => {
    mockUpdateAvailable();

    const { getByText, getByRole } = render(<UpdateCallout />);

    expect(getByText("v1.3.0")).toBeInTheDocument();
    const link = getByRole("link", { name: "Release notes" });
    expect(link).toHaveAttribute("href", RELEASE_NOTES_URL);
    expect(link).toHaveAttribute("target", "_blank");
    expect(getByRole("button", { name: "Copy install command" })).toBeInTheDocument();
  });

  it("copies the install command and shows a success toast", async () => {
    mockUpdateAvailable();
    mockCopyToClipboard.mockResolvedValue(undefined);

    const { getByRole } = render(<UpdateCallout />);
    fireEvent.click(getByRole("button", { name: "Copy install command" }));

    expect(mockCopyToClipboard).toHaveBeenCalledWith(INSTALL_COMMAND);
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Install command copied to clipboard",
        status: "success",
      }),
    );
  });

  it("shows an error toast when copying the install command fails", async () => {
    mockUpdateAvailable();
    mockCopyToClipboard.mockRejectedValue(new Error("copy failed"));

    const { getByRole } = render(<UpdateCallout />);
    fireEvent.click(getByRole("button", { name: "Copy install command" }));

    expect(mockCopyToClipboard).toHaveBeenCalledWith(INSTALL_COMMAND);
    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith({
        message: "Failed to copy install command",
        status: "error",
      }),
    );
  });

  it("hides after dismiss, stays hidden for the same tag, and re-shows for a different tag", () => {
    mockUpdateAvailable();

    const { getByRole, queryByTestId, rerender, unmount } = render(<UpdateCallout />);
    expect(queryByTestId("update-callout")).toBeInTheDocument();

    fireEvent.click(getByRole("button", { name: "Dismiss update notification" }));
    expect(localStorageMock.setDismissedTag).toHaveBeenCalledWith("v1.3.0");

    rerender(<UpdateCallout />);
    expect(queryByTestId("update-callout")).not.toBeInTheDocument();
    unmount();

    // A fresh mount with the same dismissed tag stays hidden.
    const remount = render(<UpdateCallout />);
    expect(remount.queryByTestId("update-callout")).not.toBeInTheDocument();

    // A different eligible tag shows the callout again.
    mockUpdateAvailable({
      status: buildStatus({
        latestEligible: {
          version: "v1.4.0",
          releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0",
          prerelease: false,
        } as GetUpdateStatusResponse["latestEligible"],
      }),
    });
    remount.rerender(<UpdateCallout />);
    expect(remount.queryByTestId("update-callout")).toBeInTheDocument();
  });

  it("carries the collapsed/expanded responsive nav classes for the laptop hover-to-expand idiom", () => {
    mockUpdateAvailable();

    const { getByTestId } = render(<UpdateCallout />);

    const collapsed = getByTestId("update-callout-collapsed");
    expect(collapsed).toHaveClass("hidden", "laptop:flex", "laptop:group-hover/nav:hidden", "desktop:hidden");

    const expanded = getByTestId("update-callout-expanded");
    expect(expanded).toHaveClass("laptop:hidden", "laptop:group-hover/nav:block", "desktop:block");
    expect(expanded).not.toHaveClass("hidden");
  });

  it("renders nothing when no update is available", () => {
    mockUpdateAvailable({
      status: buildStatus({ updateAvailable: false, installCommand: "", latestEligible: undefined }),
    });

    const { container } = render(<UpdateCallout />);

    expect(container).toBeEmptyDOMElement();
  });
});
