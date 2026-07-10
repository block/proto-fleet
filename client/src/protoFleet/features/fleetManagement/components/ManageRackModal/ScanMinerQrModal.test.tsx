import type { ComponentProps } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ScanMinerQrModal from "./ScanMinerQrModal";
import { MinerIdentifierType, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// --- Mock the scanner hook so tests never touch real camera/WASM APIs. ---
const mockUseQrScanner = vi.fn();
const mockCanUseLiveCamera = vi.fn();
let capturedOnDetected: ((raws: string[]) => void) | undefined;
let capturedScannerOptions: { onDetected: (raws: string[]) => void; active: boolean; restartKey?: number } | undefined;

vi.mock("@/protoFleet/features/fleetManagement/hooks/useQrScanner", () => ({
  canUseLiveCamera: () => mockCanUseLiveCamera(),
  useQrScanner: (opts: { onDetected: (raws: string[]) => void; active: boolean; restartKey?: number }) => {
    capturedOnDetected = opts.onDetected;
    capturedScannerOptions = opts;
    return mockUseQrScanner(opts);
  },
}));

// --- Mock the serial lookup so we control found / notFound / error. ---
const mockLookup = vi.fn();
vi.mock("@/protoFleet/api/lookupMinerByIdentifier", () => ({
  lookupMinerByIdentifier: (...args: unknown[]) => mockLookup(...args),
}));

// Lightweight Modal stub that renders children + buttons.
vi.mock("@/shared/components/Modal", () => ({
  default: ({ children, open, title, description, size, showHeader, buttons }: any) =>
    open === false ? null : (
      <div data-testid="modal" data-size={size} data-show-header={String(showHeader)}>
        {title ? <h2>{title}</h2> : null}
        {description ? <p>{description}</p> : null}
        {children}
        {buttons?.map((b: any, i: number) => (
          <button key={i} disabled={b.disabled} onClick={b.onClick}>
            {b.text}
          </button>
        ))}
      </div>
    ),
}));

function snapshot(overrides: Record<string, unknown> = {}) {
  return {
    deviceIdentifier: "dev-1",
    name: "Miner One",
    serialNumber: "SN123",
    model: "S21",
    ipAddress: "10.0.0.5",
    placement: undefined,
    pairingStatus: PairingStatus.PAIRED,
    ...overrides,
  };
}

function renderScanMinerQrModal(overrides: Partial<ComponentProps<typeof ScanMinerQrModal>> = {}) {
  const props: ComponentProps<typeof ScanMinerQrModal> = {
    show: true,
    currentRackLabel: "Rack A",
    eligibility: {},
    targetSlotLabel: "Slot 1",
    onDismiss: vi.fn(),
    onConfirm: vi.fn(),
    onAssign: vi.fn().mockReturnValue({ slotLabel: "Slot 1", hasNextSlot: true }),
    onUndoAssignment: vi.fn(),
    onScanNextSlot: vi.fn().mockReturnValue(true),
    ...overrides,
  };

  return {
    ...render(<ScanMinerQrModal {...props} />),
    props,
  };
}

describe("ScanMinerQrModal", () => {
  beforeEach(() => {
    mockUseQrScanner.mockReset();
    mockCanUseLiveCamera.mockReset();
    mockLookup.mockReset();
    capturedOnDetected = undefined;
    capturedScannerOptions = undefined;
    mockUseQrScanner.mockReturnValue({
      videoRef: { current: null },
      status: "scanning",
      errorMessage: "",
      detectFromBlob: vi.fn(),
    });
  });

  it("resolves a scanned serial to a miner and assigns it immediately", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "found", snapshot: snapshot() });
    const onAssign = vi.fn().mockReturnValue({ slotLabel: "Slot 1", hasNextSlot: true });

    const { props } = renderScanMinerQrModal({ onAssign });

    await act(async () => {
      capturedOnDetected?.(["SN:SN123"]);
    });

    await waitFor(() => expect(screen.getByText("Miner assigned")).toBeInTheDocument());
    expect(screen.getByText("Miner One was assigned to Rack A, Slot 1.")).toBeInTheDocument();
    expect(screen.getByText("Miner")).toBeInTheDocument();
    expect(screen.getByText("Slot")).toBeInTheDocument();
    expect(screen.getByText("Rack A, Slot 1")).toBeInTheDocument();
    expect(screen.getByText("Serial")).toBeInTheDocument();
    expect(screen.getByText("SN123")).toBeInTheDocument();
    expect(screen.getByText("Model")).toBeInTheDocument();
    expect(screen.getByText("S21")).toBeInTheDocument();
    expect(mockLookup).toHaveBeenCalledWith("SN123", MinerIdentifierType.SERIAL_NUMBER, expect.any(AbortSignal));
    expect(onAssign).toHaveBeenCalledWith("dev-1");
    expect(props.onConfirm).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Scan next slot" }));
    expect(props.onScanNextSlot).toHaveBeenCalled();
  });

  it("undoes the just-made assignment from the success dialog", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "found", snapshot: snapshot() });
    const onUndoAssignment = vi.fn();

    renderScanMinerQrModal({ onUndoAssignment });

    await act(async () => {
      capturedOnDetected?.(["SN:SN123"]);
    });

    await waitFor(() => expect(screen.getByText("Miner assigned")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));

    expect(onUndoAssignment).toHaveBeenCalled();
  });

  it("tries every decoded barcode until one resolves", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup
      .mockResolvedValueOnce({ status: "notFound" })
      .mockResolvedValueOnce({ status: "found", snapshot: snapshot() });

    renderScanMinerQrModal();

    await act(async () => {
      capturedOnDetected?.(["FIRSTMISS111", "SECONDHIT222"]);
    });

    await waitFor(() => expect(screen.getByText("Miner assigned")).toBeInTheDocument());
    expect(mockLookup).toHaveBeenCalledTimes(2);
  });

  it("tries an explicitly-typed candidate before an unspecified one", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValue({ status: "found", snapshot: snapshot() });

    renderScanMinerQrModal();

    await act(async () => {
      capturedOnDetected?.(["MODEL234T", "SN:REALSN"]);
    });

    await waitFor(() => expect(screen.getByText("Miner assigned")).toBeInTheDocument());
    expect(mockLookup.mock.calls[0][0]).toBe("REALSN");
  });

  it("de-dupes a value decoded more than once in the same frame", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValue({ status: "found", snapshot: snapshot() });

    renderScanMinerQrModal();

    await act(async () => {
      capturedOnDetected?.(["SN:DUP", "SN:DUP"]);
    });

    await waitFor(() => expect(screen.getByText("Miner assigned")).toBeInTheDocument());
    expect(mockLookup).toHaveBeenCalledTimes(1);
  });

  it("shows a not-found message when the serial has no paired miner", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "notFound" });

    renderScanMinerQrModal();

    await act(async () => {
      capturedOnDetected?.(["SN:NOPE"]);
    });

    await waitFor(() => expect(screen.getByText("No paired miner found")).toBeInTheDocument());
    expect(
      screen.getByText('No paired miner found for "NOPE". Check that the miner is paired to this Fleet.'),
    ).toBeInTheDocument();
  });

  it("allows reassigning a miner already in a different rack through the confirmation path", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({
      status: "found",
      snapshot: snapshot({ placement: { rack: { id: 9n, label: "Rack B" } } }),
    });
    const onAssign = vi.fn();
    const onConfirm = vi.fn();

    renderScanMinerQrModal({ onAssign, onConfirm });

    await act(async () => {
      capturedOnDetected?.(["SN123"]);
    });

    await waitFor(() => expect(screen.getByText("Miner already assigned")).toBeInTheDocument());
    expect(screen.getByText("Assigning it here will move it from Rack B.")).toBeInTheDocument();
    expect(onAssign).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Assign to slot" }));
    expect(onConfirm).toHaveBeenCalledWith("dev-1", true);
  });

  it("blocks assigning a miner that isn't fully paired", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({
      status: "found",
      snapshot: snapshot({ pairingStatus: PairingStatus.AUTHENTICATION_NEEDED }),
    });
    const onAssign = vi.fn();
    const onConfirm = vi.fn();

    renderScanMinerQrModal({ onAssign, onConfirm });

    await act(async () => {
      capturedOnDetected?.(["SN123"]);
    });

    await waitFor(() => expect(screen.getByText(/isn't fully paired/i)).toBeInTheDocument());
    expect(
      screen.getByText("Only paired miners can be assigned to a rack. Finish pairing this miner, then scan it again."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Assign to slot")).not.toBeInTheDocument();
    expect(onAssign).not.toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("prioritizes pairing guidance when an unpaired miner is already assigned elsewhere", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({
      status: "found",
      snapshot: snapshot({
        placement: { rack: { id: 9n, label: "Rack B" } },
        pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      }),
    });
    const onAssign = vi.fn();
    const onConfirm = vi.fn();

    renderScanMinerQrModal({ onAssign, onConfirm });

    await act(async () => {
      capturedOnDetected?.(["SN123"]);
    });

    await waitFor(() => expect(screen.getByText(/isn't fully paired/i)).toBeInTheDocument());
    expect(
      screen.getByText("Only paired miners can be assigned to a rack. Finish pairing this miner, then scan it again."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Miner already assigned")).not.toBeInTheDocument();
    expect(screen.queryByText("Assigning it here will move it from Rack B.")).not.toBeInTheDocument();
    expect(screen.queryByText("Assign to slot")).not.toBeInTheDocument();
    expect(onAssign).not.toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("shows the target slot in the live scanner copy", () => {
    mockCanUseLiveCamera.mockReturnValue(true);

    renderScanMinerQrModal({ targetSlotLabel: "Slot 2" });

    expect(screen.getByText("Scan for Slot 2")).toBeInTheDocument();
    expect(screen.getAllByText("Point the camera at the miner QR code to assign it to Slot 2.")).toHaveLength(1);
    expect(screen.getByTestId("modal")).toHaveAttribute("data-size", "fullscreen");
    expect(screen.getByTestId("modal")).toHaveAttribute("data-show-header", "false");
  });

  it("renders the photo-capture fallback when the live camera is unavailable (HTTP)", () => {
    mockCanUseLiveCamera.mockReturnValue(false);

    renderScanMinerQrModal();

    expect(screen.getByText("Camera unavailable")).toBeInTheDocument();
    expect(
      screen.getByText("Live scanning needs HTTPS or localhost. Take a photo for Slot 1 instead."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Take photo instead" })).toBeInTheDocument();
  });

  it("opens the photo picker from the fallback action", () => {
    mockCanUseLiveCamera.mockReturnValue(false);

    renderScanMinerQrModal();

    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const clickSpy = vi.spyOn(input, "click").mockImplementation(() => undefined);

    fireEvent.click(screen.getByRole("button", { name: "Take photo instead" }));

    expect(clickSpy).toHaveBeenCalledTimes(1);
  });

  it("decodes a selected photo and clears the input for retrying the same file", async () => {
    mockCanUseLiveCamera.mockReturnValue(false);
    const detectFromBlob = vi.fn().mockResolvedValue(["SN:PHOTO123"]);
    mockUseQrScanner.mockReturnValue({
      videoRef: { current: null },
      status: "idle",
      errorMessage: "",
      detectFromBlob,
    });
    mockLookup.mockResolvedValueOnce({ status: "found", snapshot: snapshot({ serialNumber: "PHOTO123" }) });

    renderScanMinerQrModal();
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(["qr"], "scan.png", { type: "image/png" });

    await act(async () => {
      fireEvent.change(input, { target: { files: [file] } });
    });

    expect(detectFromBlob).toHaveBeenCalledWith(file);
    expect(input.value).toBe("");
    await waitFor(() => expect(screen.getByText("Miner assigned")).toBeInTheDocument());
  });

  it("restarts a failed live camera session when retrying", () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockUseQrScanner.mockReturnValue({
      videoRef: { current: null },
      status: "error",
      errorMessage: "Could not start the camera. You can take a photo instead.",
      detectFromBlob: vi.fn(),
    });

    renderScanMinerQrModal();

    expect(capturedScannerOptions?.restartKey).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(capturedScannerOptions?.active).toBe(true);
    expect(capturedScannerOptions?.restartKey).toBe(1);
  });

  it("surfaces a lookup error", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "error", message: "server exploded" });

    renderScanMinerQrModal();

    await act(async () => {
      capturedOnDetected?.(["SN123"]);
    });

    await waitFor(() => expect(screen.getByText("server exploded")).toBeInTheDocument());
  });
});
