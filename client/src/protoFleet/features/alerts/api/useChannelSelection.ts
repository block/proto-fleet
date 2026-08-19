import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useChannels } from "@/protoFleet/features/alerts/api/useChannels";
import type { Channel } from "@/protoFleet/features/alerts/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export interface UseChannelSelectionResult {
  // Live selection: ids for channels deleted since they were cached are dropped — a stale id
  // renders no checkbox, so it could never be deselected and every save would fail server-side.
  selectedIds: Set<string>;
  toggleChannel: (id: string) => void;
  channels: Channel[];
  channelsLoaded: boolean;
  // Seed the selection for a new editing session; also re-arms the lazy fetch.
  reset: (ids: Iterable<string>) => void;
}

// Owns the channel-selection session shared by the delivery picker and the maintenance-window
// modal: channels only render while `active` (the host's custom/selected mode), so they are
// fetched lazily, once per session.
export function useChannelSelection(active: boolean): UseChannelSelectionResult {
  const { channels, refresh } = useChannels();
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [channelsLoaded, setChannelsLoaded] = useState(false);
  const sessionFetchedRef = useRef(false);

  useEffect(() => {
    if (!active || sessionFetchedRef.current) return;
    sessionFetchedRef.current = true;
    void refresh()
      .then(() => setChannelsLoaded(true))
      .catch((error) => {
        // Re-arm the fetch: the effect only re-fires when `active` flips, so leaving this
        // set would pin the picker on its loading state for the rest of the session after
        // one transient failure. Toggling back into the selected mode retries instead.
        sessionFetchedRef.current = false;
        pushToast({
          message: getErrorMessage(error, "Failed to load channels"),
          status: STATUSES.error,
        });
      });
  }, [active, refresh]);

  const liveSelectedIds = useMemo(() => {
    if (!channelsLoaded) return selectedIds;
    const live = new Set(channels.map((c) => c.id));
    return new Set([...selectedIds].filter((id) => live.has(id)));
  }, [selectedIds, channels, channelsLoaded]);

  const reset = useCallback((ids: Iterable<string>) => {
    sessionFetchedRef.current = false;
    setChannelsLoaded(false);
    setSelectedIds(new Set(ids));
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

  return { selectedIds: liveSelectedIds, toggleChannel, channels, channelsLoaded, reset };
}
