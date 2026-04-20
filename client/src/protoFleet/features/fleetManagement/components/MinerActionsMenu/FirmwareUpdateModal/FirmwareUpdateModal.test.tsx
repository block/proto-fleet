import type { ReactNode } from "react";
import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FirmwareUpdateModal from "./FirmwareUpdateModal";

const mockListFirmwareFiles = vi.fn();
const mockUseFirmwareUpload = vi.fn();

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({
    listFirmwareFiles: mockListFirmwareFiles,
  }),
}));

vi.mock("@/protoFleet/components/FirmwareUpload", () => ({
  useFirmwareUpload: () => mockUseFirmwareUpload(),
  FileDropZone: vi.fn(() => <div data-testid="file-drop-zone" />),
  FileErrorStatus: vi.fn(({ message }: { message: string }) => <div data-testid="file-error-status">{message}</div>),
  FileProcessingStatus: vi.fn(() => <div data-testid="file-processing-status" />),
  FileReadyStatus: vi.fn(() => <div data-testid="file-ready-status" />),
}));

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: vi.fn(({ children, open, title }: { children: ReactNode; open?: boolean; title?: string }) => {
    if (open === false) return null;
    return (
      <div data-testid="modal">
        <div>{title}</div>
        {children}
      </div>
    );
  }),
}));

vi.mock("@/shared/components/ProgressCircular/ProgressCircular", () => ({
  default: vi.fn(() => <div data-testid="progress-circular" />),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { error: "error" },
}));

describe("FirmwareUpdateModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseFirmwareUpload.mockReturnValue({
      state: "idle",
      file: null,
      firmwareFileId: null,
      uploadProgress: 0,
      errorMessage: null,
      serverConfig: null,
      processFile: vi.fn(),
      reset: vi.fn(),
      retry: vi.fn(),
    });
  });

  it("keeps showing the loading spinner when the file list resolves empty before config loads", async () => {
    let resolveFiles: ((files: Array<unknown>) => void) | undefined;
    mockListFirmwareFiles.mockReturnValue(
      new Promise<Array<unknown>>((resolve) => {
        resolveFiles = resolve;
      }),
    );

    render(<FirmwareUpdateModal open onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(screen.getByTestId("progress-circular")).toBeInTheDocument();

    await act(async () => {
      resolveFiles?.([]);
      await Promise.resolve();
    });

    expect(screen.getByTestId("progress-circular")).toBeInTheDocument();
    expect(screen.queryByTestId("file-drop-zone")).not.toBeInTheDocument();
  });

  it("renders existing files immediately even while config is still loading", async () => {
    mockListFirmwareFiles.mockResolvedValue([
      { id: "fw-1", filename: "alpha.swu", size: 1024, uploaded_at: "2025-01-01T00:00:00Z" },
    ]);

    render(<FirmwareUpdateModal open onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(await screen.findByText("Select an existing firmware file")).toBeInTheDocument();
    expect(screen.getByText("alpha.swu")).toBeInTheDocument();
  });
});
