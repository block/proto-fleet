import { useCallback, useMemo, useRef, useState } from "react";

import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type {
  MaintenanceWindow,
  MaintenanceWindowWithActive,
  Rule,
  RuleConfig,
  RuleRouting,
} from "@/protoFleet/features/alerts/types";

// `now` is injectable so callers can recompute against a ticking clock at render time instead of trusting the load-time snapshot.
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

export interface UseAlertsResult {
  rules: Rule[];
  maintenanceWindows: MaintenanceWindowWithActive[];
  loading: boolean;
  refresh: () => Promise<void>;
  pauseRule: (id: string) => Promise<void>;
  resumeRule: (id: string) => Promise<void>;
  createRule: (config: RuleConfig, routing?: RuleRouting) => Promise<Rule>;
  updateRule: (id: string, config: RuleConfig) => Promise<Rule>;
  removeRule: (id: string) => Promise<void>;
  setRuleRouting: (id: string, routing: RuleRouting) => Promise<Rule>;
  createMaintenanceWindow: (input: api.MaintenanceWindowMutationInput) => Promise<MaintenanceWindow>;
  updateMaintenanceWindow: (input: api.MaintenanceWindowMutationInput & { id: string }) => Promise<MaintenanceWindow>;
  removeMaintenanceWindow: (id: string) => Promise<void>;
}

// Feature-scoped data hook: holds rules/maintenance windows in local state rather than a shared store, which is reserved for UI persistence.
export function useAlerts(): UseAlertsResult {
  const [rules, setRules] = useState<Rule[]>([]);
  const [maintenanceWindows, setMaintenanceWindows] = useState<MaintenanceWindowWithActive[]>([]);
  const [loading, setLoading] = useState(false);

  // Ordering guards: deleted ids are tombstoned so a slow mutation response
  // can't re-add the row, and any mutation bumps the epoch so a refresh
  // snapshot that raced it is discarded instead of overwriting newer state.
  const deletedIdsRef = useRef<Set<string>>(new Set());
  const mutationEpochRef = useRef(0);

  const noteMutation = useCallback(() => {
    mutationEpochRef.current += 1;
  }, []);

  const isDeletedWindow = useCallback(
    (w: MaintenanceWindow): boolean =>
      deletedIdsRef.current.has(w.id) ||
      (w.scope.kind === "rule" && w.scope.rule_id != null && deletedIdsRef.current.has(w.scope.rule_id)),
    [],
  );

  const upsertRule = useCallback((updated: Rule) => {
    if (deletedIdsRef.current.has(updated.id)) return;
    setRules((current) => {
      // Null routing means the server couldn't read it; keep the last-known value so a route-read outage can't repaint a routed rule as default.
      const next = updated.routing
        ? updated
        : { ...updated, routing: current.find((r) => r.id === updated.id)?.routing ?? null };
      return upsertById(current, next);
    });
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const epoch = mutationEpochRef.current;
      const [nextRules, nextWindows] = await Promise.all([api.listRules(), api.listMaintenanceWindows()]);
      // A mutation landed mid-flight; its state is newer than this snapshot.
      if (epoch !== mutationEpochRef.current) return;
      setRules(nextRules.filter((r) => !deletedIdsRef.current.has(r.id)));
      setMaintenanceWindows(nextWindows.filter((w) => !isDeletedWindow(w)).map((w) => withActive(w)));
    } finally {
      setLoading(false);
    }
  }, [isDeletedWindow]);

  const pauseRule = useCallback(
    async (id: string) => {
      const updated = await api.pauseRule(id);
      noteMutation();
      upsertRule(updated);
    },
    [noteMutation, upsertRule],
  );

  const resumeRule = useCallback(
    async (id: string) => {
      const updated = await api.resumeRule(id);
      noteMutation();
      upsertRule(updated);
    },
    [noteMutation, upsertRule],
  );

  const createRule = useCallback(
    async (config: RuleConfig, routing?: RuleRouting) => {
      const created = await api.createRule(config, routing);
      noteMutation();
      upsertRule(created);
      return created;
    },
    [noteMutation, upsertRule],
  );

  const updateRule = useCallback(
    async (id: string, config: RuleConfig) => {
      const updated = await api.updateRule(id, config);
      noteMutation();
      upsertRule(updated);
      return updated;
    },
    [noteMutation, upsertRule],
  );

  const setRuleRouting = useCallback(
    async (id: string, routing: RuleRouting) => {
      const updated = await api.setRuleRouting(id, routing);
      noteMutation();
      upsertRule(updated);
      return updated;
    },
    [noteMutation, upsertRule],
  );

  const removeRule = useCallback(
    async (id: string) => {
      await api.deleteRule(id);
      noteMutation();
      deletedIdsRef.current.add(id);
      setRules((current) => current.filter((r) => r.id !== id));
      // The server delete also removes the rule's rule-scoped maintenance
      // windows; drop them locally so the list doesn't show stale entries.
      setMaintenanceWindows((current) => current.filter((w) => !(w.scope.kind === "rule" && w.scope.rule_id === id)));
    },
    [noteMutation],
  );

  const createMaintenanceWindow = useCallback(
    async (input: api.MaintenanceWindowMutationInput) => {
      const created = await api.createMaintenanceWindow(input);
      noteMutation();
      if (!isDeletedWindow(created)) {
        setMaintenanceWindows((current) => upsertById(current, withActive(created)));
      }
      return created;
    },
    [noteMutation, isDeletedWindow],
  );

  const updateMaintenanceWindow = useCallback(
    async (input: api.MaintenanceWindowMutationInput & { id: string }) => {
      const updated = await api.updateMaintenanceWindow(input);
      noteMutation();
      if (!isDeletedWindow(updated)) {
        // A history-affecting edit (e.g. scope change) makes Alertmanager assign a new silence id; drop the stale row so the window isn't listed twice.
        setMaintenanceWindows((current) => {
          const base = updated.id !== input.id ? current.filter((s) => s.id !== input.id) : current;
          return upsertById(base, withActive(updated));
        });
      }
      return updated;
    },
    [noteMutation, isDeletedWindow],
  );

  const removeMaintenanceWindow = useCallback(
    async (id: string) => {
      await api.deleteMaintenanceWindow(id);
      noteMutation();
      deletedIdsRef.current.add(id);
      setMaintenanceWindows((current) => current.filter((s) => s.id !== id));
    },
    [noteMutation],
  );

  return useMemo(
    () => ({
      rules,
      maintenanceWindows,
      loading,
      refresh,
      pauseRule,
      resumeRule,
      createRule,
      updateRule,
      removeRule,
      setRuleRouting,
      createMaintenanceWindow,
      updateMaintenanceWindow,
      removeMaintenanceWindow,
    }),
    [
      rules,
      maintenanceWindows,
      loading,
      refresh,
      pauseRule,
      resumeRule,
      createRule,
      updateRule,
      removeRule,
      setRuleRouting,
      createMaintenanceWindow,
      updateMaintenanceWindow,
      removeMaintenanceWindow,
    ],
  );
}
