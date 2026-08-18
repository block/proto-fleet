import { useCallback, useState } from "react";
import { useChannelSelection } from "@/protoFleet/features/alerts/api/useChannelSelection";
import type { Channel, RoutingMode, RuleRouting } from "@/protoFleet/features/alerts/types";

export interface UseDeliveryRoutingResult {
  mode: RoutingMode;
  setMode: (mode: RoutingMode) => void;
  selectedIds: Set<string>;
  toggleChannel: (id: string) => void;
  channels: Channel[];
  channelsLoaded: boolean;
  // Bumped by reset; hosts key DeliveryPicker on it so its uncontrolled segment control remounts per editing session.
  sessionKey: number;
  // Seed from a rule's routing (or null for create defaults); hosts call it from their open-sync block.
  reset: (routing: RuleRouting | null) => void;
  // Error message when the current state can't be saved, else null.
  validate: () => string | null;
  toRuleRouting: () => RuleRouting;
}

// Owns the delivery-picker mechanics shared by the Add Rule and Edit delivery dialogs.
export function useDeliveryRouting(): UseDeliveryRoutingResult {
  const [mode, setMode] = useState<RoutingMode>("default");
  const [sessionKey, setSessionKey] = useState(0);
  const {
    channels,
    channelsLoaded,
    selectedIds,
    toggleChannel,
    reset: resetSelection,
  } = useChannelSelection(mode === "custom");

  const reset = useCallback(
    (routing: RuleRouting | null) => {
      resetSelection(routing?.channel_ids ?? []);
      setMode(routing?.mode ?? "default");
      setSessionKey((key) => key + 1);
    },
    [resetSelection],
  );

  const validate = useCallback((): string | null => {
    if (mode === "custom" && selectedIds.size === 0) {
      return "Pick at least one channel, or use All channels / In-app only";
    }
    return null;
  }, [mode, selectedIds]);

  const toRuleRouting = useCallback(
    (): RuleRouting => ({ mode, channel_ids: mode === "custom" ? [...selectedIds] : [] }),
    [mode, selectedIds],
  );

  return {
    mode,
    setMode,
    selectedIds,
    toggleChannel,
    channels,
    channelsLoaded,
    sessionKey,
    reset,
    validate,
    toRuleRouting,
  };
}
