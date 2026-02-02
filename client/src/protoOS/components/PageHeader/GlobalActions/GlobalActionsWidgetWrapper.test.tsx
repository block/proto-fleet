import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import GlobalActionsWidgetWrapper from "./GlobalActionsWidgetWrapper";
import { useDownloadLogs } from "@/protoOS/api/hooks/useDownloadLogs";
import { useLocateSystem } from "@/protoOS/api/hooks/useLocateSystem";

vi.mock("@/protoOS/api/hooks/useLocateSystem", () => ({
  useLocateSystem: vi.fn(),
}));

vi.mock("@/protoOS/api/hooks/useDownloadLogs", () => ({
  useDownloadLogs: vi.fn(),
}));

describe("GlobalActionsWidgetWrapper", () => {
  const mockLocateSystem = vi.fn();
  const mockDownloadLogs = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();

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

  test("calls locateSystem with correct parameters when Blink LEDs is clicked", () => {
    const { container, getByText } = render(<GlobalActionsWidgetWrapper />);

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const blinkButton = getByText("Blink LEDs").closest("button");
    fireEvent.click(blinkButton!);

    expect(mockLocateSystem).toHaveBeenCalledTimes(1);
    expect(mockLocateSystem).toHaveBeenCalledWith({
      ledOnTime: 30,
      onError: expect.any(Function),
    });
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

  test("shows error dialog when locateSystem fails", async () => {
    const errorMessage = "Failed to locate system";

    mockLocateSystem.mockImplementation(({ onError }) => {
      onError({ status: 500, error: { message: errorMessage } });
    });

    const { container, getByText } = render(<GlobalActionsWidgetWrapper />);

    const ellipsisButton = container.querySelector("button");
    fireEvent.click(ellipsisButton!);

    const blinkButton = getByText("Blink LEDs").closest("button");
    fireEvent.click(blinkButton!);

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
