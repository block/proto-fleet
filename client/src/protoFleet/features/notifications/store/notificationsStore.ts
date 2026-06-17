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

    removeChannel: async (id) => {
      await api.deleteChannel(id);
      set((state) => {
        state.channels = state.channels.filter((c) => c.id !== id);
      });
    },
  })),
);

export const selectChannels = (s: NotificationsState) => s.channels;
