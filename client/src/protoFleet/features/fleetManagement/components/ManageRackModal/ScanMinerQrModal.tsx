import { useCallback, useEffect, useRef, useState } from "react";

import ScanMinerQrModalView, { type ScanPhase } from "./ScanMinerQrModalView";
import {
  MinerIdentifierType,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { lookupMinerByIdentifier } from "@/protoFleet/api/lookupMinerByIdentifier";
import type { MinerEligibility } from "@/protoFleet/components/MinerSelectionList";
import { canUseLiveCamera, useQrScanner } from "@/protoFleet/features/fleetManagement/hooks/useQrScanner";
import { FLEET_SELECTABLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";
import { isMinerSnapshotIneligible } from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import { parseScannedIdentifier } from "@/protoFleet/features/fleetManagement/utils/parseScannedIdentifier";

export interface ScanAssignmentResult {
  slotLabel: string;
  hasNextSlot: boolean;
}

interface ScanMinerQrModalProps {
  show: boolean;
  /** Label of the rack being edited; shown in assignment and confirmation copy. */
  currentRackLabel: string;
  /** Target rack placement, used to flag whether the scanned miner is a reparent. */
  eligibility: MinerEligibility;
  targetSlotLabel: string;
  onDismiss: () => void;
  /** `isReassignment` is true when the scanned miner is currently assigned to a
   *  different rack/building/site, so the caller can confirm the reparent. */
  onConfirm: (deviceIdentifier: string, isReassignment: boolean) => void;
  onAssign: (deviceIdentifier: string) => ScanAssignmentResult | null;
  onUndoAssignment: () => void;
  onScanNextSlot: () => boolean;
}

/**
 * Container for the scan-a-miner-QR flow: owns camera access (via useQrScanner),
 * decoding, and the identifier → miner lookup, driving the presentational
 * ScanMinerQrModalView through a `ScanPhase` state machine.
 */
export default function ScanMinerQrModal({
  show,
  currentRackLabel,
  eligibility,
  targetSlotLabel,
  onDismiss,
  onConfirm,
  onAssign,
  onUndoAssignment,
  onScanNextSlot,
}: ScanMinerQrModalProps) {
  const [phase, setPhase] = useState<ScanPhase>({ kind: "scanning" });
  const [scannerRestartKey, setScannerRestartKey] = useState(0);
  const liveCamera = canUseLiveCamera();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const scanRegionRef = useRef<HTMLDivElement | null>(null);
  // Guards the async lookup against a modal close / rescan mid-flight.
  const lookupSeq = useRef(0);
  // Aborts in-flight lookups (a multi-candidate loop issues several) on rescan
  // or unmount, so a mid-scan dismiss doesn't keep hitting the server.
  const abortRef = useRef<AbortController | null>(null);

  // Only run the live camera while we're actively scanning (not while showing
  // a result). Toggling this tears the stream down between scans.
  const cameraActive = show && liveCamera && phase.kind === "scanning";

  // A single frame/photo can decode more than one barcode (e.g. neighboring
  // miner labels in a dense rack row), and the detector's ordering isn't
  // guaranteed — so try each decoded value against the lookup, auto-assign only
  // when exactly one unique miner resolves, and ask for confirmation when more
  // than one miner resolves.
  const runLookup = useCallback(
    async (rawValues: string[]) => {
      const seq = ++lookupSeq.current;
      // Cancel any lookups still in flight from a previous scan.
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      // Parse → drop empties → de-dupe by value (the same label can decode more
      // than once in one frame) → try explicitly-typed (SN:/MAC:) candidates
      // first so a stray model/asset code can't out-race the intended serial/MAC.
      const seen = new Set<string>();
      const candidates = rawValues
        .map((raw) => parseScannedIdentifier(raw))
        .filter((c) => {
          if (!c.value || seen.has(c.value)) return false;
          seen.add(c.value);
          return true;
        })
        .sort(
          (a, b) =>
            Number(a.type === MinerIdentifierType.UNSPECIFIED) - Number(b.type === MinerIdentifierType.UNSPECIFIED),
        );

      if (candidates.length === 0) {
        setPhase({ kind: "not-found", identifier: rawValues[0]?.trim() ?? "" });
        return;
      }
      setPhase({ kind: "looking-up", identifier: candidates[0].value });
      const resolvedSnapshotsByDeviceId = new Map<string, MinerStateSnapshot>();

      for (const { value, type } of candidates) {
        const result = await lookupMinerByIdentifier(value, type, controller.signal);
        if (seq !== lookupSeq.current || controller.signal.aborted) return; // superseded / aborted
        if (result.status === "found") {
          resolvedSnapshotsByDeviceId.set(result.snapshot.deviceIdentifier, result.snapshot);
          continue;
        }
        if (result.status === "error") {
          // A transport/server failure will hit the remaining candidates too —
          // surface it now instead of waiting through N sequential failures.
          setPhase({ kind: "error", message: result.message });
          return;
        }
        // notFound → try the next candidate
      }
      const resolvedSnapshots = [...resolvedSnapshotsByDeviceId.values()];
      if (resolvedSnapshots.length === 0) {
        setPhase({ kind: "not-found", identifier: candidates[0].value });
        return;
      }

      const snapshot = resolvedSnapshots[0];
      const isReassignment = isMinerSnapshotIneligible(snapshot, eligibility);
      const notPairedForAssignment = !FLEET_SELECTABLE_PAIRING_STATUSES.includes(snapshot.pairingStatus);
      const requiresConfirmation = resolvedSnapshots.length > 1;

      if (requiresConfirmation || isReassignment || notPairedForAssignment) {
        setPhase({ kind: "found", snapshot, isReassignment, requiresConfirmation });
        return;
      }

      const assignment = onAssign(snapshot.deviceIdentifier);
      if (!assignment) {
        setPhase({ kind: "error", message: "Select a rack slot, then scan a miner." });
        return;
      }

      setPhase({
        kind: "assigned",
        snapshot,
        slotLabel: assignment.slotLabel,
        hasNextSlot: assignment.hasNextSlot,
      });
    },
    [eligibility, onAssign],
  );

  const { videoRef, status, errorMessage, detectFromBlob } = useQrScanner({
    active: cameraActive,
    restartKey: scannerRestartKey,
    scanRegionRef,
    onDetected: runLookup,
  });

  // Reset to a fresh scanning state whenever the modal (re)opens.
  useEffect(() => {
    if (show) {
      lookupSeq.current++;
      abortRef.current?.abort();
      // eslint-disable-next-line react-hooks/set-state-in-effect -- reset scan flow to its initial phase on each open
      setPhase({ kind: "scanning" });
    }
  }, [show]);

  // Stop any in-flight lookup loop when the modal unmounts (dismissed mid-scan).
  useEffect(() => () => abortRef.current?.abort(), []);

  const rescan = useCallback(() => {
    lookupSeq.current++;
    abortRef.current?.abort();
    setScannerRestartKey((key) => key + 1);
    setPhase({ kind: "scanning" });
  }, []);

  const handleFile = useCallback(
    async (file: File | undefined) => {
      if (!file) return;
      setPhase({ kind: "looking-up", identifier: "" });
      try {
        const rawValues = await detectFromBlob(file);
        if (rawValues.length) {
          await runLookup(rawValues);
        } else {
          setPhase({ kind: "not-found", identifier: "" });
        }
      } catch {
        setPhase({ kind: "error", message: "Could not read the photo. Try again with the QR code centered." });
      }
    },
    [detectFromBlob, runLookup],
  );

  const handleConfirm = useCallback(() => {
    if (phase.kind === "found") {
      const isReassignment = isMinerSnapshotIneligible(phase.snapshot, eligibility);
      if (phase.requiresConfirmation && !isReassignment) {
        const assignment = onAssign(phase.snapshot.deviceIdentifier);
        if (!assignment) {
          setPhase({ kind: "error", message: "Select a rack slot, then scan a miner." });
          return;
        }

        setPhase({
          kind: "assigned",
          snapshot: phase.snapshot,
          slotLabel: assignment.slotLabel,
          hasNextSlot: assignment.hasNextSlot,
        });
        return;
      }

      onConfirm(phase.snapshot.deviceIdentifier, isReassignment);
    }
  }, [phase, onAssign, onConfirm, eligibility]);

  const handleUndoAssignment = useCallback(() => {
    onUndoAssignment();
    rescan();
  }, [onUndoAssignment, rescan]);

  const handleScanNextSlot = useCallback(() => {
    if (onScanNextSlot()) rescan();
  }, [onScanNextSlot, rescan]);

  return (
    <ScanMinerQrModalView
      show={show}
      phase={phase}
      currentRackLabel={currentRackLabel}
      targetSlotLabel={targetSlotLabel}
      liveCamera={liveCamera}
      videoRef={videoRef}
      scanRegionRef={scanRegionRef}
      cameraStatus={status}
      cameraError={errorMessage}
      fileInputRef={fileInputRef}
      onDismiss={onDismiss}
      onConfirmFound={handleConfirm}
      onUndoAssignment={handleUndoAssignment}
      onScanNextSlot={handleScanNextSlot}
      onRescan={rescan}
      onFile={handleFile}
    />
  );
}
