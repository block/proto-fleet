import { create } from "@bufbuild/protobuf";
import { componentIssues, deviceStatusFilterStates } from "../components/MinerList/constants";
import { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  DeviceStatus,
  type MinerListFilter,
  MinerListFilterSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

/**
 * URL parameter keys for filter state
 */
const URL_PARAMS = {
  STATUS: "status",
  ISSUES: "issues",
  MODEL: "model",
  GROUP: "group",
  RACK: "rack",
} as const;

/**
 * Maps device status filter states to URL values
 */
const STATUS_TO_URL: Record<string, string> = {
  [deviceStatusFilterStates.hashing]: "hashing",
  [deviceStatusFilterStates.offline]: "offline",
  [deviceStatusFilterStates.sleeping]: "sleeping",
  [deviceStatusFilterStates.needsAttention]: "needs-attention",
};

/**
 * Maps URL values to device status filter states
 */
const URL_TO_STATUS: Record<string, string> = {
  hashing: deviceStatusFilterStates.hashing,
  offline: deviceStatusFilterStates.offline,
  sleeping: deviceStatusFilterStates.sleeping,
  "needs-attention": deviceStatusFilterStates.needsAttention,
};

/**
 * Encodes a MinerListFilter to URL search parameters
 */
export function encodeFilterToURL(filter: MinerListFilter): URLSearchParams {
  const params = new URLSearchParams();

  // Encode device statuses
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
          statusValues.add("sleeping");
          break;
      }
    });
    if (statusValues.size > 0) {
      params.set(URL_PARAMS.STATUS, Array.from(statusValues).sort().join(","));
    }
  }

  // Encode error component types (issues)
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
    if (issueValues.size > 0) {
      params.set(URL_PARAMS.ISSUES, Array.from(issueValues).sort().join(","));
    }
  }

  // Encode models
  if (filter.models.length > 0) {
    params.set(URL_PARAMS.MODEL, filter.models.sort().join(","));
  }

  // Encode group IDs
  if (filter.groupIds.length > 0) {
    params.set(URL_PARAMS.GROUP, filter.groupIds.map(String).sort().join(","));
  }

  // Encode rack IDs
  if (filter.rackIds.length > 0) {
    params.set(URL_PARAMS.RACK, filter.rackIds.map(String).sort().join(","));
  }

  return params;
}

/**
 * Parses URL search parameters into a MinerListFilter
 */
export function parseFilterFromURL(params: URLSearchParams): MinerListFilter | undefined {
  const statusParam = params.get(URL_PARAMS.STATUS);
  const issuesParam = params.get(URL_PARAMS.ISSUES);
  const modelParam = params.get(URL_PARAMS.MODEL);
  const groupParam = params.get(URL_PARAMS.GROUP);
  const rackParam = params.get(URL_PARAMS.RACK);

  // If no filter params, return undefined
  if (!statusParam && !issuesParam && !modelParam && !groupParam && !rackParam) {
    return undefined;
  }

  const filter = create(MinerListFilterSchema, {
    errorComponentTypes: [],
  });

  // Parse device statuses
  if (statusParam) {
    const statusValues = statusParam.split(",");
    statusValues.forEach((value) => {
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
          break;
      }
    });
  }

  // Parse component issues
  if (issuesParam) {
    const issueValues = issuesParam.split(",");
    issueValues.forEach((issue) => {
      switch (issue) {
        case componentIssues.controlBoard:
          filter.errorComponentTypes.push(ComponentType.CONTROL_BOARD);
          break;
        case componentIssues.fans:
          filter.errorComponentTypes.push(ComponentType.FAN);
          break;
        case componentIssues.hashBoards:
          filter.errorComponentTypes.push(ComponentType.HASH_BOARD);
          break;
        case componentIssues.psu:
          filter.errorComponentTypes.push(ComponentType.PSU);
          break;
        default:
          return; // Skip unknown issues
      }
    });
  }

  // Parse models
  if (modelParam) {
    const modelValues = modelParam.split(",");
    modelValues.forEach((model) => {
      if (model) {
        filter.models.push(model);
      }
    });
  }

  // Parse group IDs
  if (groupParam) {
    const groupValues = groupParam.split(",");
    groupValues.forEach((id) => {
      const trimmed = id.trim();
      if (trimmed && /^\d+$/.test(trimmed)) {
        filter.groupIds.push(BigInt(trimmed));
      }
    });
  }

  // Parse rack IDs
  if (rackParam) {
    const rackValues = rackParam.split(",");
    rackValues.forEach((id) => {
      const trimmed = id.trim();
      if (trimmed && /^\d+$/.test(trimmed)) {
        filter.rackIds.push(BigInt(trimmed));
      }
    });
  }

  return filter;
}

/**
 * Converts URL search parameters to ActiveFilters format used by the UI
 */
export function parseUrlToActiveFilters(params: URLSearchParams): ActiveFilters {
  const activeFilters: ActiveFilters = {
    buttonFilters: [],
    dropdownFilters: {},
  };

  // Parse status dropdown
  const statusParam = params.get(URL_PARAMS.STATUS);
  if (statusParam) {
    const statusValues = statusParam.split(",");
    const mappedStatuses = statusValues.map((v) => URL_TO_STATUS[v]).filter(Boolean);
    // Deduplicate to prevent infinite loops from duplicate URL params
    const uniqueStatuses = Array.from(new Set(mappedStatuses));
    if (uniqueStatuses.length > 0) {
      activeFilters.dropdownFilters.status = uniqueStatuses;
    }
  }

  // Parse issues dropdown
  const issuesParam = params.get(URL_PARAMS.ISSUES);
  if (issuesParam) {
    const issueValues = issuesParam.split(",");
    // Deduplicate to prevent infinite loops from duplicate URL params
    activeFilters.dropdownFilters.issues = Array.from(new Set(issueValues));
  }

  // Parse model dropdown
  const modelParam = params.get(URL_PARAMS.MODEL);
  if (modelParam) {
    const modelValues = modelParam.split(",");
    // Deduplicate to prevent infinite loops from duplicate URL params
    activeFilters.dropdownFilters.model = Array.from(new Set(modelValues));
  }

  // Parse group dropdown
  const groupParam = params.get(URL_PARAMS.GROUP);
  if (groupParam) {
    const groupValues = groupParam
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value !== "" && /^\d+$/.test(value));
    if (groupValues.length > 0) {
      activeFilters.dropdownFilters.group = Array.from(new Set(groupValues));
    }
  }

  // Parse rack dropdown
  const rackParam = params.get(URL_PARAMS.RACK);
  if (rackParam) {
    const rackValues = rackParam
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value !== "" && /^\d+$/.test(value));
    if (rackValues.length > 0) {
      activeFilters.dropdownFilters.rack = Array.from(new Set(rackValues));
    }
  }

  return activeFilters;
}

/**
 * Converts ActiveFilters to URL search parameters
 */
export function encodeActiveFiltersToURL(filters: ActiveFilters): URLSearchParams {
  const params = new URLSearchParams();

  // Encode status dropdown
  const statusFilters = filters.dropdownFilters.status;
  if (statusFilters && statusFilters.length > 0) {
    const urlValues = statusFilters.map((s) => STATUS_TO_URL[s]).filter(Boolean);
    if (urlValues.length > 0) {
      params.set(URL_PARAMS.STATUS, urlValues.sort().join(","));
    }
  }

  // Encode issues dropdown
  const issueFilters = filters.dropdownFilters.issues;
  if (issueFilters && issueFilters.length > 0) {
    params.set(URL_PARAMS.ISSUES, issueFilters.sort().join(","));
  }

  // Encode model dropdown
  const modelFilters = filters.dropdownFilters.model;
  if (modelFilters && modelFilters.length > 0) {
    params.set(URL_PARAMS.MODEL, modelFilters.sort().join(","));
  }

  // Encode group dropdown
  const groupFilters = filters.dropdownFilters.group;
  if (groupFilters && groupFilters.length > 0) {
    params.set(URL_PARAMS.GROUP, groupFilters.sort().join(","));
  }

  // Encode rack dropdown
  const rackFilters = filters.dropdownFilters.rack;
  if (rackFilters && rackFilters.length > 0) {
    params.set(URL_PARAMS.RACK, rackFilters.sort().join(","));
  }

  return params;
}
