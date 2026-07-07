import { useCallback, useEffect, useRef, useState } from "react";

import ScanMinerQrModalView, { type ScanPhase } from "./ScanMinerQrModalView";
import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
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
  // Aborts in-flight lookups (a multi-candidate loop issues several) on rescan
  // or unmount, so a mid-scan dismiss doesn't keep hitting the server.
  const abortRef = useRef<AbortController | null>(null);

  // Only run the live camera while we're actively scanning (not while showing
  // a result). Toggling this tears the stream down between scans.
  const cameraActive = show && liveCamera && phase.kind === "scanning";

  // A single frame/photo can decode more than one barcode (e.g. a serial plus a
  // model/asset code), and the detector's ordering isn't guaranteed — so try
  // each decoded value against the lookup and only report not-found once none
  // resolve, rather than committing to the first.
  const runLookup = useCallback(async (rawValues: string[]) => {
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
    for (const { value, type } of candidates) {
      const result = await lookupMinerByIdentifier(value, type, controller.signal);
      if (seq !== lookupSeq.current || controller.signal.aborted) return; // superseded / aborted
      if (result.status === "found") {
        setPhase({ kind: "found", snapshot: result.snapshot });
        return;
      }
      if (result.status === "error") {
        // A transport/server failure will hit the remaining candidates too —
        // surface it now instead of waiting through N sequential failures.
        setPhase({ kind: "error", message: result.message });
        return;
      }
      // notFound → try the next candidate
    }
    setPhase({ kind: "not-found", identifier: candidates[0].value });
  }, []);

  const { videoRef, status, errorMessage, detectFromBlob } = useQrScanner({
    active: cameraActive,
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
        setPhase({ kind: "error", message: "Could not read the photo. Try again with the code centered." });
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
