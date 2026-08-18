import { create } from "@bufbuild/protobuf";

import {
  type CurtailmentScope,
  CurtailmentScopeSchema,
  ScopeDeviceListSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

export type CurtailmentTerminalScope =
  | { type: "wholeOrg" }
  | { type: "site"; siteIds: string[] }
  | { type: "building"; buildingIds: string[] }
  | { type: "rack"; rackIds: string[] }
  | { type: "group"; groupIds: string[] }
  | { type: "deviceIdentifiers"; deviceIdentifiers: string[] };

type CurtailmentScopeSummarySelection = {
  minerSelectionMode?: "all" | "subset";
  siteSelection?: "none" | "site" | "allSites";
  siteId?: string;
  siteIds?: readonly string[];
  deviceIdentifiers?: readonly string[];
};

type CurtailmentScopeSummaryOptions = {
  fallbackLabel: string;
  getSiteLabel?: (siteId: string) => string | undefined;
};

export function normalizeCurtailmentSelectionValues(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function formatScopeCount(count: number, singular: string): string {
  return `${count} ${count === 1 ? singular : `${singular}s`}`;
}

export function getCurtailmentScopeSummary(
  scopeOrSelection: CurtailmentTerminalScope | CurtailmentScopeSummarySelection,
  { fallbackLabel, getSiteLabel }: CurtailmentScopeSummaryOptions,
): string {
  let scope: CurtailmentTerminalScope | undefined;
  let isAllSites = false;

  if ("type" in scopeOrSelection) {
    scope = scopeOrSelection;
  } else if (scopeOrSelection.minerSelectionMode === "all") {
    scope = { type: "wholeOrg" };
  } else if ((scopeOrSelection.deviceIdentifiers?.length ?? 0) > 0) {
    scope = {
      type: "deviceIdentifiers",
      deviceIdentifiers: [...(scopeOrSelection.deviceIdentifiers ?? [])],
    };
  } else {
    const siteIds = normalizeCurtailmentSelectionValues(
      scopeOrSelection.siteIds !== undefined && scopeOrSelection.siteIds.length > 0
        ? scopeOrSelection.siteIds
        : scopeOrSelection.siteId
          ? [scopeOrSelection.siteId]
          : [],
    );
    isAllSites = scopeOrSelection.siteSelection === "allSites";
    const hasSiteScope =
      isAllSites ||
      ((scopeOrSelection.siteSelection === "site" || scopeOrSelection.siteSelection === undefined) &&
        siteIds.length > 0);
    if (hasSiteScope) {
      scope = { type: "site", siteIds };
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

export function createWholeOrgCurtailmentScope(): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) },
  });
}

export function createSiteCurtailmentScope(siteId: bigint): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "site", value: create(ScopeSiteSchema, { siteId }) },
  });
}

export function createDeviceCurtailmentScope(deviceIdentifiers: string[]): CurtailmentScope {
  return create(CurtailmentScopeSchema, {
    scope: { case: "deviceIdentifiers", value: create(ScopeDeviceListSchema, { deviceIdentifiers }) },
  });
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
