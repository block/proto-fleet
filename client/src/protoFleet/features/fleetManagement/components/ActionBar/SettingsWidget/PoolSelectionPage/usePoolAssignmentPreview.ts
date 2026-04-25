import { useEffect, useRef, useState } from "react";
import {
  type DevicePoolPreview,
  type DeviceSelector,
  DeviceWarning,
  PreviewSkipReason,
  SlotWarning,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { PoolConfig, useMinerCommand } from "@/protoFleet/api/useMinerCommand";

// usePoolAssignmentPreview runs PreviewMiningPoolAssignment on every
// pool-or-scope change and exposes the typed per-device results plus a
// boolean "any warning?" flag. The caller disables Save when hasMismatch
// is true, which gives preview/commit parity by construction (the server
// rejects the same batch with FAILED_PRECONDITION).
//
// Protocol mismatches (SV2 pool + SV1 device + proxy disabled) surface
// as SlotWarning.SV2_NOT_SUPPORTED; multi-proxied slots on one SV1
// device surface as DeviceWarning.MULTIPLE_SV2_SLOTS_PROXIED. Both flow
// through the structured enums rather than free-form strings.
export interface PoolAssignmentPreview {
  previews: DevicePoolPreview[];
  hasMismatch: boolean;
  isLoading: boolean;
  error?: string;
  // True when the server short-circuited preview (selector exceeded
  // the device cap). The UI should still allow Save in that case —
  // commit-time preflight is authoritative — but can show a hint
  // that per-device detail isn't available for the current selection.
  previewSkipped: boolean;
}

// debounceMs smooths rapid pool-reorder and scope-change edits — drag-
// and-drop reorder triggers a cascade of setAssignedPoolData calls, and
// without a debounce each tick fires a preview RPC.
const debounceMs = 300;

export const usePoolAssignmentPreview = (
  deviceSelector: DeviceSelector | undefined,
  poolConfig: PoolConfig | null,
  enabled: boolean,
): PoolAssignmentPreview => {
  const { previewMiningPoolAssignment } = useMinerCommand();
  const [previews, setPreviews] = useState<DevicePoolPreview[]>([]);
  const [skipReason, setSkipReason] = useState<PreviewSkipReason>(PreviewSkipReason.UNSPECIFIED);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Monotonic request token. Each scheduled RPC captures the latest
  // value; in-flight responses ignore themselves once a newer request
  // has been issued. Without this, a slow response from preview N can
  // overwrite the state set by preview N+1, falsely re-enabling Save
  // for an assignment the latest preflight would actually reject.
  const latestRequestId = useRef(0);
  // Abort the previous in-flight RPC when a new one supersedes it so
  // server-side work for stale previews stops as soon as we know the
  // result is irrelevant. Without this, every pool-reorder during a
  // slow preview compounds load on the API; with it, only the latest
  // one keeps running.
  const inFlight = useRef<AbortController | null>(null);

  // Active when caller actually has something to preview. The deviceSelector
  // is the same one the commit path uses, so previewing an "allDevices"
  // selector evaluates the full server-resolved fleet rather than the
  // currently-loaded subset that an "includeDevices" reconstruction
  // would have used.
  const isActive = enabled && poolConfig !== null && deviceSelector !== undefined;

  useEffect(() => {
    if (!isActive || !poolConfig || !deviceSelector) {
      return;
    }

    if (timer.current !== null) {
      clearTimeout(timer.current);
    }
    timer.current = setTimeout(() => {
      latestRequestId.current += 1;
      const requestId = latestRequestId.current;
      // Cancel any preview still in flight before kicking off the new
      // one — server-side work stops as soon as the abort fires.
      inFlight.current?.abort();
      const controller = new AbortController();
      inFlight.current = controller;
      setIsLoading(true);
      void previewMiningPoolAssignment({
        deviceSelector,
        poolConfig,
        signal: controller.signal,
        onSuccess: (result, skipped) => {
          if (requestId !== latestRequestId.current) {
            return;
          }
          setPreviews(result);
          setSkipReason(skipped);
          setError(undefined);
        },
        onError: (msg) => {
          if (requestId !== latestRequestId.current) {
            return;
          }
          setPreviews([]);
          setSkipReason(PreviewSkipReason.UNSPECIFIED);
          setError(msg);
        },
      }).finally(() => {
        if (requestId !== latestRequestId.current) {
          return;
        }
        setIsLoading(false);
      });
    }, debounceMs);

    return () => {
      if (timer.current !== null) {
        clearTimeout(timer.current);
        timer.current = null;
      }
      // Aborting the in-flight RPC on unmount/effect-rerun stops the
      // server doing work for a preview the consumer no longer cares
      // about.
      inFlight.current?.abort();
      inFlight.current = null;
    };
  }, [isActive, deviceSelector, poolConfig, previewMiningPoolAssignment]);

  const effectivePreviews = isActive ? previews : [];
  const effectiveError = isActive ? error : undefined;
  const previewSkipped = isActive && skipReason !== PreviewSkipReason.UNSPECIFIED;
  // Block Save only on real preflight slot/device warnings. Preview
  // is read-only — a transport failure (timeout, abort, 5xx) doesn't
  // tell us anything about whether commit would succeed, and the
  // commit RPC reruns the authoritative server-side preflight either
  // way. Lumping transport errors into hasMismatch lets a transient
  // network blip lock operators out of urgent pool rotations.
  const hasMismatch = effectivePreviews.some(
    (d) => d.deviceWarning !== DeviceWarning.UNSPECIFIED || d.slots.some((s) => s.warning !== SlotWarning.UNSPECIFIED),
  );

  return { previews: effectivePreviews, hasMismatch, isLoading, error: effectiveError, previewSkipped };
};
