import { useCallback, useEffect, useMemo, useState } from "react";

import { authzClient } from "@/protoFleet/api/clients";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

export interface CatalogEntry {
  /** Stable key, e.g. "miner:reboot". Matches /^[a-z]+:[a-z_]+$/. */
  key: string;
  description: string;
  /** Resource group ("fleet", "miner", ...) used for UI grouping. */
  resource: string;
}

export interface PermissionGroup {
  resource: string;
  /** Human-readable group heading shown in the role builder. */
  label: string;
  entries: CatalogEntry[];
}

// `fleet` is intentionally excluded from RESOURCE_TO_GROUP so `fleet:read`
// never renders as a manually toggleable checkbox in the role builder —
// it is the dependency floor for every miner action and is auto-included
// by `withRequiredReads` whenever a miner action is selected.
const RESOURCE_TO_GROUP: Record<string, string> = {
  miner: "miner",
  rack: "infrastructure",
  site: "infrastructure",
  curtailment: "curtailment",
  pool: "pool",
  schedule: "schedule",
  fleetnode: "admin",
  serverlog: "admin",
  activity: "admin",
  apikey: "admin",
  user: "admin",
  role: "admin",
};

const GROUP_LABELS: Record<string, string> = {
  miner: "Miners",
  infrastructure: "Sites, buildings & racks",
  curtailment: "Curtailment",
  pool: "Mining pools",
  schedule: "Schedules",
  admin: "Administration",
};

const GROUP_ORDER = ["miner", "infrastructure", "curtailment", "pool", "schedule", "admin"];

/** True for catalog keys whose action segment is "read". */
export const isReadKey = (key: string): boolean => key.endsWith(":read");

/** Groups a fetched flat catalog for the role builder UI. */
export const buildPermissionGroups = (catalog: CatalogEntry[]): PermissionGroup[] =>
  GROUP_ORDER.flatMap((group) => {
    const entries = catalog.filter((entry) => RESOURCE_TO_GROUP[entry.resource] === group);
    if (entries.length === 0) return [];
    return [{ resource: group, label: GROUP_LABELS[group] ?? group, entries }];
  });

const readKeyByResource = (catalog: CatalogEntry[]): Map<string, string> =>
  new Map(catalog.filter((entry) => isReadKey(entry.key)).map((entry) => [entry.resource, entry.key]));

/**
 * The read permissions a given action key depends on, mirroring the
 * server's role-save validator: every action permission requires its
 * same-resource read partner, and any miner action additionally requires
 * the fleet:read floor.
 */
export const requiredReadsFor = (key: string, catalog: CatalogEntry[]): string[] => {
  if (isReadKey(key)) return [];

  const entry = catalog.find((e) => e.key === key);
  if (!entry) return [];

  const reads = new Set<string>();
  const sameResourceRead = readKeyByResource(catalog).get(entry.resource);
  if (sameResourceRead) reads.add(sameResourceRead);
  // fleet:read is the floor for any role that grants a miner action.
  if (entry.resource === "miner") reads.add("fleet:read");

  return [...reads];
};

/**
 * Expands a selection to include every read permission its action keys
 * depend on. Apply this whenever the user toggles an action on so the
 * UI never holds a selection the server would reject on save.
 */
export const withRequiredReads = (selected: Iterable<string>, catalog: CatalogEntry[]): string[] => {
  const result = new Set(selected);
  for (const key of [...result]) {
    for (const read of requiredReadsFor(key, catalog)) result.add(read);
  }
  return [...result];
};

/**
 * Read keys that are still required by some other selected action, so the
 * UI can keep them locked rather than letting the user clear a dependency.
 */
export const lockedReadKeys = (selected: Iterable<string>, catalog: CatalogEntry[]): Set<string> => {
  const selectedSet = new Set(selected);
  const locked = new Set<string>();
  for (const key of selectedSet) {
    for (const read of requiredReadsFor(key, catalog)) {
      if (selectedSet.has(read)) locked.add(read);
    }
  }
  return locked;
};

export interface UsePermissionCatalogResult {
  catalog: CatalogEntry[];
  permissionGroups: PermissionGroup[];
  isLoading: boolean;
  error: string | null;
  requiredReadsFor: (key: string) => string[];
  withRequiredReads: (selected: Iterable<string>) => string[];
  lockedReadKeys: (selected: Iterable<string>) => Set<string>;
}

// Module-level cache so multiple hook instances share the single fetch.
// The catalog is a server-side code constant and does not change per
// session, so a one-shot fetch per page load is correct.
let cache: CatalogEntry[] | null = null;
let inflight: Promise<CatalogEntry[]> | null = null;

const fetchCatalog = async (): Promise<CatalogEntry[]> => {
  if (cache) return cache;
  if (inflight) return inflight;
  inflight = authzClient
    .listPermissions({})
    .then((response) => {
      cache = response.permissions.map((p) => ({ key: p.key, description: p.description, resource: p.resource }));
      return cache;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
};

export const usePermissionCatalog = (): UsePermissionCatalogResult => {
  const { handleAuthErrors } = useAuthErrors();
  const [catalog, setCatalog] = useState<CatalogEntry[]>(() => cache ?? []);
  const [isLoading, setIsLoading] = useState<boolean>(() => cache === null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (cache) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- syncs to module cache; runs only when cache populated before mount
      setCatalog(cache);
      // eslint-disable-next-line react-hooks/set-state-in-effect -- syncs to module cache; runs only when cache populated before mount
      setIsLoading(false);
      return;
    }
    let cancelled = false;
    fetchCatalog()
      .then((result) => {
        if (cancelled) return;
        setCatalog(result);
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        handleAuthErrors({
          error: err,
          onError: () => setError(getErrorMessage(err) || "Failed to load permissions"),
        });
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [handleAuthErrors]);

  const permissionGroups = useMemo(() => buildPermissionGroups(catalog), [catalog]);
  const boundRequiredReadsFor = useCallback((key: string) => requiredReadsFor(key, catalog), [catalog]);
  const boundWithRequiredReads = useCallback(
    (selected: Iterable<string>) => withRequiredReads(selected, catalog),
    [catalog],
  );
  const boundLockedReadKeys = useCallback((selected: Iterable<string>) => lockedReadKeys(selected, catalog), [catalog]);

  return {
    catalog,
    permissionGroups,
    isLoading,
    error,
    requiredReadsFor: boundRequiredReadsFor,
    withRequiredReads: boundWithRequiredReads,
    lockedReadKeys: boundLockedReadKeys,
  };
};
