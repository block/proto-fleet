import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import GlobalActionsWidgetWrapper from "./GlobalActionsWidgetWrapper";
import { useDownloadLogs } from "@/protoOS/api/hooks/useDownloadLogs";
import { useLocateSystem } from "@/protoOS/api/hooks/useLocateSystem";
import { AUTH_ACTIONS } from "@/protoOS/store/types";

const { mockCheckAccess, mockSetPausedAuthAction, mockSetDismissedLoginModal, mockState } = vi.hoisted(() => ({
  mockCheckAccess: vi.fn(),
  mockSetPausedAuthAction: vi.fn(),
  mockSetDismissedLoginModal: vi.fn(),
  mockState: {
    hasAccess: undefined as boolean | undefined,
    pausedAuthAction: null as string | null,
    dismissedLoginModal: false,
  },
}));

vi.mock("@/protoOS/api/hooks/useLocateSystem", () => ({
  useLocateSystem: vi.fn(),
}));

vi.mock("@/protoOS/api/hooks/useDownloadLogs", () => ({
  useDownloadLogs: vi.fn(),
}));

vi.mock("@/protoOS/store", async () => {
  const { AUTH_ACTIONS: actions } = await import("@/protoOS/store/types");
  return {
    useAccessToken: vi.fn(() => ({
      checkAccess: mockCheckAccess,
      hasAccess: mockState.hasAccess,
    })),
    AUTH_ACTIONS: actions,
    useDismissedLoginModal: vi.fn(() => mockState.dismissedLoginModal),
    useSetDismissedLoginModal: vi.fn(() => mockSetDismissedLoginModal),
    usePausedAuthAction: vi.fn(() => mockState.pausedAuthAction),
    useSetPausedAuthAction: vi.fn(() => mockSetPausedAuthAction),
  };
});

describe("GlobalActionsWidgetWrapper", () => {
  const mockLocateSystem = vi.fn();
  const mockDownloadLogs = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockState.hasAccess = undefined;
    mockState.pausedAuthAction = null;
    mockState.dismissedLoginModal = false;

    (useLocateSystem as Mock).mockReturnValue({
      locateSystem: mockLocateSystem,
      pending: false,
    });

    (useDownloadLogs as Mock).mockReturnValue({
      downloadLogs: mockDownloadLogs,
      isDownloading: false,
    });
  });

  test("renders GlobalActionsWidget", () => {
    const { container } = render(<GlobalActionsWidgetWrapper />);
    const button = container.querySelector("button");
    expect(button).toBeInTheDocument();
  });

  test("sets pausedAuthAction and calls checkAccess when Blink LEDs is clicked", () => {
    const { container, getByText } = render(<GlobalActionsWidgetWrapper />);

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const blinkButton = getByText("Blink LEDs").closest("button");
    fireEvent.click(blinkButton!);

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(AUTH_ACTIONS.locate);
    expect(mockCheckAccess).toHaveBeenCalledTimes(1);
    // locateSystem should NOT be called directly
    expect(mockLocateSystem).not.toHaveBeenCalled();
  });

  test("calls locateSystem after auth succeeds", () => {
    mockState.hasAccess = true;
    mockState.pausedAuthAction = AUTH_ACTIONS.locate;

    render(<GlobalActionsWidgetWrapper />);

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(null);
    expect(mockLocateSystem).toHaveBeenCalledTimes(1);
    expect(mockLocateSystem).toHaveBeenCalledWith({
      ledOnTime: 30,
      onError: expect.any(Function),
    });
  });

  test("does not call locateSystem when hasAccess is false", () => {
    mockState.hasAccess = false;
    mockState.pausedAuthAction = AUTH_ACTIONS.locate;

    render(<GlobalActionsWidgetWrapper />);

    expect(mockLocateSystem).not.toHaveBeenCalled();
  });

  test("does not call locateSystem when pausedAuthAction is not locate", () => {
    mockState.hasAccess = true;
    mockState.pausedAuthAction = AUTH_ACTIONS.reboot;

    render(<GlobalActionsWidgetWrapper />);

    expect(mockLocateSystem).not.toHaveBeenCalled();
  });

  test("cleans up pausedAuthAction when login modal is dismissed", () => {
    mockState.dismissedLoginModal = true;

    render(<GlobalActionsWidgetWrapper />);

    expect(mockSetPausedAuthAction).toHaveBeenCalledWith(null);
    expect(mockSetDismissedLoginModal).toHaveBeenCalledWith(false);
  });

  test("calls downloadLogs when Download logs is clicked", async () => {
    mockDownloadLogs.mockResolvedValue(undefined);

    const { container, getByText } = render(<GlobalActionsWidgetWrapper />);

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const downloadButton = getByText("Download logs").closest("button");
    fireEvent.click(downloadButton!);

    await waitFor(() => {
      expect(mockDownloadLogs).toHaveBeenCalledTimes(1);
    });
  });

  test("shows error dialog when locateSystem fails after auth", async () => {
    const errorMessage = "Failed to locate system";

    mockLocateSystem.mockImplementation(({ onError }) => {
      onError({ status: 500, error: { message: errorMessage } });
    });

    mockState.hasAccess = true;
    mockState.pausedAuthAction = AUTH_ACTIONS.locate;

    const { getByText } = render(<GlobalActionsWidgetWrapper />);

    await waitFor(() => {
      expect(getByText("Error")).toBeInTheDocument();
      expect(getByText(errorMessage)).toBeInTheDocument();
    });
  });

  test("shows error dialog when downloadLogs fails", async () => {
    mockDownloadLogs.mockRejectedValue(new Error("Download failed"));

    const { container, getByText } = render(<GlobalActionsWidgetWrapper />);

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const downloadButton = getByText("Download logs").closest("button");
    fireEvent.click(downloadButton!);

    await waitFor(() => {
      expect(getByText("Error")).toBeInTheDocument();
      expect(getByText("Failed to download logs")).toBeInTheDocument();
    });
  });

  test("closes error dialog when Close button is clicked", async () => {
    mockDownloadLogs.mockRejectedValue(new Error("Download failed"));

    const { container, getByText, queryByText } = render(<GlobalActionsWidgetWrapper />);

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const downloadButton = getByText("Download logs").closest("button");
    fireEvent.click(downloadButton!);

    await waitFor(() => {
      expect(getByText("Error")).toBeInTheDocument();
    });

    const closeButton = getByText("Close").closest("button");
    fireEvent.click(closeButton!);

    await waitFor(() => {
      expect(queryByText("Error")).not.toBeInTheDocument();
    });
  });
});
