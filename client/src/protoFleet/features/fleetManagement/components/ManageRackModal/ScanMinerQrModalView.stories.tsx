import { useRef } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ScanMinerQrModalView, { type ScanPhase } from "./ScanMinerQrModalView";
import {
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

function mockSnapshot(overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot {
  return {
    deviceIdentifier: "device-abc123",
    name: "Miner-042",
    macAddress: "AA:BB:CC:DD:EE:FF",
    serialNumber: "1234567890123456",
    powerUsage: [],
    temperature: [],
    hashrate: [],
    efficiency: [],
    ipAddress: "192.168.1.42",
    url: "",
    deviceStatus: 0,
    pairingStatus: PairingStatus.PAIRED,
    model: "Antminer S21 XP",
    manufacturer: "Bitmain",
    temperatureStatus: 0,
    firmwareVersion: "",
    driverName: "",
    workerName: "",
    ...overrides,
  } as MinerStateSnapshot;
}

/**
 * Presentational states of the QR scan flow. The real container
 * (ScanMinerQrModal) drives these phases from the camera + serial lookup;
 * here we render each one directly so the visual states are reviewable
 * without a camera or backend.
 */
const Harness = ({
  phase,
  targetSlotLabel = "Slot 1",
  liveCamera = true,
  cameraStatus,
  cameraError = "",
}: {
  phase: ScanPhase;
  targetSlotLabel?: string;
  liveCamera?: boolean;
  cameraStatus?: string;
  cameraError?: string;
}) => {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const scanRegionRef = useRef<HTMLDivElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  return (
    <ScanMinerQrModalView
      show
      phase={phase}
      currentRackLabel="Rack A-01"
      targetSlotLabel={targetSlotLabel}
      liveCamera={liveCamera}
      videoRef={videoRef}
      scanRegionRef={scanRegionRef}
      cameraStatus={cameraStatus ?? (phase.kind === "scanning" ? "scanning" : "idle")}
      cameraError={cameraError}
      fileInputRef={fileInputRef}
      onDismiss={action("onDismiss")}
      onConfirmFound={action("onConfirmFound")}
      onUndoAssignment={action("onUndoAssignment")}
      onScanNextSlot={action("onScanNextSlot")}
      onRescan={action("onRescan")}
      onFile={(file) => action("onFile")(file?.name)}
    />
  );
};

const meta = {
  title: "Proto Fleet/Rack Management/ScanMinerQrModal",
  component: Harness,
  parameters: { layout: "fullscreen" },
} satisfies Meta<typeof Harness>;

export default meta;

type Story = StoryObj<typeof meta>;

/** Live camera viewfinder (secure context / HTTPS or localhost). */
export const Scanning: Story = {
  args: { phase: { kind: "scanning" }, liveCamera: true },
};

/** HTTP install fallback: no secure context, so we prompt for a photo capture. */
export const PhotoCaptureFallback: Story = {
  args: { phase: { kind: "scanning" }, liveCamera: false },
};

/** Live camera failed, with retry plus photo fallback available. */
export const CameraError: Story = {
  args: {
    phase: { kind: "scanning" },
    liveCamera: true,
    cameraStatus: "error",
    cameraError: "Could not start the camera. You can take a photo instead.",
  },
};

/** Resolving a scanned serial against the fleet. */
export const LookingUp: Story = {
  args: { phase: { kind: "looking-up", identifier: "1234567890123456" } },
};

/** A paired miner was resolved and assigned to the selected slot. */
export const Found: Story = {
  args: {
    phase: {
      kind: "assigned",
      snapshot: mockSnapshot(),
      slotLabel: "Slot 1",
      hasNextSlot: true,
    },
  },
};

/** Success on the final assignable slot. */
export const FoundFinalSlot: Story = {
  args: {
    phase: {
      kind: "assigned",
      snapshot: mockSnapshot({ name: "Miner-096" }),
      slotLabel: "Slot 96",
      hasNextSlot: false,
    },
  },
};

/** Resolved, but the miner already belongs to a different rack (requires confirmation). */
export const FoundInAnotherRack: Story = {
  args: {
    phase: {
      kind: "found",
      snapshot: mockSnapshot({ placement: { rack: { id: 7n, label: "Rack B-02" } } } as Partial<MinerStateSnapshot>),
      isReassignment: true,
    },
  },
};

/** Resolved, but the miner still needs pairing work before assignment. */
export const FoundNotFullyPaired: Story = {
  args: {
    phase: {
      kind: "found",
      snapshot: mockSnapshot({ pairingStatus: PairingStatus.AUTHENTICATION_NEEDED }),
      isReassignment: false,
    },
  },
};

/** The serial did not match any paired miner. */
export const NotFound: Story = {
  args: { phase: { kind: "not-found", identifier: "9999999999999999" } },
};

/** A QR code was read but no serial could be parsed from it. */
export const NoCodeDetected: Story = {
  args: { phase: { kind: "not-found", identifier: "" } },
};

/** An unexpected lookup/transport error. */
export const ErrorState: Story = {
  args: { phase: { kind: "error", message: "Failed to look up miner. Please try again." } },
};
