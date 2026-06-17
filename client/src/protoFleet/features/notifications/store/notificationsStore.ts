import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import * as api from "@/protoFleet/features/notifications/api/notificationsApi";
import type { Channel } from "@/protoFleet/features/notifications/types";

interface NotificationsState {
  channels: Channel[];

  loading: boolean;
  loaded: boolean;

  refresh: () => Promise<void>;

  createChannel: (input: api.ChannelMutationInput) => Promise<Channel>;
  updateChannel: (input: api.ChannelMutationInput & { id: string }) => Promise<Channel>;
  testChannel: (input: api.ChannelMutationInput) => Promise<api.TestChannelResult>;
  removeChannel: (id: string) => Promise<void>;
}

const upsertById = <T extends { id: string }>(list: T[], next: T): T[] => {
  const idx = list.findIndex((item) => item.id === next.id);
  if (idx < 0) return [next, ...list];
  const copy = list.slice();
  copy[idx] = next;
  return copy;
};

export const useNotificationsStore = create<NotificationsState>()(
  immer((set) => ({
    channels: [],
    loading: false,
    loaded: false,

    refresh: async () => {
      set((state) => {
        state.loading = true;
      });
      try {
        const [channels] = await Promise.all([api.listChannels()]);
        set((state) => {
          state.channels = channels;
          state.loaded = true;
        });
      } finally {
        set((state) => {
          state.loading = false;
        });
      }
    },

    createChannel: async (input) => {
      const created = await api.createChannel(input);
      set((state) => {
        state.channels = upsertById(state.channels, created);
      });
      return created;
    },

    updateChannel: async (input) => {
      const updated = await api.updateChannel(input);
      set((state) => {
        state.channels = upsertById(state.channels, updated);
      });
      return updated;
    },

    testChannel: async (input) => {
      const result = await api.testChannel(input);
      // The server doesn't persist a per-channel validation state (a read can't
      // recover it cheaply), so reflect the test outcome on the cached saved
      // channel here; the badge stays "Not tested" until something is tested.
      if (input.id) {
        set((state) => {
          const channel = state.channels.find((c) => c.id === input.id);
          if (channel) {
            channel.validation_state = result.ok ? "ok" : "failed";
            channel.validation_error = result.ok ? undefined : result.error;
            if (result.ok) channel.validated_at = new Date().toISOString();
          }
        });
      }
      return result;
    },

    removeChannel: async (id) => {
      await api.deleteChannel(id);
      set((state) => {
        state.channels = state.channels.filter((c) => c.id !== id);
      });
    },
  })),
);

export const selectChannels = (s: NotificationsState) => s.channels;
