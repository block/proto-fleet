import { useCallback, useState } from "react";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type { Channel } from "@/protoFleet/features/alerts/types";

const upsertById = (list: Channel[], next: Channel): Channel[] => {
  const idx = list.findIndex((item) => item.id === next.id);
  if (idx < 0) return [next, ...list];
  const copy = list.slice();
  copy[idx] = next;
  return copy;
};

export interface UseChannelsResult {
  channels: Channel[];
  loading: boolean;
  refresh: () => Promise<void>;
  createChannel: (input: api.ChannelMutationInput) => Promise<Channel>;
  updateChannel: (input: api.ChannelMutationInput & { id: string }) => Promise<Channel>;
  testChannel: (input: api.ChannelMutationInput) => Promise<api.TestChannelResult>;
  removeChannel: (id: string) => Promise<void>;
}

/**
 * Owns the notifications channels list and its CRUD/test operations for the
 * Alerts page. Plain component state via the api module — protoFleet keeps
 * Zustand for shared UI/session state, not per-feature server data.
 */
export function useChannels(): UseChannelsResult {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setChannels(await api.listChannels());
    } finally {
      setLoading(false);
    }
  }, []);

  const createChannel = useCallback(async (input: api.ChannelMutationInput) => {
    const created = await api.createChannel(input);
    setChannels((prev) => upsertById(prev, created));
    return created;
  }, []);

  const updateChannel = useCallback(async (input: api.ChannelMutationInput & { id: string }) => {
    const updated = await api.updateChannel(input);
    setChannels((prev) => upsertById(prev, updated));
    return updated;
  }, []);

  const testChannel = useCallback(async (input: api.ChannelMutationInput) => {
    const result = await api.testChannel(input);
    // The server doesn't persist a per-channel validation state, so reflect the
    // test outcome on the cached saved channel; the badge stays "Not tested" until tested.
    if (input.id) {
      setChannels((prev) =>
        prev.map((channel) =>
          channel.id === input.id
            ? {
                ...channel,
                validation_state: result.ok ? "ok" : "failed",
                validation_error: result.ok ? undefined : result.error,
                validated_at: result.ok ? new Date().toISOString() : channel.validated_at,
              }
            : channel,
        ),
      );
    }
    return result;
  }, []);

  const removeChannel = useCallback(async (id: string) => {
    await api.deleteChannel(id);
    setChannels((prev) => prev.filter((channel) => channel.id !== id));
  }, []);

  return { channels, loading, refresh, createChannel, updateChannel, testChannel, removeChannel };
}
