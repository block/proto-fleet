import { type ReactNode, type RefObject } from "react";

import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { INACTIVE_PLACEHOLDER } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { FLEET_SELECTABLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { getMinerRackLabel } from "@/protoFleet/features/fleetManagement/utils/minerPlacement";

import { Alert, Dismiss, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import type { ButtonProps } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

/** Discriminated state of the scan flow, owned by the container. */
export type ScanPhase =
  | { kind: "scanning" }
  | { kind: "looking-up"; identifier: string }
  | { kind: "assigned"; snapshot: MinerStateSnapshot; slotLabel: string; hasNextSlot: boolean }
  | { kind: "found"; snapshot: MinerStateSnapshot; isReassignment: boolean }
  | { kind: "not-found"; identifier: string }
  | { kind: "error"; message: string };

export interface ScanMinerQrModalViewProps {
  show: boolean;
  phase: ScanPhase;
  /** Label of the rack being edited; shown in assignment and confirmation copy. */
  currentRackLabel: string;
  /** Label of the rack slot the scan will assign into. */
  targetSlotLabel: string;
  /** Whether a live camera stream is available (secure context). */
  liveCamera: boolean;
  /** Live-camera view bindings (unused in the photo-capture fallback). */
  videoRef: RefObject<HTMLVideoElement | null>;
  cameraStatus: string;
  cameraError: string;
  /** Hidden file input for the photo-capture fallback. */
  fileInputRef: RefObject<HTMLInputElement | null>;
  onDismiss: () => void;
  onConfirmFound: () => void;
  onUndoAssignment: () => void;
  onScanNextSlot: () => void;
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
  targetSlotLabel,
  liveCamera,
  videoRef,
  cameraStatus,
  cameraError,
  fileInputRef,
  onDismiss,
  onConfirmFound,
  onUndoAssignment,
  onScanNextSlot,
  onRescan,
  onFile,
}: ScanMinerQrModalViewProps) {
  if (!show) return null;

  // A miner already in a *different* rack can still be assigned here — it's a
  // reparent, confirmed via the warning in ManageRackModal — so this only drives
  // an informational note, not a block.
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
  const cameraUnavailable = phase.kind === "scanning" && !liveCamera;
  const cameraFailed = phase.kind === "scanning" && liveCamera && (cameraStatus === "error" || !!cameraError);

  if (cameraUnavailable) {
    return (
      <CameraErrorDialog
        errorMessage="Live scanning needs HTTPS or localhost."
        targetSlotLabel={targetSlotLabel}
        fileInputRef={fileInputRef}
        showRetry={false}
        onDismiss={onDismiss}
        onFile={onFile}
        onRescan={onRescan}
      />
    );
  }

  if (cameraFailed) {
    return (
      <CameraErrorDialog
        errorMessage={cameraError || "Could not start the camera."}
        targetSlotLabel={targetSlotLabel}
        fileInputRef={fileInputRef}
        onDismiss={onDismiss}
        onFile={onFile}
        onRescan={onRescan}
      />
    );
  }

  if (phase.kind === "looking-up") {
    return (
      <Dialog
        open={show}
        loading
        title="Looking up miner"
        subtitle={
          phase.identifier ? `${phase.identifier}, ${targetSlotLabel}` : `Reading QR code for ${targetSlotLabel}`
        }
        onDismiss={onDismiss}
      />
    );
  }

  if (phase.kind === "assigned") {
    return (
      <AssignedMinerDialog
        snapshot={phase.snapshot}
        rackLabel={currentRackLabel}
        slotLabel={phase.slotLabel}
        hasNextSlot={phase.hasNextSlot}
        onDismiss={onDismiss}
        onUndoAssignment={onUndoAssignment}
        onScanNextSlot={onScanNextSlot}
      />
    );
  }

  if (phase.kind === "found") {
    return (
      <FoundMinerDialog
        snapshot={phase.snapshot}
        currentRackLabel={currentRackLabel}
        inOtherRack={foundInOtherRack}
        isReassignment={phase.isReassignment}
        otherRackLabel={getMinerRackLabel(phase.snapshot)}
        notPaired={notPairedForAssignment}
        onDismiss={onDismiss}
        onConfirm={onConfirmFound}
        onRescan={onRescan}
      />
    );
  }

  if (phase.kind === "not-found") {
    return (
      <ScanResultDialog
        intent="warning"
        title="No paired miner found"
        subtitle={
          phase.identifier
            ? `No paired miner found for "${phase.identifier}". Check that the miner is paired to this Fleet.`
            : "Make sure the whole QR code is visible and try again."
        }
        onDismiss={onDismiss}
        buttons={[
          {
            text: "Try again",
            variant: variants.primary,
            onClick: onRescan,
          },
        ]}
      />
    );
  }

  if (phase.kind === "error") {
    return (
      <ScanResultDialog
        intent="critical"
        title="Couldn't complete scan"
        subtitle={phase.message}
        onDismiss={onDismiss}
        buttons={[
          {
            text: "Try again",
            variant: variants.primary,
            onClick: onRescan,
          },
        ]}
      />
    );
  }

  return (
    <Modal
      open={show}
      size="fullscreen"
      showHeader={false}
      className="flex h-full flex-col !p-0"
      bodyClassName="flex h-full min-h-0 flex-col"
      onDismiss={onDismiss}
    >
      <div className="flex h-full min-h-0 flex-col">
        <ScannerHeader targetSlotLabel={targetSlotLabel} onDismiss={onDismiss} />
        <div className="flex min-h-0 flex-1 px-6 pb-6">
          {phase.kind === "scanning" && liveCamera ? (
            <LiveCameraView videoRef={videoRef} status={cameraStatus} errorMessage={cameraError} />
          ) : null}
        </div>
      </div>
    </Modal>
  );
}

function ScannerHeader({ targetSlotLabel, onDismiss }: { targetSlotLabel: string; onDismiss: () => void }) {
  return (
    <Header
      title={`Scan for ${targetSlotLabel}`}
      description={`Point the camera at the miner QR code to assign it to ${targetSlotLabel}.`}
      titleSize="text-heading-200"
      className="shrink-0 px-6 py-5"
      inline
      stackButtonsOnPhone={false}
      icon={<Dismiss />}
      iconAriaLabel="Close scanner"
      iconOnClick={onDismiss}
    />
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
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <div className="relative min-h-[280px] w-full flex-1 overflow-hidden rounded-2xl bg-black">
        <video ref={videoRef} className="h-full w-full object-cover" muted playsInline autoPlay />
        {/* Framing reticle */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <div className="aspect-square h-[min(70%,420px)] rounded-2xl border-2 border-white/80" />
        </div>
        {status === "starting" ? (
          <div className="absolute inset-0 flex items-center justify-center bg-black/40">
            <ProgressCircular indeterminate />
          </div>
        ) : null}
      </div>
      {errorMessage ? <Callout intent="danger" prefixIcon={<Alert />} title={errorMessage} /> : null}
    </div>
  );
}

function CameraErrorDialog({
  errorMessage,
  targetSlotLabel,
  fileInputRef,
  showRetry = true,
  onDismiss,
  onFile,
  onRescan,
}: {
  errorMessage: string;
  targetSlotLabel: string;
  fileInputRef: RefObject<HTMLInputElement | null>;
  showRetry?: boolean;
  onDismiss: () => void;
  onFile: (file: File | undefined) => void;
  onRescan: () => void;
}) {
  const openPhotoCapture = () => fileInputRef.current?.click();

  return (
    <Dialog
      open
      title="Camera unavailable"
      subtitle={
        showRetry
          ? `${errorMessage} Try again, or take a photo for ${targetSlotLabel}.`
          : `${errorMessage} Take a photo for ${targetSlotLabel} instead.`
      }
      subtitleSize="text-300"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        dismissButton(onDismiss),
        ...(showRetry
          ? [
              {
                text: "Try again",
                variant: variants.secondary,
                onClick: onRescan,
              },
            ]
          : []),
        {
          text: "Take photo instead",
          variant: variants.primary,
          onClick: openPhotoCapture,
        },
      ]}
    >
      <HiddenPhotoInput fileInputRef={fileInputRef} onFile={onFile} />
    </Dialog>
  );
}

function HiddenPhotoInput({
  fileInputRef,
  onFile,
}: {
  fileInputRef: RefObject<HTMLInputElement | null>;
  onFile: (file: File | undefined) => void;
}) {
  return (
    <input
      ref={fileInputRef}
      type="file"
      accept="image/*"
      capture="environment"
      className="hidden"
      onChange={(e) => {
        onFile(e.target.files?.[0]);
        e.currentTarget.value = "";
      }}
    />
  );
}

function FoundMinerDialog({
  snapshot,
  currentRackLabel,
  inOtherRack,
  isReassignment,
  otherRackLabel,
  notPaired,
  onDismiss,
  onConfirm,
  onRescan,
}: {
  snapshot: MinerStateSnapshot;
  currentRackLabel: string;
  inOtherRack: boolean;
  isReassignment: boolean;
  otherRackLabel: string;
  notPaired: boolean;
  onDismiss: () => void;
  onConfirm: () => void;
  onRescan: () => void;
}) {
  const title = notPaired
    ? "Miner isn't fully paired"
    : inOtherRack
      ? "Miner already assigned"
      : isReassignment
        ? "Confirm miner move"
        : "Miner can't be assigned";
  const subtitle = notPaired
    ? "Only paired miners can be assigned to a rack. Finish pairing this miner, then scan it again."
    : inOtherRack
      ? `Assigning it here will move it from ${otherRackLabel}.`
      : isReassignment
        ? `Assigning it here will move it to ${currentRackLabel}.`
        : "Choose another miner for this rack slot.";
  const canConfirmAssignment = isReassignment && !notPaired;
  const buttons: ButtonProps[] = [
    dismissButton(onDismiss),
    {
      text: "Scan another",
      variant: canConfirmAssignment ? variants.secondary : variants.primary,
      onClick: onRescan,
    },
    ...(canConfirmAssignment
      ? [
          {
            text: "Assign to slot",
            variant: variants.primary,
            onClick: onConfirm,
          },
        ]
      : []),
  ];

  return (
    <Dialog
      open
      title={title}
      subtitle={subtitle}
      subtitleSize="text-300"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="warning">
          <Alert />
        </DialogIcon>
      }
      buttons={buttons}
    >
      <div className="flex flex-col">
        <SummaryRow label="Miner">{snapshot.name || snapshot.deviceIdentifier}</SummaryRow>
        <SummaryRow label="Serial">{snapshot.serialNumber || INACTIVE_PLACEHOLDER}</SummaryRow>
        <SummaryRow label="Model">{snapshot.model || INACTIVE_PLACEHOLDER}</SummaryRow>
        <SummaryRow label="IP address" divider={false}>
          {snapshot.ipAddress || INACTIVE_PLACEHOLDER}
        </SummaryRow>
      </div>
    </Dialog>
  );
}

function AssignedMinerDialog({
  snapshot,
  rackLabel,
  slotLabel,
  hasNextSlot,
  onDismiss,
  onUndoAssignment,
  onScanNextSlot,
}: {
  snapshot: MinerStateSnapshot;
  rackLabel: string;
  slotLabel: string;
  hasNextSlot: boolean;
  onDismiss: () => void;
  onUndoAssignment: () => void;
  onScanNextSlot: () => void;
}) {
  const minerName = snapshot.name || snapshot.deviceIdentifier;
  const assignmentLabel = `${rackLabel}, ${slotLabel}`;
  const buttons: ButtonProps[] = [
    dismissButton(onDismiss),
    {
      text: "Undo",
      variant: variants.secondary,
      onClick: onUndoAssignment,
    },
    ...(hasNextSlot
      ? [
          {
            text: "Scan next slot",
            variant: variants.primary,
            onClick: onScanNextSlot,
          },
        ]
      : []),
  ];

  return (
    <Dialog
      open
      title="Miner assigned"
      subtitle={`${minerName} was assigned to ${assignmentLabel}.`}
      subtitleSize="text-300"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="success">
          <Success />
        </DialogIcon>
      }
      buttons={buttons}
    >
      <div className="flex flex-col">
        <SummaryRow label="Miner">{minerName}</SummaryRow>
        <SummaryRow label="Slot">{assignmentLabel}</SummaryRow>
        <SummaryRow label="Serial">{snapshot.serialNumber || INACTIVE_PLACEHOLDER}</SummaryRow>
        <SummaryRow label="Model" divider={false}>
          {snapshot.model || INACTIVE_PLACEHOLDER}
        </SummaryRow>
      </div>
    </Dialog>
  );
}

function ScanResultDialog({
  intent,
  title,
  subtitle,
  buttons,
  onDismiss,
}: {
  intent: "critical" | "warning";
  title: string;
  subtitle: string;
  buttons: ButtonProps[];
  onDismiss: () => void;
}) {
  return (
    <Dialog
      open
      title={title}
      subtitle={subtitle}
      subtitleSize="text-300"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent={intent === "critical" ? "critical" : "warning"}>
          <Alert />
        </DialogIcon>
      }
      buttons={[dismissButton(onDismiss), ...buttons]}
    />
  );
}

function dismissButton(onDismiss: () => void): ButtonProps {
  return {
    text: "Dismiss",
    variant: variants.secondary,
    onClick: onDismiss,
  };
}

function SummaryRow({ label, children, divider = true }: { label: string; children: ReactNode; divider?: boolean }) {
  return (
    <Row compact divider={divider}>
      <div className="flex w-full items-center justify-between gap-4">
        <span className="shrink-0 text-300 text-text-primary-70">{label}</span>
        <span className="min-w-0 text-right text-300 break-words text-text-primary">{children}</span>
      </div>
    </Row>
  );
}
