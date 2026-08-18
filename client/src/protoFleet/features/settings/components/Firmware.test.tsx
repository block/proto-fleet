import { MemoryRouter, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Firmware from "./Firmware";

const mockListFirmwareFiles = vi.fn();
const mockDeleteFirmwareFile = vi.fn();
const mockDeleteAllFirmwareFiles = vi.fn();
const mockUpdateFirmwareMetadata = vi.fn();

vi.mock("@/protoFleet/api/useFirmwareApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFirmwareApi")>()),
  useFirmwareApi: () => ({
    listFirmwareFiles: mockListFirmwareFiles,
    updateFirmwareMetadata: mockUpdateFirmwareMetadata,
    deleteFirmwareFile: mockDeleteFirmwareFile,
    deleteAllFirmwareFiles: mockDeleteAllFirmwareFiles,
  }),
}));

vi.mock("@/protoFleet/features/settings/components/EditFirmwareMetadataDialog", () => ({
  default: ({ open, file, onConfirm }: any) =>
    open ? (
      <div data-testid="edit-firmware-metadata-dialog">
        <span>{file.filename}</span>
        <button
          onClick={() => onConfirm({ targetManufacturer: "Proto", targetModel: "Rig", firmwareVersion: "2.0.0" })}
        >
          Save changes
        </button>
      </div>
    ) : null,
}));

vi.mock("@/shared/features/toaster");

vi.mock("@/protoFleet/features/rollout/betweenChannel/RolloutLanesTab", () => ({
  default: () => <div>Stable rollout lanes</div>,
}));

const LocationProbe = () => {
  const location = useLocation();
  return <div data-testid="location-probe">{`${location.pathname}${location.search}`}</div>;
};

const renderFirmware = (initialEntry = "/settings/firmware") =>
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Firmware />
      <LocationProbe />
    </MemoryRouter>,
  );

beforeEach(() => {
  vi.clearAllMocks();
  mockListFirmwareFiles.mockResolvedValue([]);
  mockDeleteFirmwareFile.mockResolvedValue(undefined);
  mockUpdateFirmwareMetadata.mockResolvedValue(undefined);
  mockDeleteAllFirmwareFiles.mockResolvedValue({ deleted_count: 0 });
});

const sampleFiles = [
  {
    id: "f1",
    filename: "alpha.swu",
    size: 1024,
    uploaded_at: "2025-06-01T12:00:00Z",
    target_manufacturer: "Proto",
    target_model: "Rig",
  },
  {
    id: "f2",
    filename: "beta.tar.gz",
    size: 2048000,
    uploaded_at: "2025-06-02T14:30:00Z",
    target_manufacturer: "Bitmain",
    target_model: "S19",
  },
];

describe("Firmware", () => {
  it("keeps files and stable rollout lanes under the existing firmware route", async () => {
    renderFirmware();

    expect(screen.getByRole("button", { name: "Files" })).toHaveAttribute("aria-current", "page");
    fireEvent.click(screen.getByRole("button", { name: "Rollout lanes" }));

    expect(screen.getByText("Stable rollout lanes")).toBeInTheDocument();
    expect(screen.queryByText("No firmware files uploaded")).not.toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?tab=rolloutLanes");
  });

  it("restores rollout lanes from the URL and Files clears rollout state while preserving other params", () => {
    const initialEntry = "/settings/firmware?site=alpha&tab=rolloutLanes&setupLane=lane-123";
    const firstView = renderFirmware(initialEntry);

    expect(screen.getByRole("button", { name: "Rollout lanes" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByText("Stable rollout lanes")).toBeInTheDocument();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(initialEntry);

    firstView.unmount();
    renderFirmware(initialEntry);
    expect(screen.getByRole("button", { name: "Rollout lanes" })).toHaveAttribute("aria-current", "page");

    fireEvent.click(screen.getByRole("button", { name: "Files" }));

    expect(screen.getByRole("button", { name: "Files" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/settings/firmware?site=alpha");
  });

  it("renders page title", async () => {
    const { getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("Firmware")).toBeInTheDocument();
    });
  });

  it("shows loading text on mount", () => {
    mockListFirmwareFiles.mockReturnValue(new Promise(() => {}));

    const { getByText } = renderFirmware();

    expect(getByText("Loading firmware files...")).toBeInTheDocument();
  });

  it("renders empty state when list returns no files", async () => {
    mockListFirmwareFiles.mockResolvedValue([]);

    const { getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("No firmware files uploaded")).toBeInTheDocument();
      expect(getByText("Upload firmware before deploying updates to your fleet.")).toBeInTheDocument();
    });
  });

  it("renders file list with filenames", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
      expect(getByText("beta.tar.gz")).toBeInTheDocument();
    });
  });

  it("expands and collapses a truncated filename", async () => {
    const filename = "proto-rig-firmware-with-a-very-long-release-name-2.0.0.swu";
    mockListFirmwareFiles.mockResolvedValue([{ ...sampleFiles[0], filename }]);
    const scrollWidthSpy = vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(500);
    const clientWidthSpy = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(200);

    renderFirmware();

    const expandButton = await screen.findByRole("button", { name: `Show full file name: ${filename}` });
    expect(expandButton).toHaveAttribute("aria-expanded", "false");
    expect(within(expandButton).getByText(filename)).toHaveClass("truncate");

    fireEvent.click(expandButton);

    const collapseButton = screen.getByRole("button", { name: `Hide full file name: ${filename}` });
    expect(collapseButton).toHaveAttribute("aria-expanded", "true");
    expect(within(collapseButton).getByText(filename)).not.toHaveClass("truncate");

    fireEvent.click(collapseButton);

    expect(screen.getByRole("button", { name: `Show full file name: ${filename}` })).toHaveAttribute(
      "aria-expanded",
      "false",
    );

    scrollWidthSpy.mockRestore();
    clientWidthSpy.mockRestore();
  });

  it("does not show an expand control when the full filename fits", async () => {
    const filename = "firmware-2.0.0.swu";
    mockListFirmwareFiles.mockResolvedValue([{ ...sampleFiles[0], filename }]);
    const scrollWidthSpy = vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(150);
    const clientWidthSpy = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(384);

    renderFirmware();

    expect(await screen.findByText(filename, { selector: "span:not([aria-hidden])" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: `Show full file name: ${filename}` })).not.toBeInTheDocument();

    scrollWidthSpy.mockRestore();
    clientWidthSpy.mockRestore();
  });

  it("updates metadata from the row action", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);
    renderFirmware();

    await screen.findByText("alpha.swu");
    fireEvent.click(screen.getAllByTestId("list-actions-trigger")[0]);
    fireEvent.click(await screen.findByText("Edit metadata"));
    expect(screen.getByTestId("edit-firmware-metadata-dialog")).toHaveTextContent("alpha.swu");

    await act(async () => {
      fireEvent.click(screen.getByText("Save changes"));
    });

    expect(mockUpdateFirmwareMetadata).toHaveBeenCalledWith("f1", {
      targetManufacturer: "Proto",
      targetModel: "Rig",
      firmwareVersion: "2.0.0",
    });
  });

  it("renders legacy firmware targets as unknown", async () => {
    mockListFirmwareFiles.mockResolvedValue([
      {
        id: "legacy",
        filename: "legacy.swu",
        size: 1024,
        uploaded_at: "2025-06-01T12:00:00Z",
        target_manufacturer: "",
        target_model: "",
      },
    ]);

    renderFirmware();

    expect(await screen.findByText("Unknown")).toBeInTheDocument();
  });

  it("hides Delete all button when no files exist", async () => {
    mockListFirmwareFiles.mockResolvedValue([]);

    const { queryByText } = renderFirmware();

    await waitFor(() => {
      expect(queryByText("Delete all")).not.toBeInTheDocument();
    });
  });

  it("enables Delete all button when files exist", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getByText } = renderFirmware();

    await waitFor(() => {
      const deleteAllButton = getByText("Delete all").closest("button");
      expect(deleteAllButton).not.toBeDisabled();
    });
  });

  it("opens delete confirmation dialog when per-row delete action is triggered", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getAllByText, getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    fireEvent.click(screen.getAllByTestId("list-actions-trigger")[0]);
    const deleteButtons = getAllByText("Delete");
    fireEvent.click(deleteButtons[0]);

    expect(getByText("Delete firmware file?")).toBeInTheDocument();
    const dialog = screen.getByTestId("delete-firmware-dialog");
    expect(within(dialog).getByText(/alpha\.swu/)).toBeInTheDocument();
    expect(mockDeleteFirmwareFile).not.toHaveBeenCalled();
  });

  it("calls deleteFirmwareFile after confirming single delete dialog", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);

    const { getAllByText, getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    fireEvent.click(screen.getAllByTestId("list-actions-trigger")[0]);
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

    const { getAllByText, getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    mockListFirmwareFiles.mockClear();

    fireEvent.click(screen.getAllByTestId("list-actions-trigger")[0]);
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

    const { getByText } = renderFirmware();

    await waitFor(() => {
      expect(getByText("alpha.swu")).toBeInTheDocument();
    });

    fireEvent.click(getByText("Delete all"));

    expect(getByText("Delete all firmware files?")).toBeInTheDocument();
  });

  it("calls deleteAllFirmwareFiles on dialog confirm", async () => {
    mockListFirmwareFiles.mockResolvedValue(sampleFiles);
    mockDeleteAllFirmwareFiles.mockResolvedValue({ deleted_count: 2 });

    const { getByText } = renderFirmware();

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

    renderFirmware();

    await waitFor(() => {
      expect(pushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Network error",
        }),
      );
    });
  });
});
