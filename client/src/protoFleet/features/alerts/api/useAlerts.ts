import { useCallback, useMemo, useState } from "react";

import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import type {
  MaintenanceWindow,
  MaintenanceWindowWithActive,
  Rule,
  RuleConfig,
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
  createRule: (config: RuleConfig) => Promise<Rule>;
  updateRule: (id: string, config: RuleConfig) => Promise<Rule>;
  removeRule: (id: string) => Promise<void>;
  createMaintenanceWindow: (input: api.MaintenanceWindowMutationInput) => Promise<MaintenanceWindow>;
  updateMaintenanceWindow: (input: api.MaintenanceWindowMutationInput & { id: string }) => Promise<MaintenanceWindow>;
  removeMaintenanceWindow: (id: string) => Promise<void>;
}

// Feature-scoped data hook: holds rules/maintenance windows in local state rather than a shared store, which is reserved for UI persistence.
export function useAlerts(): UseAlertsResult {
  const [rules, setRules] = useState<Rule[]>([]);
  const [maintenanceWindows, setMaintenanceWindows] = useState<MaintenanceWindowWithActive[]>([]);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [nextRules, nextWindows] = await Promise.all([api.listRules(), api.listMaintenanceWindows()]);
      setRules(nextRules);
      setMaintenanceWindows(nextWindows.map((w) => withActive(w)));
    } finally {
      setLoading(false);
    }
  }, []);

  const pauseRule = useCallback(async (id: string) => {
    const updated = await api.pauseRule(id);
    setRules((current) => upsertById(current, updated));
  }, []);

  const resumeRule = useCallback(async (id: string) => {
    const updated = await api.resumeRule(id);
    setRules((current) => upsertById(current, updated));
  }, []);

  const createRule = useCallback(async (config: RuleConfig) => {
    const created = await api.createRule(config);
    setRules((current) => upsertById(current, created));
    return created;
  }, []);

  const updateRule = useCallback(async (id: string, config: RuleConfig) => {
    const updated = await api.updateRule(id, config);
    setRules((current) => upsertById(current, updated));
    return updated;
  }, []);

  const removeRule = useCallback(async (id: string) => {
    await api.deleteRule(id);
    setRules((current) => current.filter((r) => r.id !== id));
  }, []);

  const createMaintenanceWindow = useCallback(async (input: api.MaintenanceWindowMutationInput) => {
    const created = await api.createMaintenanceWindow(input);
    setMaintenanceWindows((current) => upsertById(current, withActive(created)));
    return created;
  }, []);

  const updateMaintenanceWindow = useCallback(async (input: api.MaintenanceWindowMutationInput & { id: string }) => {
    const updated = await api.updateMaintenanceWindow(input);
    // A history-affecting edit (e.g. scope change) makes Alertmanager assign a new silence id; drop the stale row so the window isn't listed twice.
    setMaintenanceWindows((current) => {
      const base = updated.id !== input.id ? current.filter((s) => s.id !== input.id) : current;
      return upsertById(base, withActive(updated));
    });
    return updated;
  }, []);

  const removeMaintenanceWindow = useCallback(async (id: string) => {
    await api.deleteMaintenanceWindow(id);
    setMaintenanceWindows((current) => current.filter((s) => s.id !== id));
  }, []);

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
      createMaintenanceWindow,
      updateMaintenanceWindow,
      removeMaintenanceWindow,
    ],
  );
}
