/**
 * Saved miner-list views: filters + sort bundled into named, persistable
 * presets. See docs/plans/2026-04-30-custom-views.md.
 */

const STORAGE_KEY_PREFIX = "proto-fleet-miner-views";

export const VIEWS_SCHEMA_VERSION = 1;

/** URL search-param key that records the active view id. */
export const VIEW_URL_PARAM = "view";

/** Built-in view id for the unfiltered default — kept as a clean URL. */
export const ALL_MINERS_VIEW_ID = "all-miners";

/** URL keys owned by filter + sort + view machinery. */
const FILTER_AND_SORT_KEYS: ReadonlySet<string> = new Set([
  "status",
  "issues",
  "model",
  "group",
  "rack",
  "firmware",
  "zone",
  "sort",
  "dir",
]);

/**
 * Build the URL params for activating a view. Layers the view's own
 * (canonical) params on top of any current params that are unrelated to
 * filter/sort/view, so unrelated URL keys aren't dropped on activation.
 */
export const buildUrlForView = (view: SavedView, currentParams: URLSearchParams): string => {
  const next = new URLSearchParams(view.searchParams);
  next.set(VIEW_URL_PARAM, view.id);
  currentParams.forEach((value, key) => {
    if (key === VIEW_URL_PARAM) return;
    if (next.has(key)) return;
    if (FILTER_AND_SORT_KEYS.has(key)) return;
    next.append(key, value);
  });
  return next.toString();
};

export type SavedView = {
  id: string;
  name: string;
  /** Canonical URLSearchParams string, sans the `view` key. */
  searchParams: string;
  createdAt: string;
};

export type SavedViewsRecord = {
  version: typeof VIEWS_SCHEMA_VERSION;
  views: SavedView[];
  deletedBuiltInIds: string[];
};

export type BuiltInView = SavedView & { builtIn: true };

export const BUILT_IN_VIEWS: readonly BuiltInView[] = [
  {
    id: ALL_MINERS_VIEW_ID,
    name: "All miners",
    searchParams: "",
    createdAt: "1970-01-01T00:00:00.000Z",
    builtIn: true,
  },
  {
    id: "needs-attention",
    name: "Needs attention",
    searchParams: "status=needs-attention",
    createdAt: "1970-01-01T00:00:00.000Z",
    builtIn: true,
  },
  {
    id: "offline",
    name: "Offline",
    searchParams: "status=offline",
    createdAt: "1970-01-01T00:00:00.000Z",
    builtIn: true,
  },
];

const BUILT_IN_IDS: ReadonlySet<string> = new Set(BUILT_IN_VIEWS.map((view) => view.id));

export const isBuiltInViewId = (id: string): boolean => BUILT_IN_IDS.has(id);

export const createDefaultSavedViewsRecord = (): SavedViewsRecord => ({
  version: VIEWS_SCHEMA_VERSION,
  views: [],
  deletedBuiltInIds: [],
});

export const getSavedViewsStorageKey = (username: string): string => `${STORAGE_KEY_PREFIX}:${username || "anonymous"}`;

/**
 * Sort the filter+sort URL entries deterministically so two states can be
 * compared by string equality. Keys outside the filter+sort set (including
 * the `view` key and any unrelated URL state) are dropped, so a saved view
 * never accidentally captures transient query params.
 */
export const canonicalizeSearchParams = (params: URLSearchParams | string): string => {
  const source = typeof params === "string" ? new URLSearchParams(params) : new URLSearchParams(params);

  const entries: [string, string][] = [];
  source.forEach((value, key) => {
    if (FILTER_AND_SORT_KEYS.has(key)) {
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

const normalizeSavedView = (raw: unknown): SavedView | null => {
  if (typeof raw !== "object" || raw === null) return null;
  const candidate = raw as Partial<SavedView>;

  if (!isNonEmptyString(candidate.id)) return null;
  if (isBuiltInViewId(candidate.id)) return null;
  if (!isNonEmptyString(candidate.name)) return null;
  if (typeof candidate.searchParams !== "string") return null;

  const createdAt = isNonEmptyString(candidate.createdAt) ? candidate.createdAt : new Date().toISOString();

  return {
    id: candidate.id,
    name: candidate.name,
    searchParams: canonicalizeSearchParams(candidate.searchParams),
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

  const deletedBuiltInIds: string[] = [];
  const seenDeleted = new Set<string>();
  for (const id of Array.isArray(candidate.deletedBuiltInIds) ? candidate.deletedBuiltInIds : []) {
    if (typeof id !== "string" || seenDeleted.has(id)) continue;
    seenDeleted.add(id);
    deletedBuiltInIds.push(id);
  }

  return {
    version: VIEWS_SCHEMA_VERSION,
    views,
    deletedBuiltInIds,
  };
};

export const isSavedViewsRecordDefault = (record: SavedViewsRecord): boolean =>
  record.views.length === 0 && record.deletedBuiltInIds.length === 0;

/**
 * Built-in views the user hasn't dismissed, in declaration order.
 */
export const visibleBuiltInViews = (record: SavedViewsRecord): BuiltInView[] => {
  const dismissed = new Set(record.deletedBuiltInIds);
  return BUILT_IN_VIEWS.filter((view) => !dismissed.has(view.id));
};

export const findView = (id: string, record: SavedViewsRecord): SavedView | undefined => {
  if (isBuiltInViewId(id)) {
    if (record.deletedBuiltInIds.includes(id)) return undefined;
    return BUILT_IN_VIEWS.find((view) => view.id === id);
  }
  return record.views.find((view) => view.id === id);
};

const generateUserViewId = (): string => {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `view-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
};

export const createUserView = (input: { name: string; searchParams: string }): SavedView => ({
  id: generateUserViewId(),
  name: input.name,
  searchParams: canonicalizeSearchParams(input.searchParams),
  createdAt: new Date().toISOString(),
});
