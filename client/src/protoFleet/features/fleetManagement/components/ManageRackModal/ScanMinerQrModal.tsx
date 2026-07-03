import { useCallback, useEffect, useRef, useState } from "react";

import ScanMinerQrModalView, { type ScanPhase } from "./ScanMinerQrModalView";
import { lookupMinerBySerial } from "@/protoFleet/api/lookupMinerBySerial";
import { canUseLiveCamera, useQrScanner } from "@/protoFleet/features/fleetManagement/hooks/useQrScanner";
import { parseScannedSerial } from "@/protoFleet/features/fleetManagement/utils/parseScannedSerial";

interface ScanMinerQrModalProps {
  show: boolean;
  /** Label of the rack being edited; a miner already in a *different* rack is blocked. */
  currentRackLabel: string;
  onDismiss: () => void;
  onConfirm: (deviceIdentifier: string) => void;
}

/**
 * Container for the scan-a-miner-QR flow: owns camera access (via useQrScanner),
 * decoding, and the serial → miner lookup, driving the presentational
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

  const runLookup = useCallback(async (rawValue: string) => {
    const serial = parseScannedSerial(rawValue);
    if (!serial) {
      setPhase({ kind: "not-found", serial: rawValue.trim() });
      return;
    }
    const seq = ++lookupSeq.current;
    setPhase({ kind: "looking-up", serial });
    const result = await lookupMinerBySerial(serial);
    if (seq !== lookupSeq.current) return; // superseded
    switch (result.status) {
      case "found":
        setPhase({ kind: "found", snapshot: result.snapshot });
        break;
      case "notFound":
        setPhase({ kind: "not-found", serial });
        break;
      case "error":
        setPhase({ kind: "error", message: result.message });
        break;
    }
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
      setPhase({ kind: "looking-up", serial: "" });
      try {
        const rawValue = await detectFromBlob(file);
        if (rawValue) {
          await runLookup(rawValue);
        } else {
          setPhase({ kind: "not-found", serial: "" });
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
