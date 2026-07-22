import type { ChangeEvent, ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FirmwareUploadDialog from "./FirmwareUploadDialog";

const mockGetMinerModelGroups = vi.fn();
const mockProcessFile = vi.fn();

vi.mock("@/protoFleet/api/useMinerModelGroups", () => ({
  default: () => ({ getMinerModelGroups: mockGetMinerModelGroups }),
}));

vi.mock("@/protoFleet/components/FirmwareUpload", () => ({
  useFirmwareUpload: () => ({
    state: "idle",
    file: null,
    uploadProgress: 0,
    errorMessage: null,
    serverConfig: { allowedExtensions: [".swu"] },
    processFile: mockProcessFile,
    reset: vi.fn(),
    retry: vi.fn(),
  }),
  FileDropZone: ({ disabled }: { disabled?: boolean }) => (
    <div data-testid="file-drop-zone" data-disabled={String(!!disabled)} />
  ),
  FileErrorStatus: vi.fn(() => null),
  FileProcessingStatus: vi.fn(() => null),
  FileReadyStatus: vi.fn(() => null),
}));

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: ({ children, open }: { children: ReactNode; open?: boolean }) => (open ? <div>{children}</div> : null),
}));

vi.mock("@/shared/components/Select", () => ({
  default: ({ id, onChange, disabled }: { id: string; onChange: (value: string) => void; disabled?: boolean }) => (
    <button
      type="button"
      data-testid={id}
      disabled={disabled}
      onClick={() => onChange(id.includes("manufacturer") ? "Proto" : "Rig")}
    >
      {id}
    </button>
  ),
}));

vi.mock("@/shared/components/Input", () => ({
  default: ({ id, onChange }: { id: string; onChange: (value: string) => void }) => (
    <input
      data-testid={id}
      onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(event.target.value)}
    />
  ),
}));

describe("FirmwareUploadDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetMinerModelGroups.mockResolvedValue([{ manufacturer: "Proto", model: "Rig", count: 1 }]);
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
});
