import { useCallback, useEffect, useRef, useState } from "react";

import ScanMinerQrModalView, { type ScanPhase } from "./ScanMinerQrModalView";
import { lookupMinerByIdentifier } from "@/protoFleet/api/lookupMinerByIdentifier";
import { canUseLiveCamera, useQrScanner } from "@/protoFleet/features/fleetManagement/hooks/useQrScanner";
import { parseScannedIdentifier } from "@/protoFleet/features/fleetManagement/utils/parseScannedIdentifier";

interface ScanMinerQrModalProps {
  show: boolean;
  /** Label of the rack being edited; a miner already in a *different* rack is blocked. */
  currentRackLabel: string;
  onDismiss: () => void;
  onConfirm: (deviceIdentifier: string) => void;
}

/**
 * Container for the scan-a-miner-QR flow: owns camera access (via useQrScanner),
 * decoding, and the identifier → miner lookup, driving the presentational
 * ScanMinerQrModalView through a `ScanPhase` state machine.
 */
export default function ScanMinerQrModal({ show, currentRackLabel, onDismiss, onConfirm }: ScanMinerQrModalProps) {
  const [phase, setPhase] = useState<ScanPhase>({ kind: "scanning" });
  const liveCamera = canUseLiveCamera();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Guards the async lookup against a modal close / rescan mid-flight.
  const lookupSeq = useRef(0);

  // Only run the live camera while we're actively scanning (not while showing
  // a result). Toggling this tears the stream down between scans.
  const cameraActive = show && liveCamera && phase.kind === "scanning";

  // A single frame/photo can decode more than one barcode (e.g. a serial plus a
  // model/asset code), and the detector's ordering isn't guaranteed — so try
  // each decoded value against the lookup and only report not-found once none
  // resolve, rather than committing to the first.
  const runLookup = useCallback(async (rawValues: string[]) => {
    const seq = ++lookupSeq.current;
    const candidates = rawValues.map((raw) => ({ raw, ...parseScannedIdentifier(raw) })).filter((c) => c.value);
    if (candidates.length === 0) {
      setPhase({ kind: "not-found", identifier: rawValues[0]?.trim() ?? "" });
      return;
    }
    setPhase({ kind: "looking-up", identifier: candidates[0].value });
    let lastError: string | null = null;
    for (const { value, type } of candidates) {
      const result = await lookupMinerByIdentifier(value, type);
      if (seq !== lookupSeq.current) return; // superseded by a newer scan / close
      if (result.status === "found") {
        setPhase({ kind: "found", snapshot: result.snapshot });
        return;
      }
      if (result.status === "error") lastError = result.message;
      // notFound → keep trying the remaining decoded values
    }
    setPhase(
      lastError ? { kind: "error", message: lastError } : { kind: "not-found", identifier: candidates[0].value },
    );
  }, []);

  const { videoRef, status, errorMessage, detectFromBlob } = useQrScanner({
    active: cameraActive,
    onDetected: runLookup,
  });

  // Reset to a fresh scanning state whenever the modal (re)opens.
  useEffect(() => {
    if (show) {
      lookupSeq.current++;
      // eslint-disable-next-line react-hooks/set-state-in-effect -- reset scan flow to its initial phase on each open
      setPhase({ kind: "scanning" });
    }
  }, [show]);

  const rescan = useCallback(() => {
    lookupSeq.current++;
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
    if (phase.kind === "found") onConfirm(phase.snapshot.deviceIdentifier);
  }, [phase, onConfirm]);

  return (
    <ScanMinerQrModalView
      show={show}
      phase={phase}
      currentRackLabel={currentRackLabel}
      liveCamera={liveCamera}
      videoRef={videoRef}
      cameraStatus={status}
      cameraError={errorMessage}
      fileInputRef={fileInputRef}
      onDismiss={onDismiss}
      onConfirm={handleConfirm}
      onRescan={rescan}
      onFile={handleFile}
    />
  );
}
