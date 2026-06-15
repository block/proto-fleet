// Zustand cache + mutations for the notifications surface; mutations call the server then merge the canonical row.
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import * as api from "@/protoFleet/features/notifications/api/notificationsApi";
import type {
  Channel,
  NotificationHistoryEntry,
  Rule,
  Silence,
  SilenceScope,
  SilenceWithActive,
} from "@/protoFleet/features/notifications/types";

interface NotificationsState {
  channels: Channel[];
  rules: Rule[];
  // Silences carry a derived `active` flag computed at fetch/mutation time, since computing it in render would call Date.now() (lint-blocked).
  silences: SilenceWithActive[];

  // History is paginated independently: refreshHistory() resets, loadMoreHistory() appends keyed off the last row's id.
  history: NotificationHistoryEntry[];
  historyHasMore: boolean;
  historyLoading: boolean;

  loading: boolean;
  loaded: boolean;

  refresh: () => Promise<void>;
  refreshHistory: () => Promise<void>;
  loadMoreHistory: () => Promise<void>;

  createChannel: (input: api.ChannelMutationInput) => Promise<Channel>;
  updateChannel: (input: api.ChannelMutationInput & { id: string }) => Promise<Channel>;
  removeChannel: (id: string) => Promise<void>;

  // Rules are pause/resume only; no create/edit/delete by design.
  pauseRule: (id: string) => Promise<void>;
  resumeRule: (id: string) => Promise<void>;

  createSilence: (input: api.SilenceMutationInput) => Promise<Silence>;
  updateSilence: (input: api.SilenceMutationInput & { id: string }) => Promise<Silence>;
  removeSilence: (id: string) => Promise<void>;
}

const withActive = (s: Silence): SilenceWithActive => {
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

const HISTORY_PAGE_SIZE = 50;

export const useNotificationsStore = create<NotificationsState>()(
  immer((set, get) => ({
    channels: [],
    rules: [],
    silences: [],
    history: [],
    historyHasMore: false,
    historyLoading: false,
    loading: false,
    loaded: false,

    refresh: async () => {
      set((state) => {
        state.loading = true;
      });
      try {
        const [channels, rules, silences] = await Promise.all([
          api.listChannels(),
          api.listRules(),
          api.listSilences(),
        ]);
        set((state) => {
          state.channels = channels;
          state.rules = rules;
          state.silences = silences.map(withActive);
          state.loaded = true;
        });
      } finally {
        set((state) => {
          state.loading = false;
        });
      }
    },

    refreshHistory: async () => {
      set((state) => {
        state.historyLoading = true;
      });
      try {
        const page = await api.listHistory({ page_size: HISTORY_PAGE_SIZE });
        set((state) => {
          state.history = page.notifications;
          state.historyHasMore = page.has_more;
        });
      } finally {
        set((state) => {
          state.historyLoading = false;
        });
      }
    },

    loadMoreHistory: async () => {
      const { history, historyHasMore, historyLoading } = get();
      if (!historyHasMore || historyLoading || history.length === 0) return;
      set((state) => {
        state.historyLoading = true;
      });
      try {
        const page = await api.listHistory({
          before_id: history[history.length - 1].id,
          page_size: HISTORY_PAGE_SIZE,
        });
        set((state) => {
          state.history = [...state.history, ...page.notifications];
          state.historyHasMore = page.has_more;
        });
      } finally {
        set((state) => {
          state.historyLoading = false;
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

    createSilence: async (input) => {
      const created = await api.createSilence(input);
      set((state) => {
        state.silences = upsertById(state.silences, withActive(created));
      });
      return created;
    },

    updateSilence: async (input) => {
      const updated = await api.updateSilence(input);
      set((state) => {
        state.silences = upsertById(state.silences, withActive(updated));
      });
      return updated;
    },

    removeSilence: async (id) => {
      await api.deleteSilence(id);
      set((state) => {
        state.silences = state.silences.filter((s) => s.id !== id);
      });
    },
  })),
);

// Mirrors the server's silenceActive(); normal flow reads silence.active off the cached row instead.
export const computeSilenceActive = withActive;

export type { SilenceScope };

// Selectors return raw references; map/filter MUST happen in a component useMemo to avoid array identity churn.
export const selectChannels = (s: NotificationsState) => s.channels;
export const selectRules = (s: NotificationsState) => s.rules;
export const selectSilences = (s: NotificationsState) => s.silences;
