import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

import { BarcodeDetector } from "barcode-detector/ponyfill";

import { initBarcodeScanner } from "@/protoFleet/features/fleetManagement/utils/initBarcodeScanner";

/**
 * Whether the current browsing context can open a live camera stream.
 *
 * `navigator.mediaDevices` is only defined in a secure context (HTTPS or
 * localhost). Fleet's default install serves plain HTTP over a LAN IP, where
 * this is `undefined` — so we feature-detect rather than assume, and the UI
 * falls back to file/photo capture (which needs no secure context).
 */
export function canUseLiveCamera(): boolean {
  return typeof navigator !== "undefined" && !!navigator.mediaDevices?.getUserMedia;
}

type ScanStatus = "idle" | "starting" | "scanning" | "error";

interface UseQrScannerOptions {
  /** Called with the raw decoded text when a code is found. */
  onDetected: (rawValue: string) => void;
  /** When false, the scanner stays torn down (e.g. modal closed). */
  active: boolean;
}

interface UseQrScannerResult {
  videoRef: RefObject<HTMLVideoElement | null>;
  status: ScanStatus;
  /** Populated when status === "error"; a user-facing message. */
  errorMessage: string;
  /** Decode a still image (File/Blob) from the photo-capture fallback. */
  detectFromBlob: (blob: Blob) => Promise<string | null>;
}

const SCAN_INTERVAL_MS = 250;

/**
 * Drive a live QR/barcode scan session against the device camera.
 *
 * Lifecycle is tied to `active`: turning it on requests the rear camera and
 * starts a polling decode loop; turning it off (or unmount) stops all tracks
 * and cancels the loop. Detection stops on the first hit — the caller decides
 * whether to resume by toggling `active`.
 *
 * `detectFromBlob` is exposed for the HTTP fallback path, where there is no
 * live stream but the same WASM/native decoder still applies to a captured
 * photo.
 */
export function useQrScanner({ onDetected, active }: UseQrScannerOptions): UseQrScannerResult {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const detectorRef = useRef<BarcodeDetector | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const detectedRef = useRef(false);
  const onDetectedRef = useRef(onDetected);

  const [status, setStatus] = useState<ScanStatus>("idle");
  const [errorMessage, setErrorMessage] = useState("");

  // Keep the latest callback without retriggering the start/stop effect.
  useEffect(() => {
    onDetectedRef.current = onDetected;
  }, [onDetected]);

  const getDetector = useCallback((): BarcodeDetector => {
    if (!detectorRef.current) {
      initBarcodeScanner();
      detectorRef.current = new BarcodeDetector({ formats: ["qr_code"] });
    }
    return detectorRef.current;
  }, []);

  const detectFromBlob = useCallback(
    async (blob: Blob): Promise<string | null> => {
      const results = await getDetector().detect(blob);
      return results[0]?.rawValue ?? null;
    },
    [getDetector],
  );

  const stop = useCallback(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
    if (streamRef.current) {
      for (const track of streamRef.current.getTracks()) track.stop();
      streamRef.current = null;
    }
    if (videoRef.current) {
      videoRef.current.srcObject = null;
    }
  }, []);

  useEffect(() => {
    if (!active || !canUseLiveCamera()) return;

    let cancelled = false;
    detectedRef.current = false;
    // Resetting scan status is the effect's purpose: it synchronizes React
    // state with the freshly-(re)started camera stream (an external system).
    /* eslint-disable react-hooks/set-state-in-effect -- initialize UI state for a new camera session */
    setErrorMessage("");
    setStatus("starting");
    /* eslint-enable react-hooks/set-state-in-effect */

    const start = async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: { ideal: "environment" } },
          audio: false,
        });
        if (cancelled) {
          for (const track of stream.getTracks()) track.stop();
          return;
        }
        streamRef.current = stream;
        const video = videoRef.current;
        if (video) {
          video.srcObject = stream;
          await video.play().catch(() => {
            // Autoplay can reject if the element isn't visible yet; the
            // interval loop below still reads frames once it plays.
          });
        }

        const detector = getDetector();
        setStatus("scanning");

        intervalRef.current = setInterval(async () => {
          const el = videoRef.current;
          if (!el || detectedRef.current || el.readyState < 2) return;
          try {
            const results = await detector.detect(el);
            const value = results[0]?.rawValue;
            if (value && !detectedRef.current) {
              detectedRef.current = true;
              onDetectedRef.current(value);
            }
          } catch {
            // Transient decode failures (e.g. a blurry frame) are expected;
            // keep polling.
          }
        }, SCAN_INTERVAL_MS);
      } catch (err) {
        if (cancelled) return;
        setStatus("error");
        setErrorMessage(cameraErrorMessage(err));
      }
    };

    void start();

    return () => {
      cancelled = true;
      stop();
      setStatus("idle");
    };
  }, [active, getDetector, stop]);

  return { videoRef, status, errorMessage, detectFromBlob };
}

/** Map a getUserMedia rejection to a short, actionable message. */
function cameraErrorMessage(err: unknown): string {
  if (err instanceof DOMException) {
    switch (err.name) {
      case "NotAllowedError":
      case "SecurityError":
        return "Camera access was blocked. Allow camera permission in your browser and try again.";
      case "NotFoundError":
      case "OverconstrainedError":
        return "No camera was found on this device.";
      case "NotReadableError":
        return "The camera is already in use by another app.";
    }
  }
  return "Could not start the camera. You can take a photo instead.";
}
