import { create } from "@bufbuild/protobuf";
import { componentIssues, deviceStatusFilterStates, minerTypes } from "../components/MinerList/constants";
import {
  ComponentStatus,
  ComponentStatusFilterSchema,
  ComponentType,
  DeviceStatus,
  type MinerListFilter,
  MinerListFilterSchema,
  MinerType,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

/**
 * URL parameter keys for filter state
 */
const URL_PARAMS = {
  STATUS: "status",
  ISSUES: "issues",
  TYPE: "type",
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
    const statusValues: string[] = [];
    filter.deviceStatus.forEach((status) => {
      switch (status) {
        case DeviceStatus.ONLINE:
          statusValues.push("hashing");
          break;
        case DeviceStatus.ERROR:
          statusValues.push("needs-attention");
          break;
        case DeviceStatus.OFFLINE:
          statusValues.push("offline");
          break;
        case DeviceStatus.INACTIVE:
          statusValues.push("sleeping");
          break;
      }
    });
    if (statusValues.length > 0) {
      params.set(URL_PARAMS.STATUS, statusValues.join(","));
    }
  }

  // Encode component filters (issues)
  if (filter.componentFilters.length > 0) {
    const issueValues: string[] = [];
    filter.componentFilters.forEach((compFilter) => {
      switch (compFilter.component) {
        case ComponentType.CONTROL_BOARD:
          issueValues.push(componentIssues.controlBoard);
          break;
        case ComponentType.FANS:
          issueValues.push(componentIssues.fans);
          break;
        case ComponentType.HASH_BOARDS:
          issueValues.push(componentIssues.hashBoards);
          break;
        case ComponentType.PSU:
          issueValues.push(componentIssues.psu);
          break;
      }
    });
    if (issueValues.length > 0) {
      params.set(URL_PARAMS.ISSUES, issueValues.join(","));
    }
  }

  // Encode miner types
  if (filter.types.length > 0) {
    const typeValues: string[] = [];
    filter.types.forEach((type) => {
      switch (type) {
        case MinerType.PROTO_RIG:
          typeValues.push(minerTypes.protoRig);
          break;
        case MinerType.BITMAIN:
          typeValues.push(minerTypes.bitmain);
          break;
      }
    });
    if (typeValues.length > 0) {
      params.set(URL_PARAMS.TYPE, typeValues.join(","));
    }
  }

  return params;
}

/**
 * Parses URL search parameters into a MinerListFilter
 */
export function parseFilterFromURL(params: URLSearchParams): MinerListFilter | undefined {
  const statusParam = params.get(URL_PARAMS.STATUS);
  const issuesParam = params.get(URL_PARAMS.ISSUES);
  const typeParam = params.get(URL_PARAMS.TYPE);

  // If no filter params, return undefined
  if (!statusParam && !issuesParam && !typeParam) {
    return undefined;
  }

  const filter = create(MinerListFilterSchema, {
    componentFilters: [],
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
      const componentFilter = create(ComponentStatusFilterSchema, {
        statuses: [ComponentStatus.WARNING, ComponentStatus.ERROR],
      });

      switch (issue) {
        case componentIssues.controlBoard:
          componentFilter.component = ComponentType.CONTROL_BOARD;
          break;
        case componentIssues.fans:
          componentFilter.component = ComponentType.FANS;
          break;
        case componentIssues.hashBoards:
          componentFilter.component = ComponentType.HASH_BOARDS;
          break;
        case componentIssues.psu:
          componentFilter.component = ComponentType.PSU;
          break;
        default:
          return; // Skip unknown issues
      }

      filter.componentFilters.push(componentFilter);
    });
  }

  // Parse miner types
  if (typeParam) {
    const typeValues = typeParam.split(",");
    typeValues.forEach((value) => {
      switch (value) {
        case minerTypes.protoRig:
          filter.types.push(MinerType.PROTO_RIG);
          break;
        case minerTypes.bitmain:
          filter.types.push(MinerType.BITMAIN);
          break;
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
    if (mappedStatuses.length > 0) {
      activeFilters.dropdownFilters.status = mappedStatuses;
    }
  }

  // Parse issues dropdown
  const issuesParam = params.get(URL_PARAMS.ISSUES);
  if (issuesParam) {
    const issueValues = issuesParam.split(",");
    activeFilters.dropdownFilters.issues = issueValues;
  }

  // Parse type dropdown
  const typeParam = params.get(URL_PARAMS.TYPE);
  if (typeParam) {
    const typeValues = typeParam.split(",");
    activeFilters.dropdownFilters.type = typeValues;
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
      params.set(URL_PARAMS.STATUS, urlValues.join(","));
    }
  }

  // Encode issues dropdown
  const issueFilters = filters.dropdownFilters.issues;
  if (issueFilters && issueFilters.length > 0) {
    params.set(URL_PARAMS.ISSUES, issueFilters.join(","));
  }

  // Encode type dropdown
  const typeFilters = filters.dropdownFilters.type;
  if (typeFilters && typeFilters.length > 0) {
    params.set(URL_PARAMS.TYPE, typeFilters.join(","));
  }

  return params;
}
