import { type RefObject } from "react";

import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { INACTIVE_PLACEHOLDER } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { FLEET_SELECTABLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { getMinerRackLabel } from "@/protoFleet/features/fleetManagement/utils/minerPlacement";

import { Alert, Checkmark, Info } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

/** Discriminated state of the scan flow, owned by the container. */
export type ScanPhase =
  | { kind: "scanning" }
  | { kind: "looking-up"; identifier: string }
  | { kind: "found"; snapshot: MinerStateSnapshot }
  | { kind: "not-found"; identifier: string }
  | { kind: "error"; message: string };

export interface ScanMinerQrModalViewProps {
  show: boolean;
  phase: ScanPhase;
  /** Label of the rack being edited; a miner already in a *different* rack is blocked. */
  currentRackLabel: string;
  /** Whether a live camera stream is available (secure context). */
  liveCamera: boolean;
  /** Live-camera view bindings (unused in the photo-capture fallback). */
  videoRef: RefObject<HTMLVideoElement | null>;
  cameraStatus: string;
  cameraError: string;
  /** Hidden file input for the photo-capture fallback. */
  fileInputRef: RefObject<HTMLInputElement | null>;
  onDismiss: () => void;
  onConfirm: () => void;
  onRescan: () => void;
  onFile: (file: File | undefined) => void;
}

/**
 * Presentational shell for the scan-a-miner-QR flow. Renders purely from
 * `phase` and camera bindings — all camera access, decoding, and the
 * identifier lookup live in the ScanMinerQrModal container. Kept separate so
 * the visual states are storyable without a camera or backend.
 */
export default function ScanMinerQrModalView({
  show,
  phase,
  currentRackLabel,
  liveCamera,
  videoRef,
  cameraStatus,
  cameraError,
  fileInputRef,
  onDismiss,
  onConfirm,
  onRescan,
  onFile,
}: ScanMinerQrModalViewProps) {
  if (!show) return null;

  // A miner already assigned to a *different* rack cannot be moved here via scan.
  const foundInOtherRack =
    phase.kind === "found" &&
    !!getMinerRackLabel(phase.snapshot) &&
    getMinerRackLabel(phase.snapshot) !== currentRackLabel;

  // Enforce the same eligibility rule as the list/search assignment flows
  // (FLEET_SELECTABLE_PAIRING_STATUSES = PAIRED only). LookupMinerByIdentifier
  // also resolves AUTHENTICATION_NEEDED / DEFAULT_PASSWORD miners, so without
  // this guard the scan flow would let operators rack not-fully-paired miners
  // that the rest of the UI excludes.
  const notPairedForAssignment =
    phase.kind === "found" && !FLEET_SELECTABLE_PAIRING_STATUSES.includes(phase.snapshot.pairingStatus);

  return (
    <Modal
      open={show}
      title="Scan miner barcode"
      size="standard"
      phoneSheet
      onDismiss={onDismiss}
      buttons={
        phase.kind === "found"
          ? [
              {
                text: "Scan another",
                variant: variants.secondary,
                onClick: onRescan,
                dismissModalOnClick: false,
              },
              {
                text: "Assign to slot",
                variant: variants.primary,
                disabled: foundInOtherRack || notPairedForAssignment,
                onClick: onConfirm,
                dismissModalOnClick: false,
              },
            ]
          : phase.kind === "not-found" || phase.kind === "error"
            ? [
                {
                  text: "Try again",
                  variant: variants.primary,
                  onClick: onRescan,
                  dismissModalOnClick: false,
                },
              ]
            : undefined
      }
    >
      <div className="flex flex-col gap-4 p-2">
        {phase.kind === "scanning" && liveCamera ? (
          <LiveCameraView videoRef={videoRef} status={cameraStatus} errorMessage={cameraError} />
        ) : null}

        {phase.kind === "scanning" && !liveCamera ? (
          <PhotoCapturePrompt fileInputRef={fileInputRef} onFile={onFile} />
        ) : null}

        {phase.kind === "looking-up" ? (
          <div className="flex flex-col items-center gap-3 py-10">
            <ProgressCircular indeterminate />
            <span className="text-300 text-text-primary-70">
              {phase.identifier ? `Looking up ${phase.identifier}…` : "Reading code…"}
            </span>
          </div>
        ) : null}

        {phase.kind === "found" ? (
          <FoundMiner
            snapshot={phase.snapshot}
            inOtherRack={foundInOtherRack}
            otherRackLabel={getMinerRackLabel(phase.snapshot)}
            notPaired={notPairedForAssignment}
          />
        ) : null}

        {phase.kind === "not-found" ? (
          <Callout
            intent="warning"
            prefixIcon={<Alert />}
            title={phase.identifier ? `No paired miner found for "${phase.identifier}"` : "No code detected"}
            subtitle={
              phase.identifier
                ? "Check that the miner is paired to this Fleet, or scan a different code."
                : "Make sure the whole code is visible and try again."
            }
          />
        ) : null}

        {phase.kind === "error" ? <Callout intent="danger" prefixIcon={<Alert />} title={phase.message} /> : null}

        {/* Photo-capture fallback stays available even on secure contexts so a
            user whose camera failed can still take a photo. */}
        {phase.kind === "scanning" && liveCamera && (cameraStatus === "error" || cameraError) ? (
          <PhotoCapturePrompt fileInputRef={fileInputRef} onFile={onFile} compact />
        ) : null}
      </div>
    </Modal>
  );
}

function LiveCameraView({
  videoRef,
  status,
  errorMessage,
}: {
  videoRef: RefObject<HTMLVideoElement | null>;
  status: string;
  errorMessage: string;
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="relative aspect-square w-full overflow-hidden rounded-2xl bg-black">
        <video ref={videoRef} className="h-full w-full object-cover" muted playsInline autoPlay />
        {/* Framing reticle */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <div className="h-2/3 w-2/3 rounded-2xl border-2 border-white/80" />
        </div>
        {status === "starting" ? (
          <div className="absolute inset-0 flex items-center justify-center bg-black/40">
            <ProgressCircular indeterminate />
          </div>
        ) : null}
      </div>
      {errorMessage ? (
        <Callout intent="danger" prefixIcon={<Alert />} title={errorMessage} />
      ) : (
        <span className="text-center text-300 text-text-primary-50">
          Point the camera at the QR code or barcode on the miner's label.
        </span>
      )}
    </div>
  );
}

function PhotoCapturePrompt({
  fileInputRef,
  onFile,
  compact = false,
}: {
  fileInputRef: RefObject<HTMLInputElement | null>;
  onFile: (file: File | undefined) => void;
  compact?: boolean;
}) {
  return (
    <div className="flex flex-col gap-3">
      {!compact ? (
        <Callout
          intent="information"
          prefixIcon={<Info />}
          title="Take a photo of the code"
          subtitle="Live scanning needs a secure (HTTPS) connection. Tap below to capture the code on the miner's label with your camera."
        />
      ) : null}
      <button
        type="button"
        className="w-full rounded-xl border border-border-10 bg-surface-5 py-4 text-300 font-medium text-text-primary hover:bg-surface-10"
        onClick={() => fileInputRef.current?.click()}
      >
        {compact ? "Take a photo instead" : "Open camera"}
      </button>
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        capture="environment"
        className="hidden"
        onChange={(e) => onFile(e.target.files?.[0])}
      />
    </div>
  );
}

function FoundMiner({
  snapshot,
  inOtherRack,
  otherRackLabel,
  notPaired,
}: {
  snapshot: MinerStateSnapshot;
  inOtherRack: boolean;
  otherRackLabel: string;
  notPaired: boolean;
}) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start gap-3 rounded-2xl border border-core-primary-fill bg-core-primary-5 p-4">
        <Checkmark className="mt-0.5 shrink-0 text-core-primary-fill" />
        <div className="flex flex-col gap-1">
          <span className="text-400 font-medium text-text-primary">{snapshot.name || snapshot.deviceIdentifier}</span>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-300 text-text-primary-70">
            <dt className="text-text-primary-50">Serial</dt>
            <dd>{snapshot.serialNumber || INACTIVE_PLACEHOLDER}</dd>
            <dt className="text-text-primary-50">Model</dt>
            <dd>{snapshot.model || INACTIVE_PLACEHOLDER}</dd>
            <dt className="text-text-primary-50">IP address</dt>
            <dd>{snapshot.ipAddress || INACTIVE_PLACEHOLDER}</dd>
          </dl>
        </div>
      </div>
      {inOtherRack ? (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title={`Already assigned to rack "${otherRackLabel}"`}
          subtitle="Remove it from that rack before assigning it here."
        />
      ) : notPaired ? (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title="This miner isn't fully paired yet"
          subtitle="Only paired miners can be assigned to a rack. Finish pairing this miner, then scan it again."
        />
      ) : null}
    </div>
  );
}
