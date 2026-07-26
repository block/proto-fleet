import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useChannels } from "@/protoFleet/features/alerts/api/useChannels";
import type { Channel, RoutingMode, RuleRouting } from "@/protoFleet/features/alerts/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

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
  const { channels, refresh } = useChannels();
  const [mode, setMode] = useState<RoutingMode>("default");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [channelsLoaded, setChannelsLoaded] = useState(false);
  const [sessionKey, setSessionKey] = useState(0);
  const sessionFetchedRef = useRef(false);

  // Channels are only rendered in custom mode, so fetch lazily, once per session.
  useEffect(() => {
    if (mode !== "custom" || sessionFetchedRef.current) return;
    sessionFetchedRef.current = true;
    void refresh()
      .then(() => setChannelsLoaded(true))
      .catch((error) => {
        pushToast({
          message: getErrorMessage(error, "Failed to load channels"),
          status: STATUSES.error,
        });
      });
  }, [mode, refresh]);

  // Derive the live selection: an id for a channel deleted since the rules cache was fetched renders
  // no checkbox, so it could never be deselected and every save would fail server-side.
  const liveSelectedIds = useMemo(() => {
    if (!channelsLoaded) return selectedIds;
    const live = new Set(channels.map((c) => c.id));
    return new Set([...selectedIds].filter((id) => live.has(id)));
  }, [selectedIds, channels, channelsLoaded]);

  const reset = useCallback((routing: RuleRouting | null) => {
    sessionFetchedRef.current = false;
    setChannelsLoaded(false);
    setMode(routing?.mode ?? "default");
    setSelectedIds(new Set(routing?.channel_ids ?? []));
    setSessionKey((key) => key + 1);
  }, []);

  const toggleChannel = useCallback((id: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const validate = useCallback((): string | null => {
    if (mode === "custom" && liveSelectedIds.size === 0) {
      return "Pick at least one channel, or use All channels / In-app only";
    }
    return null;
  }, [mode, liveSelectedIds]);

  const toRuleRouting = useCallback(
    (): RuleRouting => ({ mode, channel_ids: mode === "custom" ? [...liveSelectedIds] : [] }),
    [mode, liveSelectedIds],
  );

  return {
    mode,
    setMode,
    selectedIds: liveSelectedIds,
    toggleChannel,
    channels,
    channelsLoaded,
    sessionKey,
    reset,
    validate,
    toRuleRouting,
  };
}
