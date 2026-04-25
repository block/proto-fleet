import { useEffect, useRef, useState } from "react";
import {
  type DevicePoolPreview,
  type DeviceSelector,
  DeviceWarning,
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
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Monotonic request token. Each scheduled RPC captures the latest
  // value; in-flight responses ignore themselves once a newer request
  // has been issued. Without this, a slow response from preview N can
  // overwrite the state set by preview N+1, falsely re-enabling Save
  // for an assignment the latest preflight would actually reject.
  const latestRequestId = useRef(0);

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
      setIsLoading(true);
      void previewMiningPoolAssignment({
        deviceSelector,
        poolConfig,
        onSuccess: (result) => {
          if (requestId !== latestRequestId.current) {
            return;
          }
          setPreviews(result);
          setError(undefined);
        },
        onError: (msg) => {
          if (requestId !== latestRequestId.current) {
            return;
          }
          setPreviews([]);
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
    };
  }, [isActive, deviceSelector, poolConfig, previewMiningPoolAssignment]);

  const effectivePreviews = isActive ? previews : [];
  const effectiveError = isActive ? error : undefined;
  // Treat any error or in-flight state as "not saveable yet" — a
  // transient preview failure that left previews=[] would otherwise
  // pass the every() check below and re-enable Save. The save path
  // would still get rejected by the server, but the UI should match.
  const hasMismatch =
    effectiveError !== undefined ||
    isLoading ||
    effectivePreviews.some(
      (d) =>
        d.deviceWarning !== DeviceWarning.UNSPECIFIED || d.slots.some((s) => s.warning !== SlotWarning.UNSPECIFIED),
    );

  return { previews: effectivePreviews, hasMismatch, isLoading, error: effectiveError };
};
