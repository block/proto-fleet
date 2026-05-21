// Local cache + mutations for the notifications surface.
//
// The cache is zustand, mirroring the prototype's shape so the
// components didn't need a wholesale rewrite. The differences from
// the prototype:
//
//   - There's no rule append / remove path. Rules are read-only (the
//     ops Grafana YAML owns them); we only toggle `enabled` via
//     pauseRule / resumeRule.
//   - Mutations call the server first, then merge the canonical row
//     into the cache. Failures bubble out so the toast helper can
//     surface them.
//   - A `refresh()` method primes the cache from the server. The
//     page component calls it on mount.
import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import * as api from "@/protoFleet/features/notifications/api/notificationsApi";
import type {
  Channel,
  Rule,
  Silence,
  SilenceScope,
  SilenceWithActive,
} from "@/protoFleet/features/notifications/types";

interface NotificationsState {
  channels: Channel[];
  rules: Rule[];
  // Silences carry their derived `active` flag computed at fetch +
  // mutation time. Computing it in components would require calling
  // Date.now() during render, which the project's react-hooks/purity
  // and react-hooks/set-state-in-effect lints both block. Snapshotting
  // here is correct enough for this UI — the time only matters at the
  // moment a user takes an action, and every action triggers a
  // re-decoration via `withActive` below.
  silences: SilenceWithActive[];

  loading: boolean;
  loaded: boolean;

  refresh: () => Promise<void>;

  // Channels
  createChannel: (input: api.ChannelMutationInput) => Promise<Channel>;
  updateChannel: (input: api.ChannelMutationInput & { id: string }) => Promise<Channel>;
  removeChannel: (id: string) => Promise<void>;

  // Rules — only pause/resume; no create/edit/delete by design.
  pauseRule: (id: string) => Promise<void>;
  resumeRule: (id: string) => Promise<void>;

  // Silences
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

export const useNotificationsStore = create<NotificationsState>()(
  immer((set) => ({
    channels: [],
    rules: [],
    silences: [],
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

// Pure helper exported alongside the store. Mirrors the server's
// silenceActive() so the two stay consistent — but in normal flow
// callers just read silence.active off the cached row instead of
// recomputing.
export const computeSilenceActive = withActive;

// Re-export for code that wants to construct a SilenceScope inline.
export type { SilenceScope };

// Selectors — return raw store references. Anything that maps or
// filters MUST happen inside a component useMemo to avoid array
// identity churn.
export const selectChannels = (s: NotificationsState) => s.channels;
export const selectRules = (s: NotificationsState) => s.rules;
export const selectSilences = (s: NotificationsState) => s.silences;
