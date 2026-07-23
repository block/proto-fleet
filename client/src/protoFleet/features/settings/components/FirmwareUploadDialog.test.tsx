import type { ChangeEvent, ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FirmwareUploadDialog from "./FirmwareUploadDialog";

const mockGetMinerModelGroups = vi.fn();
const mockProcessFile = vi.fn();
const mockUseFirmwareUpload = vi.fn();
let mockSelectedFile: File;

vi.mock("@/protoFleet/api/useMinerModelGroups", () => ({
  default: () => ({ getMinerModelGroups: mockGetMinerModelGroups }),
}));

vi.mock("@/protoFleet/components/FirmwareUpload", () => ({
  useFirmwareUpload: () => mockUseFirmwareUpload(),
  FileDropZone: ({ disabled, onFileSelect }: { disabled?: boolean; onFileSelect: (file: File) => void }) => (
    <button
      type="button"
      data-testid="file-drop-zone"
      data-disabled={String(!!disabled)}
      disabled={disabled}
      onClick={() => onFileSelect(mockSelectedFile)}
    />
  ),
  FileSelectedStatus: ({ fileName, onRemove }: { fileName: string; fileSize: number; onRemove: () => void }) => (
    <div data-testid="file-selected-status">
      {fileName}
      <button type="button" onClick={onRemove}>
        Remove
      </button>
    </div>
  ),
  firmwareVersionFromFilename: (filename: string) => filename.match(/(\d+\.\d+\.\d+)/)?.[1] ?? null,
  FileErrorStatus: vi.fn(() => null),
  FileProcessingStatus: vi.fn(() => null),
}));

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: ({
    children,
    open,
    buttons,
  }: {
    children: ReactNode;
    open?: boolean;
    buttons?: Array<{ text: string; onClick?: () => void; disabled?: boolean }>;
  }) =>
    open ? (
      <div>
        {children}
        {buttons?.map((button) => (
          <button key={button.text} onClick={button.onClick} disabled={button.disabled}>
            {button.text}
          </button>
        ))}
      </div>
    ) : null,
}));

vi.mock("@/shared/components/Select", () => ({
  default: ({
    id,
    value,
    onChange,
    disabled,
  }: {
    id: string;
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <button
      type="button"
      data-testid={id}
      data-value={value}
      disabled={disabled}
      onClick={() => onChange(id.includes("manufacturer") ? "Proto" : "Rig")}
    >
      {id}
    </button>
  ),
}));

vi.mock("@/shared/components/Input", () => ({
  default: ({
    id,
    initValue,
    onChange,
    disabled,
  }: {
    id: string;
    initValue?: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <input
      data-testid={id}
      value={initValue ?? ""}
      disabled={disabled}
      onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(event.target.value)}
    />
  ),
}));

describe("FirmwareUploadDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSelectedFile = new File(["firmware"], "update-2.0.0.swu");
    mockGetMinerModelGroups.mockResolvedValue([{ manufacturer: "Proto", model: "Rig", count: 1 }]);
    mockUseFirmwareUpload.mockReturnValue({
      state: "idle",
      file: null,
      firmwareFileId: null,
      uploadProgress: 0,
      errorMessage: null,
      serverConfig: { allowedExtensions: [".swu"] },
      processFile: mockProcessFile,
      reset: vi.fn(),
      retry: vi.fn(),
    });
  });

  it("selects a file before metadata is complete and infers its version", async () => {
    render(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    const dropZone = await screen.findByTestId("file-drop-zone");
    expect(dropZone).toHaveAttribute("data-disabled", "false");
    fireEvent.click(dropZone);

    expect(screen.getByTestId("file-selected-status")).toHaveTextContent("update-2.0.0.swu");
    expect(screen.getByTestId("firmware-version")).toHaveValue("2.0.0");
    expect(screen.getByText("Upload")).toBeDisabled();
    expect(mockProcessFile).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));

    expect(screen.getByText("Upload")).not.toBeDisabled();
    fireEvent.click(screen.getByText("Upload"));

    expect(mockProcessFile).toHaveBeenCalledWith(
      mockSelectedFile,
      {
        targetManufacturer: "Proto",
        targetModel: "Rig",
        firmwareVersion: "2.0.0",
      },
      expect.any(Function),
    );
  });

  it("does not replace a version entered before file selection", async () => {
    render(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    await screen.findByTestId("file-drop-zone");
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "9.0.0" } });
    fireEvent.click(screen.getByTestId("file-drop-zone"));

    expect(screen.getByTestId("firmware-version")).toHaveValue("9.0.0");
  });

  it("keeps upload unavailable when the filename has no version and metadata is incomplete", async () => {
    mockSelectedFile = new File(["firmware"], "update.swu");
    render(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    await screen.findByTestId("file-drop-zone");
    fireEvent.click(screen.getByTestId("file-drop-zone"));
    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));

    expect(screen.getByTestId("firmware-version")).toHaveValue("");
    expect(screen.getByText("Upload")).toBeDisabled();
    expect(mockProcessFile).not.toHaveBeenCalled();
  });

  it("closes and refreshes after the upload becomes ready", async () => {
    const onSuccess = vi.fn();
    render(<FirmwareUploadDialog open onSuccess={onSuccess} onDismiss={vi.fn()} />);

    await screen.findByTestId("firmware-target-manufacturer");
    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.0.0" } });
    fireEvent.click(screen.getByTestId("file-drop-zone"));
    fireEvent.click(screen.getByText("Upload"));

    const onReady = mockProcessFile.mock.calls[0]?.[2] as (() => void) | undefined;
    onReady?.();

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledOnce();
    });
    expect(screen.queryByText("Done")).not.toBeInTheDocument();
  });
});
