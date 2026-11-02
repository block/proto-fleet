import {
  getValidSiteScopeId,
  normalizeCurtailmentSelectionValues,
  parseCurtailmentTargetId,
} from "@/protoFleet/api/curtailmentScopes";
import type {
  CurtailmentFormValues,
  CurtailmentResponseProfileOption,
  CurtailmentSiteOption,
  CurtailmentSiteSelection,
} from "@/protoFleet/api/curtailmentTypes";

export interface CurtailmentTargetPath {
  siteSelection: CurtailmentSiteSelection;
  siteIds: string[];
  siteNamesById: Record<string, string>;
  buildingIds: string[];
  rackIds: string[];
  groupIds: string[];
}

export function parseCurtailmentTargetIds(values: readonly string[]): bigint[] {
  return values.flatMap((value) => {
    const parsed = parseCurtailmentTargetId(value);
    return parsed === undefined ? [] : [parsed];
  });
}

export function getValidSiteScopeIds(siteIds?: readonly string[], fallbackSiteId?: string): string[] {
  const normalizedSiteIds = siteIds?.flatMap((siteId) => {
    const validSiteId = getValidSiteScopeId(siteId);
    return validSiteId ? [validSiteId] : [];
  });

  if (normalizedSiteIds !== undefined && normalizedSiteIds.length > 0) {
    return normalizeCurtailmentSelectionValues(normalizedSiteIds);
  }

  const validFallbackSiteId = getValidSiteScopeId(fallbackSiteId);
  return validFallbackSiteId ? [validFallbackSiteId] : [];
}

export function getSelectedSiteIds(
  values: Pick<CurtailmentFormValues, "siteSelection" | "siteId" | "siteIds">,
): string[] {
  return values.siteSelection === "site" ? getValidSiteScopeIds(values.siteIds, values.siteId) : [];
}

export function getSiteScopeIds(values: Pick<CurtailmentFormValues, "siteSelection" | "siteId" | "siteIds">): string[] {
  return values.siteSelection === "site" || values.siteSelection === "allSites"
    ? getValidSiteScopeIds(values.siteIds, values.siteId)
    : [];
}

export function getSiteNameForId(values: Partial<CurtailmentFormValues>, siteId: string): string {
  return (
    values.siteNamesById?.[siteId]?.trim() ||
    (values.siteId === siteId ? values.scopeId?.trim() : undefined) ||
    `Site ${siteId}`
  );
}

function createSiteNamesById(sites: readonly CurtailmentSiteOption[]): Record<string, string> {
  return Object.fromEntries(sites.map((site) => [site.id, site.name]));
}

function getSiteScopeLabel(sites: readonly CurtailmentSiteOption[]): string {
  if (sites.length === 1) {
    return sites[0].name || `Site ${sites[0].id}`;
  }

  return `${sites.length} sites`;
}

export function hasAllMinersSelected(values: Pick<CurtailmentFormValues, "minerSelectionMode">): boolean {
  return values.minerSelectionMode === "all";
}

export function getExplicitMinerCount(
  values: Pick<CurtailmentFormValues, "deviceIdentifiers" | "minerSelectionMode">,
): number {
  return hasAllMinersSelected(values) ? 0 : values.deviceIdentifiers.length;
}

function hasSelectedCurtailmentTarget(
  values: Pick<
    CurtailmentFormValues,
    | "buildingTargetIds"
    | "deviceIdentifiers"
    | "groupTargetIds"
    | "minerSelectionMode"
    | "rackTargetIds"
    | "siteId"
    | "siteIds"
    | "siteSelection"
  >,
): boolean {
  return (
    hasAllMinersSelected(values) ||
    getSiteScopeIds(values).length > 0 ||
    (values.buildingTargetIds?.length ?? 0) > 0 ||
    (values.rackTargetIds?.length ?? 0) > 0 ||
    (values.groupTargetIds?.length ?? 0) > 0 ||
    getExplicitMinerCount(values) > 0
  );
}

export function getCurtailmentTargetPath(values: CurtailmentFormValues): CurtailmentTargetPath {
  return {
    siteSelection: values.siteSelection ?? "none",
    siteIds: getSiteScopeIds(values),
    siteNamesById: values.siteNamesById ?? {},
    buildingIds: values.scopeType === "building" ? (values.buildingTargetIds ?? []) : [],
    rackIds: values.scopeType === "rack" ? (values.rackTargetIds ?? []) : [],
    groupIds: values.scopeType === "group" ? (values.groupTargetIds ?? []) : [],
  };
}

export function getInitialCurtailmentTargetPath(
  values: CurtailmentFormValues,
  initialValues?: Partial<CurtailmentFormValues>,
): CurtailmentTargetPath {
  const path = getCurtailmentTargetPath(values);
  const initialSiteIds = getValidSiteScopeIds(initialValues?.siteIds, initialValues?.siteId);
  if (initialSiteIds.length === 0) {
    return path;
  }
  return {
    ...path,
    siteSelection: initialValues?.siteSelection ?? "site",
    siteIds: initialSiteIds,
    siteNamesById: initialValues?.siteNamesById ?? path.siteNamesById,
  };
}

export function withTerminalScope(
  values: CurtailmentFormValues,
  scope:
    | { type: "building"; ids: string[] }
    | { type: "rack"; ids: string[] }
    | { type: "group"; ids: string[] }
    | { type: "explicitMiners"; ids: string[] },
): CurtailmentFormValues {
  return {
    ...values,
    scopeType: scope.type,
    scopeId: undefined,
    siteSelection: "none",
    siteId: "",
    siteIds: [],
    siteNamesById: {},
    buildingTargetIds: scope.type === "building" ? scope.ids : [],
    rackTargetIds: scope.type === "rack" ? scope.ids : [],
    groupTargetIds: scope.type === "group" ? scope.ids : [],
    deviceIdentifiers: scope.type === "explicitMiners" ? scope.ids : [],
    minerSelectionMode: "subset",
  };
}

export function withTargetPathScope(values: CurtailmentFormValues, path: CurtailmentTargetPath): CurtailmentFormValues {
  if (path.groupIds.length > 0) {
    return withTerminalScope(values, { type: "group", ids: path.groupIds });
  }
  if (path.rackIds.length > 0) {
    return withTerminalScope(values, { type: "rack", ids: path.rackIds });
  }
  if (path.buildingIds.length > 0) {
    return withTerminalScope(values, { type: "building", ids: path.buildingIds });
  }
  if (path.siteIds.length > 0) {
    const sites = path.siteIds.map((siteId) => ({
      id: siteId,
      name: path.siteNamesById[siteId] ?? `Site ${siteId}`,
    }));
    return path.siteSelection === "allSites" ? withAllSitesScope(values, sites) : withSiteScopes(values, sites);
  }
  return withWholeFleetScope(values);
}

function withWholeFleetScope(values: CurtailmentFormValues): CurtailmentFormValues {
  return {
    ...values,
    scopeType: "wholeOrg",
    scopeId: "whole-org",
    siteSelection: "none",
    siteId: "",
    siteIds: [],
    siteNamesById: {},
    buildingTargetIds: [],
    rackTargetIds: [],
    groupTargetIds: [],
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
  };
}

export function withAllMinerScope(values: CurtailmentFormValues): CurtailmentFormValues {
  return {
    ...withWholeFleetScope(values),
    siteSelection: "allSites",
    minerSelectionMode: "all",
  };
}

function withAllSitesScope(
  values: CurtailmentFormValues,
  sites: readonly CurtailmentSiteOption[] = getSiteScopeIds(values).map((siteId) => ({
    id: siteId,
    name: getSiteNameForId(values, siteId),
  })),
): CurtailmentFormValues {
  const selectedSites = normalizeCurtailmentSelectionValues(sites.map((site) => site.id)).map((siteId) => {
    const site = sites.find((candidate) => candidate.id === siteId);
    return {
      id: siteId,
      name: site?.name?.trim() || `Site ${siteId}`,
    };
  });
  const firstSite = selectedSites[0];
  return {
    ...values,
    scopeType: selectedSites.length > 0 ? "site" : "wholeOrg",
    scopeId: selectedSites.length > 0 ? "All sites" : "whole-org",
    siteSelection: "allSites",
    siteId: firstSite?.id ?? "",
    siteIds: selectedSites.map((site) => site.id),
    siteNamesById: createSiteNamesById(selectedSites),
    buildingTargetIds: [],
    rackTargetIds: [],
    groupTargetIds: [],
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
  };
}

function withNoSiteScope(values: CurtailmentFormValues): CurtailmentFormValues {
  const hasSelectedMiners = getExplicitMinerCount(values) > 0;

  return {
    ...values,
    scopeType: hasSelectedMiners ? "explicitMiners" : "wholeOrg",
    scopeId: hasSelectedMiners ? undefined : "whole-org",
    siteSelection: "none",
    siteId: "",
    siteIds: [],
    siteNamesById: {},
    buildingTargetIds: [],
    rackTargetIds: [],
    groupTargetIds: [],
  };
}

export function withSiteScopes(
  values: CurtailmentFormValues,
  sites: readonly CurtailmentSiteOption[],
): CurtailmentFormValues {
  const selectedSites = normalizeCurtailmentSelectionValues(sites.map((site) => site.id)).map((siteId) => {
    const site = sites.find((candidate) => candidate.id === siteId);
    return {
      id: siteId,
      name: site?.name?.trim() || `Site ${siteId}`,
    };
  });
  if (selectedSites.length === 0) {
    return withNoSiteScope(values);
  }

  const firstSite = selectedSites[0];

  return {
    ...values,
    scopeType: "site",
    scopeId: getSiteScopeLabel(selectedSites),
    siteSelection: "site",
    siteId: firstSite.id,
    siteIds: selectedSites.map((site) => site.id),
    siteNamesById: createSiteNamesById(selectedSites),
    buildingTargetIds: [],
    rackTargetIds: [],
    groupTargetIds: [],
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
  };
}

export function withResponseProfileScope(values: CurtailmentFormValues): CurtailmentFormValues {
  const siteIds = getSelectedSiteIds(values);

  if (values.scopeType === "building" && (values.buildingTargetIds?.length ?? 0) > 0) {
    return withTerminalScope(values, { type: "building", ids: values.buildingTargetIds ?? [] });
  }

  if (values.scopeType === "rack" && (values.rackTargetIds?.length ?? 0) > 0) {
    return withTerminalScope(values, { type: "rack", ids: values.rackTargetIds ?? [] });
  }

  if (values.scopeType === "group" && (values.groupTargetIds?.length ?? 0) > 0) {
    return withTerminalScope(values, { type: "group", ids: values.groupTargetIds ?? [] });
  }

  if (values.scopeType === "explicitMiners" && getExplicitMinerCount(values) > 0) {
    return withTerminalScope(values, { type: "explicitMiners", ids: values.deviceIdentifiers });
  }

  if (hasAllMinersSelected(values)) {
    return withAllMinerScope(values);
  }

  if (values.siteSelection === "allSites") {
    return withAllSitesScope(values);
  }

  if (values.siteSelection === "site" && siteIds.length > 0) {
    return withSiteScopes(
      values,
      siteIds.map((siteId) => ({
        id: siteId,
        name: getSiteNameForId(values, siteId),
      })),
    );
  }

  return withWholeFleetScope(values);
}

export function withDefaultSiteScope(
  values: CurtailmentFormValues,
  defaultSiteScope?: CurtailmentSiteOption,
): CurtailmentFormValues {
  return defaultSiteScope && !hasSelectedCurtailmentTarget(values)
    ? withSiteScopes(values, [defaultSiteScope])
    : values;
}

export function hasResponseProfileScopeValues(
  responseProfileValues: CurtailmentResponseProfileOption["values"],
): boolean {
  return (
    "scopeType" in responseProfileValues ||
    "scopeId" in responseProfileValues ||
    "siteSelection" in responseProfileValues ||
    "siteId" in responseProfileValues ||
    "siteIds" in responseProfileValues ||
    "siteNamesById" in responseProfileValues ||
    "buildingTargetIds" in responseProfileValues ||
    "rackTargetIds" in responseProfileValues ||
    "groupTargetIds" in responseProfileValues ||
    "deviceIdentifiers" in responseProfileValues ||
    "minerSelectionMode" in responseProfileValues
  );
}

function removeResponseProfileScopeValues(
  values: CurtailmentResponseProfileOption["values"],
): CurtailmentResponseProfileOption["values"] {
  const behaviorValues = { ...values };

  delete behaviorValues.scopeType;
  delete behaviorValues.scopeId;
  delete behaviorValues.siteSelection;
  delete behaviorValues.siteId;
  delete behaviorValues.siteIds;
  delete behaviorValues.siteNamesById;
  delete behaviorValues.buildingTargetIds;
  delete behaviorValues.rackTargetIds;
  delete behaviorValues.groupTargetIds;
  delete behaviorValues.deviceIdentifiers;
  delete behaviorValues.minerSelectionMode;

  return behaviorValues;
}

export function withSelectedResponseProfileValues(
  values: CurtailmentFormValues,
  responseProfileValues: CurtailmentResponseProfileOption["values"],
): CurtailmentFormValues {
  const hasScopeValues = hasResponseProfileScopeValues(responseProfileValues);
  const behaviorValues = hasScopeValues
    ? removeResponseProfileScopeValues(responseProfileValues)
    : responseProfileValues;
  const nextValues = {
    ...values,
    ...behaviorValues,
  };

  if (!hasScopeValues) {
    return nextValues;
  }

  const siteIds = getValidSiteScopeIds(responseProfileValues.siteIds, responseProfileValues.siteId);
  const deviceIdentifiers = responseProfileValues.deviceIdentifiers ?? [];
  const buildingIds = responseProfileValues.buildingTargetIds ?? [];
  const rackIds = responseProfileValues.rackTargetIds ?? [];
  const groupIds = responseProfileValues.groupTargetIds ?? [];
  const minerSelectionMode = responseProfileValues.minerSelectionMode ?? "subset";
  const siteSelection = responseProfileValues.siteSelection ?? (siteIds.length > 0 ? "site" : "none");
  const scopeType =
    responseProfileValues.scopeType ??
    (siteSelection === "allSites"
      ? "wholeOrg"
      : siteIds.length > 0
        ? "site"
        : deviceIdentifiers.length
          ? "explicitMiners"
          : "wholeOrg");

  if (minerSelectionMode === "all") {
    return withAllMinerScope({ ...nextValues, deviceIdentifiers: [], minerSelectionMode });
  }

  if (siteSelection === "allSites") {
    return withAllSitesScope({ ...nextValues, deviceIdentifiers, minerSelectionMode });
  }

  if (scopeType === "site" || siteSelection === "site") {
    if (siteIds.length > 0) {
      return withSiteScopes(
        { ...nextValues, deviceIdentifiers, minerSelectionMode },
        siteIds.map((siteId) => ({
          id: siteId,
          name: getSiteNameForId(responseProfileValues, siteId),
        })),
      );
    }

    return withWholeFleetScope(nextValues);
  }

  if (scopeType === "building" && buildingIds.length > 0) {
    return withTerminalScope(nextValues, { type: "building", ids: buildingIds });
  }

  if (scopeType === "rack" && rackIds.length > 0) {
    return withTerminalScope(nextValues, { type: "rack", ids: rackIds });
  }

  if (scopeType === "group" && groupIds.length > 0) {
    return withTerminalScope(nextValues, { type: "group", ids: groupIds });
  }

  if (scopeType === "explicitMiners") {
    return withTerminalScope({ ...nextValues, minerSelectionMode }, { type: "explicitMiners", ids: deviceIdentifiers });
  }

  return withWholeFleetScope(nextValues);
}
