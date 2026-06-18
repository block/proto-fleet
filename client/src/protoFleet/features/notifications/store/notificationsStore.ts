import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import * as api from "@/protoFleet/features/notifications/api/notificationsApi";
import type {
  MaintenanceWindow,
  MaintenanceWindowScope,
  MaintenanceWindowWithActive,
  Rule,
} from "@/protoFleet/features/notifications/types";

interface NotificationsState {
  rules: Rule[];
  maintenanceWindows: MaintenanceWindowWithActive[];

  loading: boolean;
  loaded: boolean;

  refresh: () => Promise<void>;

  pauseRule: (id: string) => Promise<void>;
  resumeRule: (id: string) => Promise<void>;

  createMaintenanceWindow: (input: api.MaintenanceWindowMutationInput) => Promise<MaintenanceWindow>;
  updateMaintenanceWindow: (input: api.MaintenanceWindowMutationInput & { id: string }) => Promise<MaintenanceWindow>;
  removeMaintenanceWindow: (id: string) => Promise<void>;
}

// `now` is injectable so callers can recompute against a ticking clock at render time
// rather than trusting the snapshot taken when the window was loaded.
export const isMaintenanceWindowActive = (s: MaintenanceWindow, now: number = Date.now()): boolean => {
  const start = new Date(s.starts_at).getTime();
  const end = s.ends_at ? new Date(s.ends_at).getTime() : Infinity;
  return now >= start && now < end;
};

const withActive = (s: MaintenanceWindow, now?: number): MaintenanceWindowWithActive => ({
  ...s,
  active: isMaintenanceWindowActive(s, now),
});

const upsertById = <T extends { id: string }>(list: T[], next: T): T[] => {
  const idx = list.findIndex((item) => item.id === next.id);
  if (idx < 0) return [next, ...list];
  const copy = list.slice();
  copy[idx] = next;
  return copy;
};

export const useNotificationsStore = create<NotificationsState>()(
  immer((set) => ({
    rules: [],
    maintenanceWindows: [],
    loading: false,
    loaded: false,

    refresh: async () => {
      set((state) => {
        state.loading = true;
      });
      try {
        const [rules, maintenanceWindows] = await Promise.all([api.listRules(), api.listMaintenanceWindows()]);
        set((state) => {
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
        // A history-affecting edit (e.g. changing scope) makes Alertmanager expire the
        // original silence and assign a new id; drop the stale row so the edited window
        // isn't listed twice (with the old one still showing active).
        if (updated.id !== input.id) {
          state.maintenanceWindows = state.maintenanceWindows.filter((s) => s.id !== input.id);
        }
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

export type { MaintenanceWindowScope };

export const selectRules = (s: NotificationsState) => s.rules;
export const selectMaintenanceWindows = (s: NotificationsState) => s.maintenanceWindows;
