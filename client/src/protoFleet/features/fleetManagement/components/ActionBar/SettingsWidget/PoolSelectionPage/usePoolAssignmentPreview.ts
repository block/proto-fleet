import { useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import {
  type DevicePoolPreview,
  type DeviceSelector,
  DeviceSelectorSchema,
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
  deviceIdentifiers: string[],
  poolConfig: PoolConfig | null,
  enabled: boolean,
): PoolAssignmentPreview => {
  const { previewMiningPoolAssignment } = useMinerCommand();
  const [previews, setPreviews] = useState<DevicePoolPreview[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Active when caller actually has something to preview. When false we
  // skip the RPC and zero out the returned values via the read-side
  // (rather than calling setState during the effect, which trips
  // react-hooks/set-state-in-effect).
  const isActive = enabled && poolConfig !== null && deviceIdentifiers.length > 0;

  useEffect(() => {
    if (!isActive || !poolConfig) {
      return;
    }

    if (timer.current !== null) {
      clearTimeout(timer.current);
    }
    timer.current = setTimeout(() => {
      const deviceSelector: DeviceSelector = create(DeviceSelectorSchema, {
        selectionType: {
          case: "includeDevices",
          value: create(DeviceIdentifierListSchema, { deviceIdentifiers }),
        },
      });
      setIsLoading(true);
      void previewMiningPoolAssignment({
        deviceSelector,
        poolConfig,
        onSuccess: (result) => {
          setPreviews(result);
          setError(undefined);
        },
        onError: (msg) => {
          setPreviews([]);
          setError(msg);
        },
      }).finally(() => {
        setIsLoading(false);
      });
    }, debounceMs);

    return () => {
      if (timer.current !== null) {
        clearTimeout(timer.current);
        timer.current = null;
      }
    };
  }, [isActive, deviceIdentifiers, poolConfig, previewMiningPoolAssignment]);

  const effectivePreviews = isActive ? previews : [];
  const effectiveError = isActive ? error : undefined;
  const hasMismatch = effectivePreviews.some(
    (d) => d.deviceWarning !== DeviceWarning.UNSPECIFIED || d.slots.some((s) => s.warning !== SlotWarning.UNSPECIFIED),
  );

  return { previews: effectivePreviews, hasMismatch, isLoading, error: effectiveError };
};
