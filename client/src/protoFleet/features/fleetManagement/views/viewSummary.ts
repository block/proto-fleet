import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  TELEMETRY_FILTER_BOUNDS,
  type TelemetryFilterKey,
} from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";
import { formatNumericRangeCondition, formatTextareaListCondition } from "@/shared/utils/filterChipFormatting";

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

const lookupDeviceSetLabels = (ids: string[], deviceSets: DeviceSet[]): string[] => {
  const labelById = new Map<string, string>();
  deviceSets.forEach((set) => {
    labelById.set(String(set.id), set.label);
  });
  return ids.map((id) => labelById.get(id) ?? `#${id}`);
};

const dedupedSorted = (params: URLSearchParams, key: string): string[] =>
  Array.from(new Set(params.getAll(key)))
    .filter((value) => value !== "")
    .sort();

export const summarizeFilters = (
  params: URLSearchParams,
  context: { availableGroups: DeviceSet[]; availableRacks: DeviceSet[] },
): FilterSummaryEntry[] => {
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
    entries.push({
      key: "rack",
      label: "Racks",
      values: lookupDeviceSetLabels(rackValues, context.availableRacks),
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

export const summarizeSort = (params: URLSearchParams): SortSummary | undefined => {
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
