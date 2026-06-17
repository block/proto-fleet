import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import * as api from "@/protoFleet/features/notifications/api/notificationsApi";
import type {
  Channel,
  MaintenanceWindow,
  MaintenanceWindowScope,
  MaintenanceWindowWithActive,
  Rule,
} from "@/protoFleet/features/notifications/types";

interface NotificationsState {
  channels: Channel[];
  rules: Rule[];
  maintenanceWindows: MaintenanceWindowWithActive[];

  loading: boolean;
  loaded: boolean;

  refresh: () => Promise<void>;

  createChannel: (input: api.ChannelMutationInput) => Promise<Channel>;
  updateChannel: (input: api.ChannelMutationInput & { id: string }) => Promise<Channel>;
  removeChannel: (id: string) => Promise<void>;

  pauseRule: (id: string) => Promise<void>;
  resumeRule: (id: string) => Promise<void>;

  createMaintenanceWindow: (input: api.MaintenanceWindowMutationInput) => Promise<MaintenanceWindow>;
  updateMaintenanceWindow: (input: api.MaintenanceWindowMutationInput & { id: string }) => Promise<MaintenanceWindow>;
  removeMaintenanceWindow: (id: string) => Promise<void>;
}

const withActive = (s: MaintenanceWindow): MaintenanceWindowWithActive => {
  const now = Date.now();
  const start = new Date(s.starts_at).getTime();
  const end = s.ends_at ? new Date(s.ends_at).getTime() : Infinity;
  return { ...s, active: now >= start && now < end };
};

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
    rules: [],
    maintenanceWindows: [],
    loading: false,
    loaded: false,

    refresh: async () => {
      set((state) => {
        state.loading = true;
      });
      try {
        const [channels, rules, maintenanceWindows] = await Promise.all([
          api.listChannels(),
          api.listRules(),
          api.listMaintenanceWindows(),
        ]);
        set((state) => {
          state.channels = channels;
          state.rules = rules;
          state.maintenanceWindows = maintenanceWindows.map(withActive);
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

    pauseRule: async (id) => {
      const updated = await api.pauseRule(id);
      set((state) => {
        state.rules = upsertById(state.rules, updated);
      });
    },

    resumeRule: async (id) => {
      const updated = await api.resumeRule(id);
      set((state) => {
        state.rules = upsertById(state.rules, updated);
      });
    },

    createMaintenanceWindow: async (input) => {
      const created = await api.createMaintenanceWindow(input);
      set((state) => {
        state.maintenanceWindows = upsertById(state.maintenanceWindows, withActive(created));
      });
      return created;
    },

    updateMaintenanceWindow: async (input) => {
      const updated = await api.updateMaintenanceWindow(input);
      set((state) => {
        state.maintenanceWindows = upsertById(state.maintenanceWindows, withActive(updated));
      });
      return updated;
    },

    removeMaintenanceWindow: async (id) => {
      await api.deleteMaintenanceWindow(id);
      set((state) => {
        state.maintenanceWindows = state.maintenanceWindows.filter((s) => s.id !== id);
      });
    },
  })),
);

export const computeMaintenanceWindowActive = withActive;

export type { MaintenanceWindowScope };

export const selectChannels = (s: NotificationsState) => s.channels;
export const selectRules = (s: NotificationsState) => s.rules;
export const selectMaintenanceWindows = (s: NotificationsState) => s.maintenanceWindows;
