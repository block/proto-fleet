import type { ChangeEvent, ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FirmwareUploadDialog from "./FirmwareUploadDialog";

const mockGetMinerModelGroups = vi.fn();
const mockProcessFile = vi.fn();
const mockUseFirmwareUpload = vi.fn();
const mockUpdateFirmwareMetadata = vi.fn();

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ updateFirmwareMetadata: mockUpdateFirmwareMetadata }),
}));

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
      onClick={() => onFileSelect(new File(["firmware"], "update.swu"))}
    />
  ),
  FileErrorStatus: vi.fn(() => null),
  FileProcessingStatus: vi.fn(() => null),
  FileReadyStatus: vi.fn(() => <div data-testid="file-ready-status" />),
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
    mockUpdateFirmwareMetadata.mockResolvedValue(undefined);
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

  it("disables file selection until manufacturer, model, and version are complete", async () => {
    render(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    const dropZone = await screen.findByTestId("file-drop-zone");
    expect(dropZone).toHaveAttribute("data-disabled", "true");

    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.0.0" } });

    expect(screen.getByTestId("file-drop-zone")).toHaveAttribute("data-disabled", "false");
  });

  it("keeps uploaded firmware metadata visible and editable", async () => {
    const view = render(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    await screen.findByTestId("firmware-target-manufacturer");
    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.0.0" } });
    fireEvent.click(screen.getByTestId("file-drop-zone"));

    mockUseFirmwareUpload.mockReturnValue({
      state: "ready",
      file: new File(["firmware"], "update.swu"),
      firmwareFileId: "firmware-1",
      uploadProgress: 100,
      errorMessage: null,
      serverConfig: { allowedExtensions: [".swu"] },
      processFile: mockProcessFile,
      reset: vi.fn(),
      retry: vi.fn(),
    });
    view.rerender(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    expect(await screen.findByTestId("firmware-target-manufacturer")).not.toBeDisabled();
    expect(screen.getByTestId("firmware-target-manufacturer")).toHaveAttribute("data-value", "Proto");
    expect(screen.getByTestId("firmware-target-model")).not.toBeDisabled();
    expect(screen.getByTestId("firmware-target-model")).toHaveAttribute("data-value", "Rig");
    expect(screen.getByTestId("firmware-version")).not.toBeDisabled();
    expect(screen.getByTestId("firmware-version")).toHaveValue("2.0.0");
    expect(screen.getByTestId("file-ready-status")).toBeInTheDocument();
    expect(screen.queryByTestId("file-drop-zone")).not.toBeInTheDocument();
    expect(screen.queryByText("Edit metadata")).not.toBeInTheDocument();
    expect(screen.getByText("Done")).toBeInTheDocument();
  });

  it("updates changed metadata when Done is selected", async () => {
    const onSuccess = vi.fn();
    const view = render(<FirmwareUploadDialog open onSuccess={onSuccess} onDismiss={vi.fn()} />);

    await screen.findByTestId("firmware-target-manufacturer");
    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.0.0" } });
    fireEvent.click(screen.getByTestId("file-drop-zone"));

    mockUseFirmwareUpload.mockReturnValue({
      state: "ready",
      file: new File(["firmware"], "update.swu"),
      firmwareFileId: "firmware-1",
      uploadProgress: 100,
      errorMessage: null,
      serverConfig: { allowedExtensions: [".swu"] },
      processFile: mockProcessFile,
      reset: vi.fn(),
      retry: vi.fn(),
    });
    view.rerender(<FirmwareUploadDialog open onSuccess={onSuccess} onDismiss={vi.fn()} />);

    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.1.0" } });
    fireEvent.click(screen.getByText("Done"));

    await waitFor(() => {
      expect(mockUpdateFirmwareMetadata).toHaveBeenCalledWith("firmware-1", {
        targetManufacturer: "Proto",
        targetModel: "Rig",
        firmwareVersion: "2.1.0",
      });
      expect(onSuccess).toHaveBeenCalledOnce();
    });
  });

  it("closes without updating unchanged metadata when Done is selected", async () => {
    const onSuccess = vi.fn();
    const view = render(<FirmwareUploadDialog open onSuccess={onSuccess} onDismiss={vi.fn()} />);

    await screen.findByTestId("firmware-target-manufacturer");
    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.0.0" } });
    fireEvent.click(screen.getByTestId("file-drop-zone"));

    mockUseFirmwareUpload.mockReturnValue({
      state: "ready",
      file: new File(["firmware"], "update.swu"),
      firmwareFileId: "firmware-1",
      uploadProgress: 100,
      errorMessage: null,
      serverConfig: { allowedExtensions: [".swu"] },
      processFile: mockProcessFile,
      reset: vi.fn(),
      retry: vi.fn(),
    });
    view.rerender(<FirmwareUploadDialog open onSuccess={onSuccess} onDismiss={vi.fn()} />);

    fireEvent.click(screen.getByText("Done"));

    expect(mockUpdateFirmwareMetadata).not.toHaveBeenCalled();
    expect(onSuccess).toHaveBeenCalledOnce();
  });

  it("keeps Done unavailable while changed metadata is incomplete", async () => {
    const view = render(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    await screen.findByTestId("firmware-target-manufacturer");
    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));
    fireEvent.click(screen.getByTestId("firmware-target-model"));
    fireEvent.change(screen.getByTestId("firmware-version"), { target: { value: "2.0.0" } });
    fireEvent.click(screen.getByTestId("file-drop-zone"));

    mockUseFirmwareUpload.mockReturnValue({
      state: "ready",
      file: new File(["firmware"], "update.swu"),
      firmwareFileId: "firmware-1",
      uploadProgress: 100,
      errorMessage: null,
      serverConfig: { allowedExtensions: [".swu"] },
      processFile: mockProcessFile,
      reset: vi.fn(),
      retry: vi.fn(),
    });
    view.rerender(<FirmwareUploadDialog open onSuccess={vi.fn()} onDismiss={vi.fn()} />);

    fireEvent.click(screen.getByTestId("firmware-target-manufacturer"));

    expect(screen.getByText("Done")).toBeDisabled();
  });
});
