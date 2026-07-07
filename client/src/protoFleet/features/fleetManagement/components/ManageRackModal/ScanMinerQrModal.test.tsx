import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ScanMinerQrModal from "./ScanMinerQrModal";
import { MinerIdentifierType, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

// --- Mock the scanner hook so tests never touch real camera/WASM APIs. ---
const mockUseQrScanner = vi.fn();
const mockCanUseLiveCamera = vi.fn();
let capturedOnDetected: ((raw: string) => void) | undefined;

vi.mock("@/protoFleet/features/fleetManagement/hooks/useQrScanner", () => ({
  canUseLiveCamera: () => mockCanUseLiveCamera(),
  useQrScanner: (opts: { onDetected: (raw: string) => void; active: boolean }) => {
    capturedOnDetected = opts.onDetected;
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
  default: ({ children, open, buttons }: any) =>
    open === false ? null : (
      <div data-testid="modal">
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

describe("ScanMinerQrModal", () => {
  beforeEach(() => {
    mockUseQrScanner.mockReset();
    mockCanUseLiveCamera.mockReset();
    mockLookup.mockReset();
    capturedOnDetected = undefined;
    mockUseQrScanner.mockReturnValue({
      videoRef: { current: null },
      status: "scanning",
      errorMessage: "",
      detectFromBlob: vi.fn(),
    });
  });

  it("resolves a scanned serial to a miner and confirms the device identifier", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "found", snapshot: snapshot() });
    const onConfirm = vi.fn();

    render(<ScanMinerQrModal show currentRackLabel="Rack A" onDismiss={vi.fn()} onConfirm={onConfirm} />);

    // Simulate the camera hook detecting a prefixed QR payload.
    await act(async () => {
      capturedOnDetected?.("SN:SN123");
    });

    await waitFor(() => expect(screen.getByText("Miner One")).toBeInTheDocument());
    // The parsed (prefix-stripped) serial + detected type are sent to the lookup.
    expect(mockLookup).toHaveBeenCalledWith("SN123", MinerIdentifierType.SERIAL_NUMBER);

    fireEvent.click(screen.getByText("Assign to slot"));
    expect(onConfirm).toHaveBeenCalledWith("dev-1");
  });

  it("shows a not-found message when the serial has no paired miner", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "notFound" });

    render(<ScanMinerQrModal show currentRackLabel="Rack A" onDismiss={vi.fn()} onConfirm={vi.fn()} />);

    await act(async () => {
      capturedOnDetected?.("SN:NOPE");
    });

    await waitFor(() => expect(screen.getByText(/No paired miner found/i)).toBeInTheDocument());
  });

  it("blocks assigning a miner already in a different rack", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({
      status: "found",
      snapshot: snapshot({ placement: { rack: { id: 9n, label: "Rack B" } } }),
    });
    const onConfirm = vi.fn();

    render(<ScanMinerQrModal show currentRackLabel="Rack A" onDismiss={vi.fn()} onConfirm={onConfirm} />);

    await act(async () => {
      capturedOnDetected?.("SN123");
    });

    await waitFor(() => expect(screen.getByText(/Already assigned to rack "Rack B"/i)).toBeInTheDocument());
    const assignBtn = screen.getByText("Assign to slot") as HTMLButtonElement;
    expect(assignBtn.disabled).toBe(true);
  });

  it("blocks assigning a miner that isn't fully paired", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({
      status: "found",
      snapshot: snapshot({ pairingStatus: PairingStatus.AUTHENTICATION_NEEDED }),
    });
    const onConfirm = vi.fn();

    render(<ScanMinerQrModal show currentRackLabel="Rack A" onDismiss={vi.fn()} onConfirm={onConfirm} />);

    await act(async () => {
      capturedOnDetected?.("SN123");
    });

    await waitFor(() => expect(screen.getByText(/isn't fully paired/i)).toBeInTheDocument());
    const assignBtn = screen.getByText("Assign to slot") as HTMLButtonElement;
    expect(assignBtn.disabled).toBe(true);
  });

  it("renders the photo-capture fallback when the live camera is unavailable (HTTP)", () => {
    mockCanUseLiveCamera.mockReturnValue(false);

    render(<ScanMinerQrModal show currentRackLabel="Rack A" onDismiss={vi.fn()} onConfirm={vi.fn()} />);

    expect(screen.getByText(/Take a photo of the code/i)).toBeInTheDocument();
    expect(screen.getByText("Open camera")).toBeInTheDocument();
  });

  it("surfaces a lookup error", async () => {
    mockCanUseLiveCamera.mockReturnValue(true);
    mockLookup.mockResolvedValueOnce({ status: "error", message: "server exploded" });

    render(<ScanMinerQrModal show currentRackLabel="Rack A" onDismiss={vi.fn()} onConfirm={vi.fn()} />);

    await act(async () => {
      capturedOnDetected?.("SN123");
    });

    await waitFor(() => expect(screen.getByText("server exploded")).toBeInTheDocument());
  });
});
