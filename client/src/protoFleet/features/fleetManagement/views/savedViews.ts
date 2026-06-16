/**
 * Saved fleet views: tab + filters + sort bundled into named, persistable
 * presets. See docs/plans/2026-04-30-custom-views.md and issue #398.
 */

import { TELEMETRY_FILTER_BOUNDS } from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";

const STORAGE_KEY_PREFIX = "proto-fleet-miner-views";

export const VIEWS_SCHEMA_VERSION = 2;

/** URL search-param key that records the active view id. */
export const VIEW_URL_PARAM = "view";

/** Top-level Fleet tabs that can own a saved view. */
export const FLEET_TAB_IDS = ["sites", "buildings", "racks", "miners"] as const;
export type FleetTabId = (typeof FLEET_TAB_IDS)[number];

const isFleetTabId = (value: unknown): value is FleetTabId =>
  typeof value === "string" && (FLEET_TAB_IDS as readonly string[]).includes(value);

const MINER_FILTER_KEYS: readonly string[] = [
  "status",
  "issues",
  "model",
  "group",
  "rack",
  "building",
  "firmware",
  "zone",
  "subnet",
  ...Object.keys(TELEMETRY_FILTER_BOUNDS).flatMap((key) => [`${key}_min`, `${key}_max`]),
];

// Racks tab today URL-tracks `building` + `site` (site is a multi-site
// deep-link shortcut). `zone` + `issues` are still component-local state on
// the racks page, so they won't appear in a saved view's URL — listed here
// so views stay forward-compatible once those filters move into the URL.
//
// `display` captures the grid/list segmented control so a saved view can
// restore the operator's preferred density alongside its filter/sort set.
const RACK_FILTER_KEYS: readonly string[] = ["building", "site", "zone", "issues", "display"];

const SORT_KEYS: readonly string[] = ["sort", "dir"];

/**
 * Per-tab whitelist of URL keys that participate in filter+sort+view
 * canonicalization. Only keys in the active tab's set are persisted into a
 * saved view; everything else is treated as transient URL state and stripped
 * on canonicalize.
 */
const FILTER_AND_SORT_KEYS_BY_TAB: Record<FleetTabId, ReadonlySet<string>> = {
  miners: new Set([...MINER_FILTER_KEYS, ...SORT_KEYS]),
  racks: new Set([...RACK_FILTER_KEYS, ...SORT_KEYS]),
  buildings: new Set<string>(),
  sites: new Set<string>(),
};

/** Tabs that expose a save-worthy filter/sort surface. Used to gate "+ New view". */
export const TABS_WITH_SAVEABLE_STATE: ReadonlySet<FleetTabId> = new Set(
  (Object.entries(FILTER_AND_SORT_KEYS_BY_TAB) as [FleetTabId, ReadonlySet<string>][])
    .filter(([, keys]) => keys.size > 0)
    .map(([tab]) => tab),
);

/**
 * Build the URL params for activating a view. Layers the view's own
 * (canonical) params on top of any current params that are unrelated to
 * filter/sort/view, so unrelated URL keys aren't dropped on activation.
 */
export const buildUrlForView = (view: SavedView, currentParams: URLSearchParams): string => {
  const next = new URLSearchParams(view.searchParams);
  next.set(VIEW_URL_PARAM, view.id);
  const whitelist = FILTER_AND_SORT_KEYS_BY_TAB[view.tab];
  currentParams.forEach((value, key) => {
    if (key === VIEW_URL_PARAM) return;
    if (next.has(key)) return;
    if (whitelist.has(key)) return;
    next.append(key, value);
  });
  return next.toString();
};

export type SavedView = {
  id: string;
  name: string;
  /** Top-level Fleet tab the view restores on activation. */
  tab: FleetTabId;
  /** Canonical URLSearchParams string, sans the `view` key. */
  searchParams: string;
  createdAt: string;
};

export type SavedViewsRecord = {
  version: typeof VIEWS_SCHEMA_VERSION;
  views: SavedView[];
};

export const createDefaultSavedViewsRecord = (): SavedViewsRecord => ({
  version: VIEWS_SCHEMA_VERSION,
  views: [],
});

export const getSavedViewsStorageKey = (username: string): string => `${STORAGE_KEY_PREFIX}:${username || "anonymous"}`;

/**
 * Sort the filter+sort URL entries deterministically so two states can be
 * compared by string equality. Keys outside the tab's filter+sort set
 * (including the `view` key and any unrelated URL state) are dropped, so a
 * saved view never accidentally captures transient query params.
 */
export const canonicalizeSearchParams = (params: URLSearchParams | string, tab: FleetTabId): string => {
  const source = typeof params === "string" ? new URLSearchParams(params) : new URLSearchParams(params);
  const whitelist = FILTER_AND_SORT_KEYS_BY_TAB[tab];

  const entries: [string, string][] = [];
  source.forEach((value, key) => {
    if (whitelist.has(key)) {
      entries.push([key, value]);
    }
  });
  entries.sort(([aKey, aValue], [bKey, bValue]) => {
    if (aKey !== bKey) return aKey < bKey ? -1 : 1;
    if (aValue !== bValue) return aValue < bValue ? -1 : 1;
    return 0;
  });

  const out = new URLSearchParams();
  entries.forEach(([key, value]) => {
    out.append(key, value);
  });
  return out.toString();
};

const isNonEmptyString = (value: unknown): value is string => typeof value === "string" && value.length > 0;

/**
 * Tab is required in v2. v1 entries (no `tab` field) migrate to "miners"
 * since the miners surface was the only tab that owned views before #398.
 */
const normalizeSavedView = (raw: unknown): SavedView | null => {
  if (typeof raw !== "object" || raw === null) return null;
  const candidate = raw as Partial<SavedView> & { tab?: unknown };

  if (!isNonEmptyString(candidate.id)) return null;
  if (!isNonEmptyString(candidate.name)) return null;
  if (typeof candidate.searchParams !== "string") return null;

  const tab: FleetTabId = isFleetTabId(candidate.tab) ? candidate.tab : "miners";
  const createdAt = isNonEmptyString(candidate.createdAt) ? candidate.createdAt : new Date().toISOString();

  return {
    id: candidate.id,
    name: candidate.name,
    tab,
    searchParams: canonicalizeSearchParams(candidate.searchParams, tab),
    createdAt,
  };
};

export const normalizeSavedViewsRecord = (raw: unknown): SavedViewsRecord => {
  if (typeof raw !== "object" || raw === null) {
    return createDefaultSavedViewsRecord();
  }
  const candidate = raw as Partial<SavedViewsRecord>;

  const views: SavedView[] = [];
  const seenIds = new Set<string>();
  for (const entry of Array.isArray(candidate.views) ? candidate.views : []) {
    const normalized = normalizeSavedView(entry);
    if (!normalized || seenIds.has(normalized.id)) continue;
    seenIds.add(normalized.id);
    views.push(normalized);
  }

  return {
    version: VIEWS_SCHEMA_VERSION,
    views,
  };
};

export const isSavedViewsRecordDefault = (record: SavedViewsRecord): boolean => record.views.length === 0;

export const findView = (id: string, record: SavedViewsRecord): SavedView | undefined =>
  record.views.find((view) => view.id === id);

const generateUserViewId = (): string => {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `view-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
};

export const createUserView = (input: { name: string; tab: FleetTabId; searchParams: string }): SavedView => ({
  id: generateUserViewId(),
  name: input.name,
  tab: input.tab,
  searchParams: canonicalizeSearchParams(input.searchParams, input.tab),
  createdAt: new Date().toISOString(),
});
