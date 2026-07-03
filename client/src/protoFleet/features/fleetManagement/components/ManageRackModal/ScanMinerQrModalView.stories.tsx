import { useRef } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ScanMinerQrModalView, { type ScanPhase } from "./ScanMinerQrModalView";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

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
    pairingStatus: 0,
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
const Harness = ({ phase, liveCamera = true }: { phase: ScanPhase; liveCamera?: boolean }) => {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  return (
    <ScanMinerQrModalView
      show
      phase={phase}
      currentRackLabel="Rack A-01"
      liveCamera={liveCamera}
      videoRef={videoRef}
      cameraStatus={phase.kind === "scanning" ? "scanning" : "idle"}
      cameraError=""
      fileInputRef={fileInputRef}
      onDismiss={action("onDismiss")}
      onConfirm={action("onConfirm")}
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

/** Resolving a scanned serial against the fleet. */
export const LookingUp: Story = {
  args: { phase: { kind: "looking-up", serial: "1234567890123456" } },
};

/** A paired miner was resolved and is ready to assign. */
export const Found: Story = {
  args: { phase: { kind: "found", snapshot: mockSnapshot() } },
};

/** Resolved, but the miner already belongs to a different rack (assign blocked). */
export const FoundInAnotherRack: Story = {
  args: {
    phase: {
      kind: "found",
      snapshot: mockSnapshot({ placement: { rack: { id: 7n, label: "Rack B-02" } } } as Partial<MinerStateSnapshot>),
    },
  },
};

/** The serial did not match any paired miner. */
export const NotFound: Story = {
  args: { phase: { kind: "not-found", serial: "9999999999999999" } },
};

/** A QR code was read but no serial could be parsed from it. */
export const NoCodeDetected: Story = {
  args: { phase: { kind: "not-found", serial: "" } },
};

/** An unexpected lookup/transport error. */
export const ErrorState: Story = {
  args: { phase: { kind: "error", message: "Failed to look up miner. Please try again." } },
};
