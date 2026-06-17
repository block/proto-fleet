import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  TELEMETRY_FILTER_BOUNDS,
  type TelemetryFilterKey,
} from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";
import type { FleetTabId } from "@/protoFleet/features/fleetManagement/views/savedViews";
import { formatNumericRangeCondition, formatTextareaListCondition } from "@/shared/utils/filterChipFormatting";

/** Lightweight {id, label} record — covers buildings/sites without dragging in their full proto types. */
export type FilterLabelSource = { id: string; label: string };

const STATUS_LABELS: Record<string, string> = {
  hashing: "Hashing",
  "needs-attention": "Needs attention",
  offline: "Offline",
  sleeping: "Sleeping",
};

const ISSUE_LABELS: Record<string, string> = {
  "control-board": "Control board",
  fans: "Fans",
  "hash-boards": "Hash boards",
  psu: "PSU",
};

const SORT_FIELD_LABELS: Record<string, string> = {
  name: "Name",
  "worker-name": "Worker name",
  ip: "IP address",
  mac: "MAC address",
  model: "Model",
  hashrate: "Hashrate",
  temp: "Temperature",
  power: "Power",
  efficiency: "Efficiency",
  firmware: "Firmware",
};

export type FilterSummaryEntry = {
  /** Stable category key, e.g. "status", for keys + tests. */
  key: string;
  /** Human-readable category label, e.g. "Status". */
  label: string;
  /** Display values, already humanized. */
  values: string[];
};

export type SortSummary = {
  fieldLabel: string;
  direction: "asc" | "desc";
};

export type DisplayMode = "grid" | "list";

export type DisplaySummary = {
  mode: DisplayMode;
  /** Humanized label for the segmented option, e.g. "Grid view". */
  label: string;
};

const DISPLAY_LABELS: Record<DisplayMode, string> = {
  grid: "Grid view",
  list: "List view",
};

export const URL_DISPLAY_PARAM = "display";

export const isDisplayMode = (value: unknown): value is DisplayMode => value === "grid" || value === "list";

export type FilterSummaryContext = {
  availableGroups: DeviceSet[];
  availableRacks: DeviceSet[];
  availableBuildings: FilterLabelSource[];
  availableSites: FilterLabelSource[];
};

const lookupDeviceSetLabels = (ids: string[], deviceSets: DeviceSet[]): string[] => {
  const labelById = new Map<string, string>();
  deviceSets.forEach((set) => {
    labelById.set(String(set.id), set.label);
  });
  return ids.map((id) => labelById.get(id) ?? `#${id}`);
};

const lookupNamedLabels = (ids: string[], items: FilterLabelSource[]): string[] => {
  const labelById = new Map<string, string>(items.map((item) => [item.id, item.label]));
  return ids.map((id) => labelById.get(id) ?? `#${id}`);
};

const dedupedSorted = (params: URLSearchParams, key: string): string[] =>
  Array.from(new Set(params.getAll(key)))
    .filter((value) => value !== "")
    .sort();

const summarizeMinerFilters = (params: URLSearchParams, context: FilterSummaryContext): FilterSummaryEntry[] => {
  const entries: FilterSummaryEntry[] = [];

  const statusValues = dedupedSorted(params, "status").map((value) => STATUS_LABELS[value] ?? value);
  if (statusValues.length) entries.push({ key: "status", label: "Status", values: statusValues });

  const issueValues = dedupedSorted(params, "issues").map((value) => ISSUE_LABELS[value] ?? value);
  if (issueValues.length) entries.push({ key: "issues", label: "Issues", values: issueValues });

  const modelValues = dedupedSorted(params, "model");
  if (modelValues.length) entries.push({ key: "model", label: "Model", values: modelValues });

  const groupValues = dedupedSorted(params, "group");
  if (groupValues.length) {
    entries.push({
      key: "group",
      label: "Groups",
      values: lookupDeviceSetLabels(groupValues, context.availableGroups),
    });
  }

  const rackValues = dedupedSorted(params, "rack");
  if (rackValues.length) {
    entries.push({ key: "rack", label: "Racks", values: lookupDeviceSetLabels(rackValues, context.availableRacks) });
  }

  const buildingValues = dedupedSorted(params, "building");
  if (buildingValues.length) {
    entries.push({
      key: "building",
      label: "Buildings",
      values: lookupNamedLabels(buildingValues, context.availableBuildings),
    });
  }

  const firmwareValues = dedupedSorted(params, "firmware");
  if (firmwareValues.length) entries.push({ key: "firmware", label: "Firmware", values: firmwareValues });

  const zoneValues = dedupedSorted(params, "zone");
  if (zoneValues.length) entries.push({ key: "zone", label: "Zone", values: zoneValues });

  // Numeric range filters: render as a single value, e.g. "50 TH/s - 200 TH/s"
  // or "≥ 50 TH/s". Mirrors the chip text so the summary reads the same way.
  (Object.keys(TELEMETRY_FILTER_BOUNDS) as TelemetryFilterKey[]).forEach((key) => {
    const bounds = TELEMETRY_FILTER_BOUNDS[key];
    const minRaw = params.get(`${key}_min`);
    const maxRaw = params.get(`${key}_max`);
    const min = minRaw !== null && minRaw !== "" ? Number(minRaw) : undefined;
    const max = maxRaw !== null && maxRaw !== "" ? Number(maxRaw) : undefined;
    if ((min === undefined || !Number.isFinite(min)) && (max === undefined || !Number.isFinite(max))) return;
    const summary = formatNumericRangeCondition(
      {
        min: Number.isFinite(min) ? min : undefined,
        max: Number.isFinite(max) ? max : undefined,
      },
      bounds.unit,
    );
    if (!summary) return;
    entries.push({ key, label: bounds.label, values: [summary] });
  });

  // Subnet (CIDR list) filter — single chip-style value, "N subnets" when more
  // than one entry, the literal CIDR when exactly one.
  const subnetValues = dedupedSorted(params, "subnet");
  if (subnetValues.length) {
    entries.push({
      key: "subnet",
      label: "Subnet",
      values: [formatTextareaListCondition(subnetValues, { noun: "subnet" })],
    });
  }

  return entries;
};

const summarizeRackFilters = (params: URLSearchParams, context: FilterSummaryContext): FilterSummaryEntry[] => {
  const entries: FilterSummaryEntry[] = [];

  const buildingValues = dedupedSorted(params, "building");
  if (buildingValues.length) {
    entries.push({
      key: "building",
      label: "Buildings",
      values: lookupNamedLabels(buildingValues, context.availableBuildings),
    });
  }

  const siteValues = dedupedSorted(params, "site");
  if (siteValues.length) {
    entries.push({ key: "site", label: "Sites", values: lookupNamedLabels(siteValues, context.availableSites) });
  }

  const zoneValues = dedupedSorted(params, "zone");
  if (zoneValues.length) entries.push({ key: "zone", label: "Zone", values: zoneValues });

  const issueValues = dedupedSorted(params, "issues").map((value) => ISSUE_LABELS[value] ?? value);
  if (issueValues.length) entries.push({ key: "issues", label: "Issues", values: issueValues });

  return entries;
};

export const summarizeFilters = (
  params: URLSearchParams,
  tab: FleetTabId,
  context: FilterSummaryContext,
): FilterSummaryEntry[] => {
  switch (tab) {
    case "miners":
      return summarizeMinerFilters(params, context);
    case "racks":
      return summarizeRackFilters(params, context);
    case "buildings":
    case "sites":
      // No filter surface in URL yet on these tabs.
      return [];
  }
};

/**
 * Tabs that own `sort`/`dir` URL params. Other tabs may carry those keys as
 * cross-navigation residue; surfacing them as a "sort summary" would offer
 * an "Include sort" toggle that silently drops on save because the tab's
 * canonicalization whitelist excludes the keys.
 */
const TABS_WITH_SORT: ReadonlySet<FleetTabId> = new Set(["miners", "racks"]);

export const summarizeSort = (params: URLSearchParams, tab: FleetTabId): SortSummary | undefined => {
  if (!TABS_WITH_SORT.has(tab)) return undefined;
  const sortField = params.get("sort");
  if (!sortField) return undefined;

  const fieldLabel = SORT_FIELD_LABELS[sortField.toLowerCase()] ?? sortField;
  const direction = params.get("dir") === "asc" ? "asc" : "desc";
  return { fieldLabel, direction };
};

/**
 * Strips sort/dir keys from a canonical search-params string. Used when the
 * "Include sort order" toggle is off.
 */
export const stripSortFromSearchParams = (searchParams: string): string => {
  const params = new URLSearchParams(searchParams);
  params.delete("sort");
  params.delete("dir");
  return params.toString();
};

/**
 * Tabs that own a `display` URL param. Miners has no grid/list toggle, so
 * surfacing display on that tab would let the modal offer an "Include
 * display mode" control whose value the canonicalization whitelist strips.
 */
const TABS_WITH_DISPLAY: ReadonlySet<FleetTabId> = new Set(["racks"]);

export const summarizeDisplay = (params: URLSearchParams, tab: FleetTabId): DisplaySummary | undefined => {
  if (!TABS_WITH_DISPLAY.has(tab)) return undefined;
  const raw = params.get(URL_DISPLAY_PARAM);
  if (!isDisplayMode(raw)) return undefined;
  return { mode: raw, label: DISPLAY_LABELS[raw] };
};

/**
 * Strips the `display` key from a canonical search-params string. Used when
 * the "Include display mode" toggle is off in the view-save modal.
 */
export const stripDisplayFromSearchParams = (searchParams: string): string => {
  const params = new URLSearchParams(searchParams);
  params.delete(URL_DISPLAY_PARAM);
  return params.toString();
};

export type DisplayChange = "unchanged" | "added" | "changed" | "removed";

export type DisplayDiff = {
  current: DisplaySummary | undefined;
  saved: DisplaySummary | undefined;
  change: DisplayChange;
};

export const diffDisplaySummaries = (
  current: DisplaySummary | undefined,
  saved: DisplaySummary | undefined,
): DisplayDiff => {
  if (!current && !saved) return { current, saved, change: "unchanged" };
  if (current && !saved) return { current, saved, change: "added" };
  if (!current && saved) return { current, saved, change: "removed" };
  if (current && saved && current.mode === saved.mode) return { current, saved, change: "unchanged" };
  return { current, saved, change: "changed" };
};

export type FilterChange = "unchanged" | "added" | "changed";

export type FilterDiffEntry = FilterSummaryEntry & {
  change: FilterChange;
  /** Previous values, only set when change === "changed". */
  previousValues?: string[];
};

export type FilterDiff = {
  /** Entries present in the current set, marked with their change status. */
  current: FilterDiffEntry[];
  /** Entries that were in the saved view but are absent from current. */
  removed: FilterSummaryEntry[];
};

/**
 * Compares two filter summaries (saved view vs current URL) and classifies
 * each entry as added/changed/unchanged, plus collects entries that were in
 * the saved view but no longer exist.
 */
export const diffFilterSummaries = (current: FilterSummaryEntry[], saved: FilterSummaryEntry[]): FilterDiff => {
  const savedByKey = new Map(saved.map((entry) => [entry.key, entry]));
  const seen = new Set<string>();

  const currentDiff: FilterDiffEntry[] = current.map((entry) => {
    seen.add(entry.key);
    const previous = savedByKey.get(entry.key);
    if (!previous) {
      return { ...entry, change: "added" };
    }
    if (
      previous.values.length === entry.values.length &&
      previous.values.every((value, i) => value === entry.values[i])
    ) {
      return { ...entry, change: "unchanged" };
    }
    return { ...entry, change: "changed", previousValues: previous.values };
  });

  const removed = saved.filter((entry) => !seen.has(entry.key));

  return { current: currentDiff, removed };
};

export type SortChange = "unchanged" | "added" | "changed" | "removed";

export type SortDiff = {
  current: SortSummary | undefined;
  saved: SortSummary | undefined;
  change: SortChange;
};

const sortEqual = (a: SortSummary, b: SortSummary): boolean =>
  a.fieldLabel === b.fieldLabel && a.direction === b.direction;

export const diffSortSummaries = (current: SortSummary | undefined, saved: SortSummary | undefined): SortDiff => {
  if (!current && !saved) return { current, saved, change: "unchanged" };
  if (current && !saved) return { current, saved, change: "added" };
  if (!current && saved) return { current, saved, change: "removed" };
  if (current && saved && sortEqual(current, saved)) return { current, saved, change: "unchanged" };
  return { current, saved, change: "changed" };
};
