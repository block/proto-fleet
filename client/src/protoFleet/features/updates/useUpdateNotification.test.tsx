import type { ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { useUpdateStatus } from "@/protoFleet/features/updates/api/useUpdateStatus";
import UpdateNotificationModal from "@/protoFleet/features/updates/components/UpdateNotificationModal";
import { useUpdateNotification } from "@/protoFleet/features/updates/useUpdateNotification";
import { pushToast, removeToast } from "@/shared/features/toaster";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

vi.mock("@/protoFleet/features/updates/api/useUpdateStatus", () => ({
  useUpdateStatus: vi.fn(),
}));

vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: vi.fn(),
}));

vi.mock("@/shared/features/toaster", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/shared/features/toaster")>();
  return {
    ...actual,
    pushToast: vi.fn(() => 101),
    removeToast: vi.fn(),
  };
});

vi.mock("@/protoFleet/features/updates/copyInstallCommand", () => ({
  copyInstallCommand: vi.fn(),
}));

const INSTALL_COMMAND = `bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v1.3.0/install.sh") v1.3.0`;
const RELEASE_NOTES_URL = "https://github.com/block/proto-fleet/releases/tag/v1.3.0";

const buildStatus = (version = "v1.3.0"): GetUpdateStatusResponse =>
  ({
    currentVersion: "v1.2.0",
    statusAvailable: true,
    updateAvailable: true,
    installCommand: INSTALL_COMMAND.replaceAll("v1.3.0", version),
    latestEligible: {
      version,
      releaseNotesUrl: RELEASE_NOTES_URL.replaceAll("v1.3.0", version),
      prerelease: false,
    },
  }) as unknown as GetUpdateStatusResponse;

const mockUseUpdateStatus = vi.mocked(useUpdateStatus);
const mockUseReactiveLocalStorage = vi.mocked(useReactiveLocalStorage);
const mockPushToast = vi.mocked(pushToast);
const mockRemoveToast = vi.mocked(removeToast);
const setDismissedTag = vi.fn();

const Harness = ({ children }: { children?: ReactNode }) => {
  const updateNotification = useUpdateNotification();
  return (
    <>
      {updateNotification.updatePill ? (
        <button type="button" onClick={updateNotification.updatePill.onClick}>
          Update available
        </button>
      ) : null}
      <UpdateNotificationModal
        open={updateNotification.modalOpen}
        release={updateNotification.release}
        installCommand={updateNotification.installCommand}
        onDismiss={updateNotification.closeModal}
      />
      {children}
    </>
  );
};

describe("useUpdateNotification", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseUpdateStatus.mockReturnValue({ status: buildStatus(), hasUpdatePermission: true });
    mockUseReactiveLocalStorage.mockReturnValue([undefined, setDismissedTag]);
  });

  it("pushes a persistent clickable info toast for an available update", async () => {
    render(<Harness />);

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Update available: Fleet v1.3.0",
          status: "info",
          ttl: false,
          onClick: expect.any(Function),
          onClose: expect.any(Function),
        }),
      ),
    );
  });

  it("stays hidden while update status is unavailable", async () => {
    mockUseUpdateStatus.mockReturnValue({
      status: { ...buildStatus(), statusAvailable: false },
      hasUpdatePermission: true,
    });

    render(<Harness />);
    await act(async () => Promise.resolve());

    expect(mockPushToast).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Update available" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("update-modal")).not.toBeInTheDocument();
  });

  it("stores the dismissed tag and shows the update pill after dismissal", async () => {
    const { rerender } = render(<Harness />);
    await waitFor(() => expect(mockPushToast).toHaveBeenCalledTimes(1));

    act(() => {
      mockPushToast.mock.calls[0][0].onClose?.();
    });

    expect(setDismissedTag).toHaveBeenCalledWith("v1.3.0");

    mockUseReactiveLocalStorage.mockReturnValue(["v1.3.0", setDismissedTag]);
    rerender(<Harness />);

    expect(screen.getByRole("button", { name: "Update available" })).toBeInTheDocument();
    expect(mockRemoveToast).toHaveBeenCalledWith(null);
  });

  it("re-invokes the toast when a newer update becomes available", async () => {
    mockUseReactiveLocalStorage.mockReturnValue(["v1.3.0", setDismissedTag]);
    mockUseUpdateStatus.mockReturnValue({ status: buildStatus("v1.4.0"), hasUpdatePermission: true });

    render(<Harness />);

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Update available: Fleet v1.4.0",
          ttl: false,
        }),
      ),
    );
    expect(screen.queryByRole("button", { name: "Update available" })).not.toBeInTheDocument();
  });

  it("opens the modal from the toast click and from the dismissed pill", async () => {
    const { rerender } = render(<Harness />);
    await waitFor(() => expect(mockPushToast).toHaveBeenCalledTimes(1));

    act(() => {
      mockPushToast.mock.calls[0][0].onClick?.();
    });

    expect(await screen.findByTestId("update-modal")).toBeInTheDocument();
    expect(screen.getByText("Update to Fleet v1.3.0")).toBeInTheDocument();
    expect(screen.getByText(INSTALL_COMMAND)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy install command" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View release notes for v1.3.0" })).toHaveAttribute(
      "href",
      RELEASE_NOTES_URL,
    );

    fireEvent.click(screen.getByRole("button", { name: "Close dialog" }));
    await waitFor(() => expect(screen.queryByTestId("update-modal")).not.toBeInTheDocument());

    mockUseReactiveLocalStorage.mockReturnValue(["v1.3.0", setDismissedTag]);
    rerender(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Update available" }));

    expect(await screen.findByTestId("update-modal")).toBeInTheDocument();
  });
});
