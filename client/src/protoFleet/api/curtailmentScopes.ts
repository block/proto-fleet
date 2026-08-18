import { create } from "@bufbuild/protobuf";

import {
  type CurtailmentScope,
  CurtailmentScopeSchema,
  ScopeBuildingSchema,
  ScopeDeviceListSchema,
  ScopeGroupSchema,
  ScopeRackSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

export type CurtailmentTerminalScopeType = "wholeOrg" | "site" | "building" | "rack" | "group" | "explicitMiners";

export const curtailmentScopeSchemaVersion = 1;

export type CurtailmentTerminalScope =
  | { type: "wholeOrg" }
  | { type: "site"; siteIds: string[] }
  | { type: "building"; buildingIds: string[] }
  | { type: "rack"; rackIds: string[] }
  | { type: "group"; groupIds: string[] }
  | { type: "deviceIdentifiers"; deviceIdentifiers: string[] };

export type CurtailmentScopeSelection = {
  scopeType?: CurtailmentTerminalScopeType | "deviceSet";
  minerSelectionMode?: "all" | "subset";
  siteSelection?: "none" | "site" | "allSites";
  siteId?: string;
  siteIds?: readonly string[];
  buildingTargetIds?: readonly string[];
  rackTargetIds?: readonly string[];
  groupTargetIds?: readonly string[];
  deviceIdentifiers?: readonly string[];
};

export type CurtailmentScopeFormFields = {
  scopeType: CurtailmentTerminalScopeType;
  siteIds: string[];
  buildingTargetIds: string[];
  rackTargetIds: string[];
  groupTargetIds: string[];
  deviceIdentifiers: string[];
};

type CurtailmentScopeSummaryOptions = {
  fallbackLabel: string;
  getSiteLabel?: (siteId: string) => string | undefined;
};

export function normalizeCurtailmentSelectionValues(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

const maxInt64 = 9_223_372_036_854_775_807n;
const baseTenIntegerPattern = /^[0-9]+$/;

export function parseCurtailmentTargetId(value: string | undefined): bigint | undefined {
  const trimmed = value?.trim() ?? "";
  if (!baseTenIntegerPattern.test(trimmed)) {
    return undefined;
  }

  const parsed = BigInt(trimmed);
  return parsed > 0n && parsed <= maxInt64 ? parsed : undefined;
}

function getSelectedSiteIds(selection: CurtailmentScopeSelection): string[] {
  const siteIds =
    selection.siteIds !== undefined && selection.siteIds.length > 0
      ? selection.siteIds
      : selection.siteId
        ? [selection.siteId]
        : [];
  return normalizeCurtailmentSelectionValues(siteIds);
}

export function getCurtailmentTerminalScope(
  selection: CurtailmentScopeSelection,
): CurtailmentTerminalScope | undefined {
  switch (selection.scopeType) {
    case "wholeOrg":
      return { type: "wholeOrg" };
    case "site": {
      const siteIds = getSelectedSiteIds(selection);
      return siteIds.length > 0 ? { type: "site", siteIds } : undefined;
    }
    case "building": {
      const buildingIds = normalizeCurtailmentSelectionValues(selection.buildingTargetIds ?? []);
      return buildingIds.length > 0 ? { type: "building", buildingIds } : undefined;
    }
    case "rack": {
      const rackIds = normalizeCurtailmentSelectionValues(selection.rackTargetIds ?? []);
      return rackIds.length > 0 ? { type: "rack", rackIds } : undefined;
    }
    case "group": {
      const groupIds = normalizeCurtailmentSelectionValues(selection.groupTargetIds ?? []);
      return groupIds.length > 0 ? { type: "group", groupIds } : undefined;
    }
    case "explicitMiners": {
      const deviceIdentifiers = normalizeCurtailmentSelectionValues(selection.deviceIdentifiers ?? []);
      return deviceIdentifiers.length > 0 ? { type: "deviceIdentifiers", deviceIdentifiers } : undefined;
    }
    case "deviceSet":
    case undefined:
      return undefined;
  }
}

function formatScopeCount(count: number, singular: string): string {
  return `${count} ${count === 1 ? singular : `${singular}s`}`;
}

export function getCurtailmentScopeSummary(
  scopeOrSelection: CurtailmentTerminalScope | CurtailmentScopeSelection,
  { fallbackLabel, getSiteLabel }: CurtailmentScopeSummaryOptions,
): string {
  let scope: CurtailmentTerminalScope | undefined;
  let isAllSites = false;

  if ("type" in scopeOrSelection) {
    scope = scopeOrSelection;
  } else {
    isAllSites = scopeOrSelection.siteSelection === "allSites";
    scope = getCurtailmentTerminalScope(scopeOrSelection);
    if (!scope && scopeOrSelection.scopeType === undefined) {
      const deviceIdentifiers = normalizeCurtailmentSelectionValues(scopeOrSelection.deviceIdentifiers ?? []);
      const siteIds = getSelectedSiteIds(scopeOrSelection);
      if (scopeOrSelection.minerSelectionMode === "all") {
        scope = { type: "wholeOrg" };
      } else if (deviceIdentifiers.length > 0) {
        scope = { type: "deviceIdentifiers", deviceIdentifiers };
      } else if (isAllSites || siteIds.length > 0) {
        scope = { type: "site", siteIds };
      }
    }
  }

  if (!scope || scope.type === "wholeOrg") {
    return fallbackLabel;
  }
  if (scope.type === "deviceIdentifiers") {
    return formatScopeCount(scope.deviceIdentifiers.length, "miner");
  }
  if (scope.type === "site") {
    if (isAllSites) {
      return "All sites";
    }
    if (scope.siteIds.length === 1) {
      const siteId = scope.siteIds[0];
      return getSiteLabel?.(siteId)?.trim() || `Site ${siteId}`;
    }
    return formatScopeCount(scope.siteIds.length, "site");
  }

  const ids = scope.type === "building" ? scope.buildingIds : scope.type === "rack" ? scope.rackIds : scope.groupIds;
  return formatScopeCount(ids.length, scope.type);
}

function createWholeOrgCurtailmentScope(): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) },
  });
}

function createSiteCurtailmentScope(siteId: bigint): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "site", value: create(ScopeSiteSchema, { siteId }) },
  });
}

function createDeviceCurtailmentScope(deviceIdentifiers: string[]): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "deviceIdentifiers", value: create(ScopeDeviceListSchema, { deviceIdentifiers }) },
  });
}

function createBuildingCurtailmentScope(buildingId: bigint): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId }) },
  });
}

function createRackCurtailmentScope(rackId: bigint): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "rack", value: create(ScopeRackSchema, { rackId }) },
  });
}

function createGroupCurtailmentScope(groupId: bigint): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "group", value: create(ScopeGroupSchema, { groupId }) },
  });
}

function createNumericCurtailmentScopes(
  values: readonly string[],
  createScope: (value: bigint) => CurtailmentScope,
): CurtailmentScope[] | undefined {
  const parsedValues = new Set<bigint>();
  for (const value of normalizeCurtailmentSelectionValues(values)) {
    const parsed = parseCurtailmentTargetId(value);
    if (parsed === undefined) {
      return undefined;
    }
    parsedValues.add(parsed);
  }
  return parsedValues.size > 0 ? [...parsedValues].map(createScope) : undefined;
}

function createCurtailmentScopes(scope: CurtailmentTerminalScope): CurtailmentScope[] | undefined {
  switch (scope.type) {
    case "wholeOrg":
      return [createWholeOrgCurtailmentScope()];
    case "site":
      return createNumericCurtailmentScopes(scope.siteIds, createSiteCurtailmentScope);
    case "building":
      return createNumericCurtailmentScopes(scope.buildingIds, createBuildingCurtailmentScope);
    case "rack":
      return createNumericCurtailmentScopes(scope.rackIds, createRackCurtailmentScope);
    case "group":
      return createNumericCurtailmentScopes(scope.groupIds, createGroupCurtailmentScope);
    case "deviceIdentifiers": {
      const deviceIdentifiers = normalizeCurtailmentSelectionValues(scope.deviceIdentifiers);
      return deviceIdentifiers.length > 0 ? [createDeviceCurtailmentScope(deviceIdentifiers)] : undefined;
    }
  }
}

export function buildCurtailmentScopes(selection: CurtailmentScopeSelection): CurtailmentScope[] | undefined {
  const scope = getCurtailmentTerminalScope(selection);
  return scope ? createCurtailmentScopes(scope) : undefined;
}

export function getCurtailmentScopeFormFields(scope: CurtailmentTerminalScope): CurtailmentScopeFormFields {
  const fields: CurtailmentScopeFormFields = {
    scopeType: scope.type === "deviceIdentifiers" ? "explicitMiners" : scope.type,
    siteIds: [],
    buildingTargetIds: [],
    rackTargetIds: [],
    groupTargetIds: [],
    deviceIdentifiers: [],
  };

  switch (scope.type) {
    case "site":
      fields.siteIds = [...scope.siteIds];
      break;
    case "building":
      fields.buildingTargetIds = [...scope.buildingIds];
      break;
    case "rack":
      fields.rackTargetIds = [...scope.rackIds];
      break;
    case "group":
      fields.groupTargetIds = [...scope.groupIds];
      break;
    case "deviceIdentifiers":
      fields.deviceIdentifiers = [...scope.deviceIdentifiers];
      break;
    case "wholeOrg":
      break;
  }

  return fields;
}

function getCurtailmentScopeCount(scope: CurtailmentTerminalScope): number | undefined {
  switch (scope.type) {
    case "wholeOrg":
      return undefined;
    case "site":
      return scope.siteIds.length;
    case "building":
      return scope.buildingIds.length;
    case "rack":
      return scope.rackIds.length;
    case "group":
      return scope.groupIds.length;
    case "deviceIdentifiers":
      return scope.deviceIdentifiers.length;
  }
}

export function getCurtailmentScopeSelectionCount(selection: CurtailmentScopeSelection): number | undefined {
  const scopes = buildCurtailmentScopes(selection);
  return scopes ? getCurtailmentScopeCount(parseCurtailmentTerminalScopes(scopes)) : undefined;
}

export function parseCurtailmentTerminalScopes(scopes: readonly CurtailmentScope[]): CurtailmentTerminalScope {
  const siteIds = new Set<string>();
  const buildingIds = new Set<string>();
  const rackIds = new Set<string>();
  const groupIds = new Set<string>();
  const deviceIdentifiers = new Set<string>();
  let hasWholeOrg = false;

  for (const scope of scopes) {
    switch (scope.scope.case) {
      case "wholeOrg":
        hasWholeOrg = true;
        break;
      case "site":
        siteIds.add(scope.scope.value.siteId.toString());
        break;
      case "deviceIdentifiers":
        scope.scope.value.deviceIdentifiers.forEach((identifier) => deviceIdentifiers.add(identifier));
        break;
      case "building":
        buildingIds.add(scope.scope.value.buildingId.toString());
        break;
      case "rack":
        rackIds.add(scope.scope.value.rackId.toString());
        break;
      case "group":
        groupIds.add(scope.scope.value.groupId.toString());
        break;
      case undefined:
        break;
    }
  }

  const selectorTypeCount =
    Number(hasWholeOrg) +
    Number(siteIds.size > 0) +
    Number(buildingIds.size > 0) +
    Number(rackIds.size > 0) +
    Number(groupIds.size > 0) +
    Number(deviceIdentifiers.size > 0);
  if (selectorTypeCount !== 1) {
    throw new Error("Curtailment scopes must contain exactly one terminal selector type");
  }
  if (hasWholeOrg) {
    return { type: "wholeOrg" };
  }
  if (siteIds.size > 0) {
    return { type: "site", siteIds: [...siteIds] };
  }
  if (buildingIds.size > 0) {
    return { type: "building", buildingIds: [...buildingIds] };
  }
  if (rackIds.size > 0) {
    return { type: "rack", rackIds: [...rackIds] };
  }
  if (groupIds.size > 0) {
    return { type: "group", groupIds: [...groupIds] };
  }
  return { type: "deviceIdentifiers", deviceIdentifiers: [...deviceIdentifiers] };
}
