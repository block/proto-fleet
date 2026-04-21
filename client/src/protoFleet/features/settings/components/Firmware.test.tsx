import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Firmware from "./Firmware";

const mockListFirmwareFiles = vi.fn();
const mockDeleteFirmwareFile = vi.fn();
const mockDeleteAllFirmwareFiles = vi.fn();

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({
    listFirmwareFiles: mockListFirmwareFiles,
    deleteFirmwareFile: mockDeleteFirmwareFile,
    deleteAllFirmwareFiles: mockDeleteAllFirmwareFiles,
  }),
}));

vi.mock("@/shared/features/toaster");

beforeEach(() => {
  vi.clearAllMocks();
  mockListFirmwareFiles.mockResolvedValue([]);
  mockDeleteFirmwareFile.mockResolvedValue(undefined);
  mockDeleteAllFirmwareFiles.mockResolvedValue({ deleted_count: 0 });
});

const sampleFiles = [
  { id: "f1", filename: "alpha.swu", size: 1024, uploaded_at: "2025-06-01T12:00:00Z" },
  { id: "f2", filename: "beta.tar.gz", size: 2048000, uploaded_at: "2025-06-02T14:30:00Z" },
];

describe("Firmware", () => {
  it("renders page title", async () => {
    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("Firmware")).toBeInTheDocument();
    });
  });

  it("shows loading text on mount", () => {
    mockListFirmwareFiles.mockReturnValue(new Promise(() => {}));

    const { getByText } = render(<Firmware />);

    expect(getByText("Loading firmware files...")).toBeInTheDocument();
  });

  it("renders empty state when list returns no files", async () => {
    mockListFirmwareFiles.mockResolvedValue([]);

    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("No firmware files uploaded.")).toBeInTheDocument();
    });
  });

  it("renders file list with filenames", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
      expect(getByText("beta.tar.gz")).toBeInTheDocument();
    });
  });

  it("disables Delete all button when no files exist", async () => {
    mockListFirmwareFiles.mockResolvedValue([]);

    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      const deleteAllButton = getByText("Delete all").closest("button");
      expect(deleteAllButton).toBeDisabled();
    });
  });

  it("enables Delete all button when files exist", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      const deleteAllButton = getByText("Delete all").closest("button");
      expect(deleteAllButton).not.toBeDisabled();
    });
  });

  it("opens delete confirmation dialog when per-row delete action is triggered", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getAllByText, getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    const deleteButtons = getAllByText("Delete");
    fireEvent.click(deleteButtons[0]);

    expect(getByText("Delete firmware file?")).toBeInTheDocument();
    const dialog = screen.getByTestId("delete-firmware-dialog");
    expect(within(dialog).getByText(/alpha\.swu/)).toBeInTheDocument();
    expect(mockDeleteFirmwareFile).not.toHaveBeenCalled();
  });

  it("calls deleteFirmwareFile after confirming single delete dialog", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getAllByText, getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    const deleteButtons = getAllByText("Delete");
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(getByText("Delete firmware file?")).toBeInTheDocument();
    });

    const dialog = screen.getByTestId("delete-firmware-dialog");
    const dialogDeleteButton = within(dialog).getByText("Delete");

    await act(async () => {
      fireEvent.click(dialogDeleteButton);
    });

    expect(mockDeleteFirmwareFile).toHaveBeenCalledWith("f1");
  });

  it("keeps delete dialog open and does not refresh list on delete failure", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);
    mockDeleteFirmwareFile.mockRejectedValue(new Error("Server error"));

    const { getAllByText, getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    mockListFirmwareFiles.mockClear();

    const deleteButtons = getAllByText("Delete");
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(getByText("Delete firmware file?")).toBeInTheDocument();
    });

    const dialog = screen.getByTestId("delete-firmware-dialog");
    const dialogDeleteButton = within(dialog).getByText("Delete");

    await act(async () => {
      fireEvent.click(dialogDeleteButton);
    });

    expect(mockDeleteFirmwareFile).toHaveBeenCalledWith("f1");
    expect(getByText("Delete firmware file?")).toBeInTheDocument();
    expect(mockListFirmwareFiles).not.toHaveBeenCalled();
  });

  it("opens delete-all dialog when Delete all button is clicked", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Delete all"));

    expect(getByText("Delete all firmware files?")).toBeInTheDocument();
  });

  it("calls deleteAllFirmwareFiles on dialog confirm", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);
    mockDeleteAllFirmwareFiles.mockResolvedValue({ deleted_count: 2 });

    const { getByText } = render(<Firmware />);

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Delete all"));

    await waitFor(() => {
      expect(getByText("Delete all firmware files?")).toBeInTheDocument();
    });

    const dialog = screen.getByTestId("delete-all-firmware-dialog");
    const dialogDeleteButton = within(dialog).getByText("Delete all");

    await act(async () => {
      fireEvent.click(dialogDeleteButton);
    });

    expect(mockDeleteAllFirmwareFiles).toHaveBeenCalled();
  });

  it("shows error toast when listFirmwareFiles rejects", async () => {
    const { pushToast } = await import("@/shared/features/toaster");
    mockListFirmwareFiles.mockRejectedValue(new Error("Network error"));

    render(<Firmware />);

    await waitFor(() => {
      expect(pushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Network error",
        }),
      );
    });
  });
});
