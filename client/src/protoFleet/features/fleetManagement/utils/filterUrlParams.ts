import { create } from "@bufbuild/protobuf";
import { componentIssues, deviceStatusFilterStates } from "../components/MinerList/constants";
import {
  fleetListTelemetryFieldForTelemetryKey,
  protoFieldForTelemetryKey,
  type TelemetryFilterKey,
} from "./telemetryFilterBounds";
import {
  type FleetListTelemetryRangeFilter,
  FleetListTelemetryRangeFilterSchema,
} from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  DeviceStatus,
  IpRangeSchema,
  type MinerListFilter,
  MinerListFilterSchema,
  NumericField,
  NumericRangeFilterSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { ActiveFilters, NumericRangeValue } from "@/shared/components/List/Filters/types";
import { classifySubnetLine, normalizeSubnetLine, validateSubnetLine } from "@/shared/utils/filterValidation";

const URL_PARAMS = {
  STATUS: "status",
  ISSUES: "issues",
  MODEL: "model",
  GROUP: "group",
  RACK: "rack",
  BUILDING: "building",
  SITE: "site",
  FIRMWARE: "firmware",
  ZONE: "zone",
  SUBNET: "subnet",
} as const;

export const UNASSIGNED_URL_VALUE = "null";
export const UNASSIGNED_FILTER_OPTION = { id: UNASSIGNED_URL_VALUE, label: "Unassigned" };

// Telemetry numeric filters use `${key}_min` / `${key}_max` URL params, one
// pair per field. Missing key = unbounded on that side.
const NUMERIC_KEYS: TelemetryFilterKey[] = ["hashrate", "efficiency", "power", "temperature"];
const numericMinParam = (key: TelemetryFilterKey) => `${key}_min`;
const numericMaxParam = (key: TelemetryFilterKey) => `${key}_max`;

export const setTelemetryNumericFilterURLParams = (
  params: URLSearchParams,
  key: TelemetryFilterKey,
  value: NumericRangeValue,
): void => {
  params.delete(numericMinParam(key));
  params.delete(numericMaxParam(key));
  if (value.min !== undefined) params.set(numericMinParam(key), String(value.min));
  if (value.max !== undefined) params.set(numericMaxParam(key), String(value.max));
};

export const FILTER_URL_PARAM_KEYS: readonly string[] = [
  ...Object.values(URL_PARAMS),
  ...NUMERIC_KEYS.flatMap((key) => [numericMinParam(key), numericMaxParam(key)]),
];

const numericKeyFromProtoField = (field: NumericField): TelemetryFilterKey | undefined => {
  switch (field) {
    case NumericField.HASHRATE_THS:
      return "hashrate";
    case NumericField.EFFICIENCY_JTH:
      return "efficiency";
    case NumericField.POWER_KW:
      return "power";
    case NumericField.TEMPERATURE_C:
      return "temperature";
    default:
      return undefined;
  }
};

const parseFiniteNumber = (raw: string): number | undefined => {
  if (raw.trim() === "") return undefined;
  const n = Number(raw);
  return Number.isFinite(n) ? n : undefined;
};

const STATUS_TO_URL: Record<string, string> = {
  [deviceStatusFilterStates.hashing]: "hashing",
  [deviceStatusFilterStates.offline]: "offline",
  [deviceStatusFilterStates.sleeping]: "sleeping",
  [deviceStatusFilterStates.needsAttention]: "needs-attention",
};

const URL_TO_STATUS: Record<string, string> = {
  hashing: deviceStatusFilterStates.hashing,
  offline: deviceStatusFilterStates.offline,
  sleeping: deviceStatusFilterStates.sleeping,
  "needs-attention": deviceStatusFilterStates.needsAttention,
};

// Encode each value as a separate URLSearchParams entry so individual values can contain
// commas, spaces, or other special chars without ambiguity. URLSearchParams handles the
// percent-encoding on `.toString()` and decodes it on construction.
const setMulti = (params: URLSearchParams, key: string, values: string[]): void => {
  values.forEach((value) => {
    if (value !== "") params.append(key, value);
  });
};

// Read values from URL using only the repeated-key format (`?k=a&k=b`).
// Used for keys whose values may contain commas (firmware/zone) — splitting would corrupt them.
const getMulti = (params: URLSearchParams, key: string): string[] => params.getAll(key).filter((value) => value !== "");

// Read values from URL accepting both repeated-key (`?k=a&k=b`) and the legacy
// comma-joined (`?k=a,b`) format that older bookmarks may carry. Only safe for keys whose
// values are guaranteed to not contain commas (enum strings, numeric IDs).
const getMultiLegacy = (params: URLSearchParams, key: string): string[] =>
  params.getAll(key).flatMap((raw) => raw.split(",").filter((piece) => piece !== ""));

const parseIdBucketValues = (params: URLSearchParams, key: string): { ids: bigint[]; includeUnassigned: boolean } => {
  const ids: bigint[] = [];
  let includeUnassigned = false;
  getMultiLegacy(params, key).forEach((raw) => {
    const trimmed = raw.trim();
    if (trimmed === UNASSIGNED_URL_VALUE) {
      includeUnassigned = true;
      return;
    }
    if (trimmed && /^\d+$/.test(trimmed)) {
      ids.push(BigInt(trimmed));
    }
  });
  return { ids, includeUnassigned };
};

export const parseIdFilterValuesFromURL = (
  params: URLSearchParams,
  key: "site" | "building" | "rack" | "group",
): { values: string[]; includeUnassigned: boolean } => {
  const values: string[] = [];
  let includeUnassigned = false;
  getMultiLegacy(params, key).forEach((raw) => {
    const trimmed = raw.trim();
    if (trimmed === UNASSIGNED_URL_VALUE) {
      includeUnassigned = true;
      values.push(UNASSIGNED_URL_VALUE);
      return;
    }
    if (trimmed && /^\d+$/.test(trimmed)) values.push(trimmed);
  });
  return { values: Array.from(new Set(values)), includeUnassigned };
};

export const issueComponentTypesFromValues = (issueValues: string[]): ComponentType[] => {
  const out: ComponentType[] = [];
  issueValues.forEach((issue) => {
    switch (issue) {
      case componentIssues.controlBoard:
        out.push(ComponentType.CONTROL_BOARD);
        break;
      case componentIssues.fans:
        out.push(ComponentType.FAN);
        break;
      case componentIssues.hashBoards:
        out.push(ComponentType.HASH_BOARD);
        break;
      case componentIssues.psu:
        out.push(ComponentType.PSU);
        break;
    }
  });
  return out;
};

export const issueComponentTypesFromURL = (params: URLSearchParams): ComponentType[] =>
  issueComponentTypesFromValues(getMultiLegacy(params, URL_PARAMS.ISSUES));

export const fleetListTelemetryRangesFromActiveFilters = (filters: ActiveFilters): FleetListTelemetryRangeFilter[] => {
  const ranges: FleetListTelemetryRangeFilter[] = [];
  Object.entries(filters.numericFilters).forEach(([key, value]) => {
    if (value.min === undefined && value.max === undefined) return;
    const field = fleetListTelemetryFieldForTelemetryKey[key as TelemetryFilterKey];
    if (field === undefined) return;
    const range = create(FleetListTelemetryRangeFilterSchema, {
      field,
      minInclusive: true,
      maxInclusive: true,
    });
    if (value.min !== undefined) range.min = value.min;
    if (value.max !== undefined) range.max = value.max;
    ranges.push(range);
  });
  return ranges;
};

export const fleetListTelemetryRangesFromURL = (params: URLSearchParams): FleetListTelemetryRangeFilter[] =>
  fleetListTelemetryRangesFromActiveFilters(parseUrlToActiveFilters(params));

/**
 * Encodes a MinerListFilter to URL search parameters
 */
export function encodeFilterToURL(filter: MinerListFilter): URLSearchParams {
  const params = new URLSearchParams();

  if (filter.deviceStatus.length > 0) {
    const statusValues = new Set<string>();
    filter.deviceStatus.forEach((status) => {
      switch (status) {
        case DeviceStatus.ONLINE:
          statusValues.add("hashing");
          break;
        case DeviceStatus.ERROR:
        case DeviceStatus.NEEDS_MINING_POOL:
        case DeviceStatus.UPDATING:
        case DeviceStatus.REBOOT_REQUIRED:
          statusValues.add("needs-attention");
          break;
        case DeviceStatus.OFFLINE:
          statusValues.add("offline");
          break;
        case DeviceStatus.INACTIVE:
        case DeviceStatus.MAINTENANCE:
          statusValues.add("sleeping");
          break;
      }
    });
    setMulti(params, URL_PARAMS.STATUS, Array.from(statusValues).sort());
  }

  if (filter.errorComponentTypes.length > 0) {
    const issueValues = new Set<string>();
    filter.errorComponentTypes.forEach((componentType) => {
      switch (componentType) {
        case ComponentType.CONTROL_BOARD:
          issueValues.add(componentIssues.controlBoard);
          break;
        case ComponentType.FAN:
          issueValues.add(componentIssues.fans);
          break;
        case ComponentType.HASH_BOARD:
          issueValues.add(componentIssues.hashBoards);
          break;
        case ComponentType.PSU:
          issueValues.add(componentIssues.psu);
          break;
      }
    });
    setMulti(params, URL_PARAMS.ISSUES, Array.from(issueValues).sort());
  }

  if (filter.models.length > 0) {
    setMulti(params, URL_PARAMS.MODEL, [...filter.models].sort());
  }

  if (filter.groupIds.length > 0) {
    setMulti(params, URL_PARAMS.GROUP, filter.groupIds.map(String).sort());
  }

  if (filter.rackIds.length > 0 || filter.includeNoRack) {
    const rackValues = filter.rackIds.map(String);
    if (filter.includeNoRack) rackValues.push(UNASSIGNED_URL_VALUE);
    setMulti(params, URL_PARAMS.RACK, rackValues.sort());
  }

  if (filter.buildingIds.length > 0 || filter.includeNoBuilding) {
    const buildingValues = filter.buildingIds.map(String);
    if (filter.includeNoBuilding) buildingValues.push(UNASSIGNED_URL_VALUE);
    setMulti(params, URL_PARAMS.BUILDING, buildingValues.sort());
  }

  if (filter.siteIds.length > 0 || filter.includeUnassigned) {
    const siteValues = filter.siteIds.map(String);
    if (filter.includeUnassigned) siteValues.push(UNASSIGNED_URL_VALUE);
    setMulti(params, URL_PARAMS.SITE, siteValues.sort());
  }

  if (filter.firmwareVersions.length > 0) {
    setMulti(params, URL_PARAMS.FIRMWARE, [...filter.firmwareVersions].sort());
  }

  if (filter.zones.length > 0) {
    setMulti(params, URL_PARAMS.ZONE, [...filter.zones].sort());
  }

  filter.numericRanges.forEach((range) => {
    const key = numericKeyFromProtoField(range.field);
    if (!key) return;
    if (range.min !== undefined) params.append(numericMinParam(key), String(range.min));
    if (range.max !== undefined) params.append(numericMaxParam(key), String(range.max));
  });

  // Ranges round-trip natively as "start-end" strings alongside CIDRs, so a
  // shared/bookmarked range reloads as its range chip rather than a CIDR set.
  const subnetValues = [...filter.ipCidrs, ...filter.ipRanges.map((r) => `${r.startIp}-${r.endIp}`)];
  if (subnetValues.length > 0) {
    setMulti(params, URL_PARAMS.SUBNET, subnetValues.sort());
  }

  return params;
}

/**
 * Parses URL search parameters into a MinerListFilter
 */
export function parseFilterFromURL(params: URLSearchParams): MinerListFilter | undefined {
  const hasAnyFilter = FILTER_URL_PARAM_KEYS.some((key) => params.has(key));

  if (!hasAnyFilter) {
    return undefined;
  }

  const filter = create(MinerListFilterSchema, {
    errorComponentTypes: [],
  });

  getMultiLegacy(params, URL_PARAMS.STATUS).forEach((value) => {
    switch (value) {
      case "hashing":
        filter.deviceStatus.push(DeviceStatus.ONLINE);
        break;
      case "needs-attention":
        filter.deviceStatus.push(DeviceStatus.ERROR);
        filter.deviceStatus.push(DeviceStatus.NEEDS_MINING_POOL);
        filter.deviceStatus.push(DeviceStatus.UPDATING);
        filter.deviceStatus.push(DeviceStatus.REBOOT_REQUIRED);
        break;
      case "offline":
        filter.deviceStatus.push(DeviceStatus.OFFLINE);
        break;
      case "sleeping":
        filter.deviceStatus.push(DeviceStatus.INACTIVE);
        filter.deviceStatus.push(DeviceStatus.MAINTENANCE);
        break;
    }
  });

  filter.errorComponentTypes.push(...issueComponentTypesFromURL(params));

  getMultiLegacy(params, URL_PARAMS.MODEL).forEach((model) => {
    if (model) filter.models.push(model);
  });

  getMultiLegacy(params, URL_PARAMS.GROUP).forEach((id) => {
    const trimmed = id.trim();
    if (trimmed && /^\d+$/.test(trimmed)) {
      filter.groupIds.push(BigInt(trimmed));
    }
  });

  const rackBucket = parseIdBucketValues(params, URL_PARAMS.RACK);
  filter.rackIds.push(...rackBucket.ids);
  filter.includeNoRack = rackBucket.includeUnassigned;

  const buildingBucket = parseIdBucketValues(params, URL_PARAMS.BUILDING);
  filter.buildingIds.push(...buildingBucket.ids);
  filter.includeNoBuilding = buildingBucket.includeUnassigned;

  const siteBucket = parseIdBucketValues(params, URL_PARAMS.SITE);
  filter.siteIds.push(...siteBucket.ids);
  filter.includeUnassigned = siteBucket.includeUnassigned;

  getMulti(params, URL_PARAMS.FIRMWARE).forEach((value) => {
    if (value) filter.firmwareVersions.push(value);
  });

  getMulti(params, URL_PARAMS.ZONE).forEach((value) => {
    if (value) filter.zones.push(value);
  });

  NUMERIC_KEYS.forEach((key) => {
    const minRaw = params.get(numericMinParam(key));
    const maxRaw = params.get(numericMaxParam(key));
    const min = minRaw !== null ? parseFiniteNumber(minRaw) : undefined;
    const max = maxRaw !== null ? parseFiniteNumber(maxRaw) : undefined;
    if (min === undefined && max === undefined) return;
    const range = create(NumericRangeFilterSchema, {
      field: protoFieldForTelemetryKey[key],
      minInclusive: true,
      maxInclusive: true,
    });
    if (min !== undefined) range.min = min;
    if (max !== undefined) range.max = max;
    filter.numericRanges.push(range);
  });

  getMulti(params, URL_PARAMS.SUBNET).forEach((value) => {
    const entry = classifySubnetLine(value);
    if (!entry) return;
    if (entry.kind === "range") {
      filter.ipRanges.push(create(IpRangeSchema, { startIp: entry.startIp, endIp: entry.endIp }));
    } else {
      filter.ipCidrs.push(entry.cidr);
    }
  });

  return filter;
}

/**
 * Converts URL search parameters to ActiveFilters format used by the UI
 */
export function parseUrlToActiveFilters(params: URLSearchParams): ActiveFilters {
  const activeFilters: ActiveFilters = {
    buttonFilters: [],
    dropdownFilters: {},
    numericFilters: {},
    textareaListFilters: {},
  };

  NUMERIC_KEYS.forEach((key) => {
    const minRaw = params.get(numericMinParam(key));
    const maxRaw = params.get(numericMaxParam(key));
    const min = minRaw !== null ? parseFiniteNumber(minRaw) : undefined;
    const max = maxRaw !== null ? parseFiniteNumber(maxRaw) : undefined;
    if (min === undefined && max === undefined) return;
    const value: NumericRangeValue = {};
    if (min !== undefined) value.min = min;
    if (max !== undefined) value.max = max;
    activeFilters.numericFilters[key] = value;
  });

  const subnetValues = getMulti(params, URL_PARAMS.SUBNET)
    .filter((value) => validateSubnetLine(value) === null)
    .map(normalizeSubnetLine);
  if (subnetValues.length > 0) {
    activeFilters.textareaListFilters.subnet = Array.from(new Set(subnetValues));
  }

  const statusValues = getMultiLegacy(params, URL_PARAMS.STATUS)
    .map((v) => URL_TO_STATUS[v])
    .filter(Boolean);
  const uniqueStatuses = Array.from(new Set(statusValues));
  if (uniqueStatuses.length > 0) {
    activeFilters.dropdownFilters.status = uniqueStatuses;
  }

  const issuesValues = getMultiLegacy(params, URL_PARAMS.ISSUES);
  if (issuesValues.length > 0) {
    activeFilters.dropdownFilters.issues = Array.from(new Set(issuesValues));
  }

  const modelValues = getMultiLegacy(params, URL_PARAMS.MODEL);
  if (modelValues.length > 0) {
    activeFilters.dropdownFilters.model = Array.from(new Set(modelValues));
  }

  const groupValues = getMultiLegacy(params, URL_PARAMS.GROUP)
    .map((value) => value.trim())
    .filter((value) => value !== "" && /^\d+$/.test(value));
  if (groupValues.length > 0) {
    activeFilters.dropdownFilters.group = Array.from(new Set(groupValues));
  }

  const rackValues = parseIdFilterValuesFromURL(params, URL_PARAMS.RACK).values;
  if (rackValues.length > 0) {
    activeFilters.dropdownFilters.rack = rackValues;
  }

  const buildingValues = parseIdFilterValuesFromURL(params, URL_PARAMS.BUILDING).values;
  if (buildingValues.length > 0) {
    activeFilters.dropdownFilters.building = buildingValues;
  }

  const siteValues = parseIdFilterValuesFromURL(params, URL_PARAMS.SITE).values;
  if (siteValues.length > 0) {
    activeFilters.dropdownFilters.site = siteValues;
  }

  const firmwareValues = getMulti(params, URL_PARAMS.FIRMWARE).filter((v) => v !== "");
  if (firmwareValues.length > 0) {
    activeFilters.dropdownFilters.firmware = Array.from(new Set(firmwareValues));
  }

  const zoneValues = getMulti(params, URL_PARAMS.ZONE).filter((v) => v !== "");
  if (zoneValues.length > 0) {
    activeFilters.dropdownFilters.zone = Array.from(new Set(zoneValues));
  }

  return activeFilters;
}

/**
 * Converts ActiveFilters to URL search parameters
 */
export function encodeActiveFiltersToURL(filters: ActiveFilters): URLSearchParams {
  const params = new URLSearchParams();

  const statusFilters = filters.dropdownFilters.status;
  if (statusFilters && statusFilters.length > 0) {
    const urlValues = statusFilters.map((s) => STATUS_TO_URL[s]).filter(Boolean);
    setMulti(params, URL_PARAMS.STATUS, [...urlValues].sort());
  }

  const issueFilters = filters.dropdownFilters.issues;
  if (issueFilters && issueFilters.length > 0) {
    setMulti(params, URL_PARAMS.ISSUES, [...issueFilters].sort());
  }

  const modelFilters = filters.dropdownFilters.model;
  if (modelFilters && modelFilters.length > 0) {
    setMulti(params, URL_PARAMS.MODEL, [...modelFilters].sort());
  }

  const groupFilters = filters.dropdownFilters.group;
  if (groupFilters && groupFilters.length > 0) {
    setMulti(params, URL_PARAMS.GROUP, [...groupFilters].sort());
  }

  const rackFilters = filters.dropdownFilters.rack;
  if (rackFilters && rackFilters.length > 0) {
    setMulti(params, URL_PARAMS.RACK, [...rackFilters].sort());
  }

  const buildingFilters = filters.dropdownFilters.building;
  if (buildingFilters && buildingFilters.length > 0) {
    setMulti(params, URL_PARAMS.BUILDING, [...buildingFilters].sort());
  }

  const siteFilters = filters.dropdownFilters.site;
  if (siteFilters && siteFilters.length > 0) {
    setMulti(params, URL_PARAMS.SITE, [...siteFilters].sort());
  }

  const firmwareFilters = filters.dropdownFilters.firmware;
  if (firmwareFilters && firmwareFilters.length > 0) {
    setMulti(params, URL_PARAMS.FIRMWARE, [...firmwareFilters].sort());
  }

  const zoneFilters = filters.dropdownFilters.zone;
  if (zoneFilters && zoneFilters.length > 0) {
    setMulti(params, URL_PARAMS.ZONE, [...zoneFilters].sort());
  }

  NUMERIC_KEYS.forEach((key) => {
    const value = filters.numericFilters[key];
    if (!value) return;
    if (value.min !== undefined) params.append(numericMinParam(key), String(value.min));
    if (value.max !== undefined) params.append(numericMaxParam(key), String(value.max));
  });

  const subnetCidrs = filters.textareaListFilters.subnet;
  if (subnetCidrs && subnetCidrs.length > 0) {
    setMulti(params, URL_PARAMS.SUBNET, [...subnetCidrs].sort());
  }

  return params;
}
