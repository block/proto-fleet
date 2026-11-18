import type { ComponentProps } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import FirmwareUpdateModal from "./FirmwareUpdateModal";
import { FileDropZone } from "@/protoFleet/components/FirmwareUpload";
import { uploadHookState } from "@/protoFleet/components/FirmwareUpload/useFirmwareUpload.fixtures";
import type Modal from "@/shared/components/Modal/Modal";

const mockListFirmwareFiles = vi.fn();
const mockUseFirmwareUpload = vi.fn();
const target = { targetManufacturer: "Proto", targetModel: "Rig" };

vi.mock("@/protoFleet/api/useFirmwareApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/api/useFirmwareApi")>()),
  useFirmwareApi: () => ({
    listFirmwareFiles: mockListFirmwareFiles,
  }),
}));

vi.mock("@/protoFleet/components/FirmwareUpload", async () => ({
  HashboardBuildField: (
    await vi.importActual<typeof import("@/protoFleet/components/FirmwareUpload/HashboardBuildField")>(
      "@/protoFleet/components/FirmwareUpload/HashboardBuildField",
    )
  ).default,
  useFirmwareUpload: () => mockUseFirmwareUpload(),
  FileDropZone: vi.fn(() => <div data-testid="file-drop-zone" />),
  FileErrorStatus: vi.fn(({ message }: { message: string }) => <div data-testid="file-error-status">{message}</div>),
  FileProcessingStatus: vi.fn(() => <div data-testid="file-processing-status" />),
  FileReadyStatus: vi.fn(() => <div data-testid="file-ready-status" />),
}));

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: vi.fn(({ children, open, title, buttons }: ComponentProps<typeof Modal>) => {
    if (open === false) return null;
    return (
      <div data-testid="modal">
        <div>{title}</div>
        {children}
        {buttons?.map(({ text, disabled, onClick }) => (
          <button key={text} disabled={disabled} onClick={onClick}>
            {text}
          </button>
        ))}
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
    mockUseFirmwareUpload.mockReturnValue(uploadHookState());
  });

  it("keeps showing the loading spinner when the file list resolves empty before config loads", async () => {
    let resolveFiles: ((files: Array<unknown>) => void) | undefined;
    mockListFirmwareFiles.mockReturnValue(
      new Promise<Array<unknown>>((resolve) => {
        resolveFiles = resolve;
      }),
    );

    render(<FirmwareUpdateModal open target={target} onConfirm={vi.fn()} onDismiss={vi.fn()} />);

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
      {
        id: "fw-1",
        filename: "alpha.swu",
        size: 1024,
        uploaded_at: "2025-01-01T00:00:00Z",
        target_manufacturer: "Proto",
        target_model: "Rig",
      },
    ]);

    render(<FirmwareUpdateModal open target={target} onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(await screen.findByText("Select an existing firmware file")).toBeInTheDocument();
    expect(screen.getByText("alpha.swu")).toBeInTheDocument();
  });

  it("enables Continue only after selecting an existing payload and submits its ID", async () => {
    mockListFirmwareFiles.mockResolvedValue([
      {
        id: "fw-1",
        filename: "alpha.swu",
        size: 1024,
        uploaded_at: "2025-01-01T00:00:00Z",
        target_manufacturer: "Proto",
        target_model: "Rig",
      },
    ]);
    const onConfirm = vi.fn();
    render(<FirmwareUpdateModal open target={target} onConfirm={onConfirm} onDismiss={vi.fn()} />);

    const continueButton = screen.getByRole("button", { name: "Continue" });
    expect(continueButton).toBeDisabled();
    fireEvent.click(continueButton);
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.click(await screen.findByRole("radio", { name: /alpha.swu/ }));
    expect(continueButton).toBeEnabled();
    fireEvent.click(continueButton);
    expect(onConfirm).toHaveBeenCalledExactlyOnceWith("fw-1");
    expect(continueButton).toBeDisabled();
  });

  it.each(["idle", "hashing", "checking", "uploading", "error"] as const)(
    "disables Continue while an uploaded payload is %s and enables it when ready",
    (state) => {
      mockListFirmwareFiles.mockResolvedValue([]);
      const file = new File(["firmware"], "update.swu");
      const onConfirm = vi.fn();
      const upload = uploadHookState({ state, file, serverConfig: { allowedExtensions: [".swu"] } });
      mockUseFirmwareUpload.mockReturnValue(upload);
      const view = render(<FirmwareUpdateModal open target={target} onConfirm={onConfirm} onDismiss={vi.fn()} />);

      const continueButton = screen.getByRole("button", { name: "Continue" });
      expect(continueButton).toBeDisabled();
      fireEvent.click(continueButton);
      expect(onConfirm).not.toHaveBeenCalled();

      mockUseFirmwareUpload.mockReturnValue({ ...upload, state: "ready", firmwareFileId: "fw-uploaded" });
      view.rerender(<FirmwareUpdateModal open target={target} onConfirm={onConfirm} onDismiss={vi.fn()} />);
      expect(continueButton).toBeEnabled();
      fireEvent.click(continueButton);
      expect(onConfirm).toHaveBeenCalledExactlyOnceWith("fw-uploaded");
    },
  );

  it("filters existing files to the selected miner target", async () => {
    mockListFirmwareFiles.mockResolvedValue([
      {
        id: "fw-1",
        filename: "alpha.swu",
        size: 1024,
        uploaded_at: "2025-01-01T00:00:00Z",
        target_manufacturer: "Proto",
        target_model: "Rig",
      },
      {
        id: "fw-2",
        filename: "beta.swu",
        size: 1024,
        uploaded_at: "2025-01-01T00:00:00Z",
        target_manufacturer: "Bitmain",
        target_model: "S21",
      },
      {
        id: "legacy",
        filename: "legacy.swu",
        size: 1024,
        uploaded_at: "2025-01-01T00:00:00Z",
        target_manufacturer: "",
        target_model: "",
      },
    ]);

    render(
      <FirmwareUpdateModal
        open
        target={{ targetManufacturer: " proto ", targetModel: "rig" }}
        onConfirm={vi.fn()}
        onDismiss={vi.fn()}
      />,
    );

    expect(await screen.findByText("alpha.swu")).toBeInTheDocument();
    expect(screen.queryByText("beta.swu")).not.toBeInTheDocument();
    expect(screen.queryByText("legacy.swu")).not.toBeInTheDocument();
  });

  it("explains that miners reboot automatically after firmware installation", () => {
    mockListFirmwareFiles.mockResolvedValue([]);

    render(<FirmwareUpdateModal open target={target} onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(screen.getByText(/reboot automatically after installation completes/i)).toBeInTheDocument();
  });

  it("keeps uploaded firmware metadata visible and read-only", async () => {
    mockListFirmwareFiles.mockResolvedValue([]);
    mockUseFirmwareUpload.mockReturnValue(uploadHookState({ serverConfig: { allowedExtensions: [".swu"] } }));
    const view = render(<FirmwareUpdateModal open target={target} onConfirm={vi.fn()} onDismiss={vi.fn()} />);
    fireEvent.change(await screen.findByLabelText("Firmware version"), { target: { value: "2.0.0" } });

    mockUseFirmwareUpload.mockReturnValue(
      uploadHookState({
        state: "ready",
        file: new File(["firmware"], "update.swu"),
        firmwareFileId: "fw-uploaded",
        uploadProgress: 100,
        serverConfig: { allowedExtensions: [".swu"] },
      }),
    );
    view.rerender(<FirmwareUpdateModal open target={target} onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(await screen.findByLabelText("Product")).toBeDisabled();
    expect(screen.getByLabelText("Model")).toBeDisabled();
    expect(screen.getByLabelText("Firmware version")).toBeDisabled();
    expect(screen.getByLabelText("Firmware version")).toHaveValue("2.0.0");
    expect(screen.getByTestId("file-ready-status")).toBeInTheDocument();
    expect(screen.queryByTestId("file-drop-zone")).not.toBeInTheDocument();
  });

  describe("CT1 PSU firmware", () => {
    const ct1 = { targetManufacturer: "Proto", targetModel: "CT1" };
    const psuUpdate = /one module per controller at a time/;

    it("names an existing capsule, explains its update, and reports the component", async () => {
      mockListFirmwareFiles.mockResolvedValue([
        {
          id: "fw-boards",
          filename: "hashboard-firmware-2.0.0-prod.zip",
          size: 1024,
          uploaded_at: "2025-01-01T00:00:00Z",
          target_manufacturer: "Proto",
          target_model: "CT1",
          firmware_version: "2.0.0",
          hashboard_build: "prod",
        },
        {
          id: "fw-psu",
          filename: "psu-firmware-boco-HA1200000-0123456789ab.psucap",
          size: 61000,
          uploaded_at: "2025-01-02T00:00:00Z",
          target_manufacturer: "Proto",
          target_model: "CT1",
          firmware_version: "app=1410;bl=1110",
          component: "psu",
        },
      ]);
      const onConfirm = vi.fn();
      render(<FirmwareUpdateModal open target={ct1} onConfirm={onConfirm} onDismiss={vi.fn()} />);

      fireEvent.click(await screen.findByRole("radio", { name: /Proto CT1 · 2\.0\.0/ }));
      expect(screen.getByText(/Update all hashboards in this container/)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("radio", { name: /Proto CT1 PSU · app=1410;bl=1110/ }));
      expect(screen.getByText(psuUpdate)).toBeInTheDocument();
      expect(screen.queryByText(/Update all hashboards in this container/)).not.toBeInTheDocument();

      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      expect(onConfirm).toHaveBeenCalledExactlyOnceWith("fw-psu", "psu");
    });

    it("uploads a capsule as PSU firmware without a hashboard build", async () => {
      mockListFirmwareFiles.mockResolvedValue([]);
      const processFile = vi.fn();
      const serverConfig = { allowedExtensions: [".zip", ".psucap"] };
      mockUseFirmwareUpload.mockReturnValue(uploadHookState({ serverConfig, processFile }));
      const onConfirm = vi.fn();
      const view = render(<FirmwareUpdateModal open target={ct1} onConfirm={onConfirm} onDismiss={vi.fn()} />);
      fireEvent.change(await screen.findByLabelText("Firmware version"), { target: { value: "app=1410;bl=1110" } });
      expect(screen.getByText("Hashboard build")).toBeInTheDocument();

      const capsule = new File(["capsule"], "boco.psucap");
      act(() => vi.mocked(FileDropZone).mock.lastCall![0].onFileSelect(capsule));
      expect(processFile).toHaveBeenCalledExactlyOnceWith(capsule, {
        targetManufacturer: "Proto",
        targetModel: "CT1",
        firmwareVersion: "app=1410;bl=1110",
        hashboardBuild: undefined,
        component: "psu",
      });

      mockUseFirmwareUpload.mockReturnValue(
        uploadHookState({ state: "ready", file: capsule, firmwareFileId: "fw-new", serverConfig, processFile }),
      );
      view.rerender(<FirmwareUpdateModal open target={ct1} onConfirm={onConfirm} onDismiss={vi.fn()} />);
      expect(screen.queryByText("Hashboard build")).not.toBeInTheDocument();
      expect(screen.getByText(psuUpdate)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Continue" }));
      expect(onConfirm).toHaveBeenCalledExactlyOnceWith("fw-new", "psu");
    });
  });

  it("fails closed when the selected miner target is unavailable", async () => {
    mockListFirmwareFiles.mockResolvedValue([
      {
        id: "fw-1",
        filename: "alpha.swu",
        size: 1024,
        uploaded_at: "2025-01-01T00:00:00Z",
        target_manufacturer: "Proto",
        target_model: "Rig",
      },
    ]);

    render(<FirmwareUpdateModal open onConfirm={vi.fn()} onDismiss={vi.fn()} />);

    expect(screen.getByText(/unable to determine the selected miners/i)).toBeInTheDocument();
    expect(screen.queryByText("alpha.swu")).not.toBeInTheDocument();
    expect(screen.queryByTestId("file-drop-zone")).not.toBeInTheDocument();
  });
});
