import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

import { BarcodeDetector } from "barcode-detector/ponyfill";

import { initBarcodeScanner } from "@/protoFleet/features/fleetManagement/utils/initBarcodeScanner";
import {
  getObjectCoverSourceCrop,
  getObjectCoverSourceCropForRegion,
} from "@/protoFleet/features/fleetManagement/utils/objectCoverSourceCrop";

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
  /**
   * Called with every raw decoded value in the frame when at least one code is
   * found. A single label or rack shot can carry more than one barcode (e.g. a
   * serial and a model/asset code), and the detector's ordering isn't
   * guaranteed — so the caller should try each value rather than assume the
   * first is the one to resolve.
   */
  onDetected: (rawValues: string[]) => void;
  /** When false, the scanner stays torn down (e.g. modal closed). */
  active: boolean;
  /** Increment to restart a still-active camera session, such as after an error. */
  restartKey?: number;
  /** Optional visible scan target inside the preview. Live detection is cropped
   *  to this region so nearby labels outside the reticle are ignored. */
  scanRegionRef?: RefObject<HTMLElement | null>;
}

interface UseQrScannerResult {
  videoRef: RefObject<HTMLVideoElement | null>;
  status: ScanStatus;
  /** Populated when status === "error"; a user-facing message. */
  errorMessage: string;
  /** Decode a still image (File/Blob) from the photo-capture fallback; returns
   *  every decoded value (see onDetected). */
  detectFromBlob: (blob: Blob) => Promise<string[]>;
}

const SCAN_INTERVAL_MS = 250;
// A blurry/empty frame *resolves* with no results; only a genuinely broken
// decoder (e.g. the WASM asset can't be fetched/instantiated on-prem) *throws*.
// After this many consecutive throws (~4s) we give up the live loop and surface
// an error so the photo-capture fallback appears instead of an endless viewfinder.
const MAX_CONSECUTIVE_DECODE_FAILURES = 16;

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
export function useQrScanner({
  onDetected,
  active,
  restartKey = 0,
  scanRegionRef,
}: UseQrScannerOptions): UseQrScannerResult {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const detectorRef = useRef<BarcodeDetector | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // Offscreen canvas used to crop each frame to the visible preview area before
  // decoding (see detectionFrame).
  const cropCanvasRef = useRef<HTMLCanvasElement | null>(null);
  const detectedRef = useRef(false);
  // Guards against overlapping decodes: detect() can take longer than
  // SCAN_INTERVAL_MS (notably the WASM fallback on mobile), and without this a
  // slow frame would let interval ticks stack up and spike CPU/battery.
  const detectingRef = useRef(false);
  // Consecutive decode *throws* (reset on any resolve). Trips the error state
  // when the decoder is persistently broken rather than just seeing no code.
  const decodeFailuresRef = useRef(0);
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
      // QR is the common case, but some vendors (e.g. Bitmain) print the serial
      // on a 1D barcode, so accept the alphanumeric linear symbologies used on
      // equipment labels alongside the 2D formats. The decoded value flows
      // through parseScannedIdentifier the same way regardless of symbology.
      detectorRef.current = new BarcodeDetector({
        formats: ["qr_code", "data_matrix", "code_128", "code_39", "code_93"],
      });
    }
    return detectorRef.current;
  }, []);

  const detectFromBlob = useCallback(
    async (blob: Blob): Promise<string[]> => {
      const results = await getDetector().detect(blob);
      return results.map((r) => r.rawValue).filter(Boolean);
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
    decodeFailuresRef.current = 0;
    // Resetting scan status is the effect's purpose: it synchronizes React
    // state with the freshly-(re)started camera stream (an external system).
    /* eslint-disable react-hooks/set-state-in-effect -- initialize UI state for a new camera session */
    setErrorMessage("");
    setStatus("starting");
    /* eslint-enable react-hooks/set-state-in-effect */

    const start = async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          // Request a high resolution: a dense 1D barcode (e.g. an Antminer
          // serial in Code 128) needs enough horizontal pixels across its bars
          // to decode, where a QR would survive a lower-res frame. `ideal` so
          // devices without a 1080p camera gracefully fall back.
          video: { facingMode: { ideal: "environment" }, width: { ideal: 1920 }, height: { ideal: 1080 } },
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

        // Cleanup may have run while getUserMedia/play() were pending — its
        // clearInterval saw no interval yet. Bail before installing one so we
        // don't leave an orphaned scan loop running after teardown.
        if (cancelled) return;

        const detector = getDetector();
        setStatus("scanning");

        intervalRef.current = setInterval(async () => {
          const el = videoRef.current;
          if (!el || detectedRef.current || detectingRef.current || el.readyState < 2) return;
          detectingRef.current = true;
          try {
            const results = await detector.detect(detectionFrame(el, cropCanvasRef, scanRegionRef));
            // The effect may have torn down while detect() was in flight; don't
            // fire onDetected (a lookup + state update) after teardown.
            if (cancelled) return;
            decodeFailuresRef.current = 0;
            const values = results.map((r) => r.rawValue).filter(Boolean);
            if (values.length && !detectedRef.current) {
              detectedRef.current = true;
              onDetectedRef.current(values);
            }
          } catch {
            // A blurry frame resolves empty; a *throw* means the decoder itself
            // failed. Tolerate a few (transient), but stop and surface an error
            // on persistent failure so the photo fallback becomes reachable.
            if (cancelled) return;
            decodeFailuresRef.current += 1;
            if (decodeFailuresRef.current >= MAX_CONSECUTIVE_DECODE_FAILURES) {
              stop();
              setStatus("error");
              setErrorMessage("Couldn't read the camera feed. Try taking a photo instead.");
            }
          } finally {
            detectingRef.current = false;
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
  }, [active, getDetector, restartKey, scanRegionRef, stop]);

  return { videoRef, status, errorMessage, detectFromBlob };
}

/**
 * The live preview renders the camera stream with `object-cover`, but
 * `detect()` reads the full video frame. Decode only the source pixels in the
 * visible scan target so labels outside the reticle cannot be assigned while
 * the operator is aiming at a different label.
 */
function detectionFrame(
  video: HTMLVideoElement,
  canvasRef: RefObject<HTMLCanvasElement | null>,
  scanRegionRef?: RefObject<HTMLElement | null>,
): HTMLVideoElement | HTMLCanvasElement {
  const vw = video.videoWidth;
  const vh = video.videoHeight;
  const rect = video.getBoundingClientRect();
  const renderedWidth = rect.width || video.clientWidth;
  const renderedHeight = rect.height || video.clientHeight;
  const scanRegion = scanRegionRef?.current;
  const scanRegionRect = scanRegion?.getBoundingClientRect();
  const crop = scanRegionRect
    ? getObjectCoverSourceCropForRegion({
        sourceWidth: vw,
        sourceHeight: vh,
        renderedWidth,
        renderedHeight,
        renderedRegionX: scanRegionRect.left - rect.left,
        renderedRegionY: scanRegionRect.top - rect.top,
        renderedRegionWidth: scanRegionRect.width,
        renderedRegionHeight: scanRegionRect.height,
      })
    : getObjectCoverSourceCrop({ sourceWidth: vw, sourceHeight: vh, renderedWidth, renderedHeight });

  if (!crop) return video;

  const isFullFrame = crop.sx === 0 && crop.sy === 0 && crop.sw === vw && crop.sh === vh;
  if (isFullFrame) return video;

  let canvas = canvasRef.current;
  if (!canvas) {
    canvas = document.createElement("canvas");
    canvasRef.current = canvas;
  }
  canvas.width = Math.max(1, Math.round(crop.sw));
  canvas.height = Math.max(1, Math.round(crop.sh));
  const ctx = canvas.getContext("2d");
  if (!ctx) return video;
  ctx.drawImage(video, crop.sx, crop.sy, crop.sw, crop.sh, 0, 0, canvas.width, canvas.height);
  return canvas;
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
