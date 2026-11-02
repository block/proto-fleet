import { normalizeResponseProfileSiteFields } from "@/protoFleet/api/curtailmentResponseProfileUtils";
import { type CurtailmentScopeSelection } from "@/protoFleet/api/curtailmentScopes";

import { getCurtailmentScopeSummary, getCurtailmentTerminalScope } from "@/protoFleet/api/curtailmentScopes";
import {
  type CurtailmentFormValues,
  type CurtailmentSiteOption,
  type CurtailmentSubmitValues,
  type ResponseProfile,
  type ResponseProfileFormValues,
} from "@/protoFleet/api/curtailmentTypes";

import type { CurtailmentResponseProfileOption, EditableResponseProfile } from "@/protoFleet/api/curtailmentTypes";
import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { immediateRestoreBatchSizeInputValue } from "@/protoFleet/features/energy/curtailmentNumericFields";

export function createSiteOptions(sites: SiteWithCounts[]): CurtailmentSiteOption[] {
  return sites
    .flatMap((siteWithCounts) => {
      const site = siteWithCounts.site;
      const id = site?.id ? site.id.toString() : "";

      return id ? [{ id, name: site?.name || `Site ${id}` }] : [];
    })
    .sort((left, right) => left.name.localeCompare(right.name, undefined, { sensitivity: "base" }));
}

export function cloneTopologyTargetIds(
  values: Pick<CurtailmentScopeSelection, "buildingTargetIds" | "rackTargetIds" | "groupTargetIds">,
) {
  return {
    buildingTargetIds: [...(values.buildingTargetIds ?? [])],
    rackTargetIds: [...(values.rackTargetIds ?? [])],
    groupTargetIds: [...(values.groupTargetIds ?? [])],
  };
}

export function normalizeResponseProfileFormValues(values: ResponseProfileFormValues): ResponseProfileFormValues {
  const hasAllMinersSelected = values.minerSelectionMode === "all";

  return {
    ...values,
    ...cloneTopologyTargetIds(values),
    deviceIdentifiers: hasAllMinersSelected ? [] : [...values.deviceIdentifiers],
    ...normalizeResponseProfileSiteFields(values),
  };
}

export function createResponseProfileFormValuesFromProfile(
  profile: ResponseProfile,
): ResponseProfileFormValues | undefined {
  return profile.formValues ? normalizeResponseProfileFormValues(profile.formValues) : undefined;
}

export function createCurtailmentFormValuesFromResponseProfile(
  values: ResponseProfileFormValues,
): Partial<CurtailmentFormValues> {
  const restoreBatchSize =
    values.restoreBatchSize ||
    (values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSizeInputValue : "");
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteFields = normalizeResponseProfileSiteFields(values);
  const { siteIds, siteId, siteNamesById } = siteFields;
  const siteName = siteId ? siteNamesById[siteId] : "";
  const deviceIdentifiers = hasAllMinersSelected ? [] : [...values.deviceIdentifiers];
  const terminalScope = getCurtailmentTerminalScope(values);
  const scopeType = hasAllMinersSelected
    ? "wholeOrg"
    : terminalScope?.type === "deviceIdentifiers"
      ? "explicitMiners"
      : (terminalScope?.type ??
        (deviceIdentifiers.length > 0 ? "explicitMiners" : siteIds.length > 0 ? "site" : "wholeOrg"));
  const scopeId =
    scopeType === "wholeOrg"
      ? "whole-org"
      : siteIds.length > 0
        ? siteIds.length === 1
          ? siteName
          : `${siteIds.length} sites`
        : scopeType === "explicitMiners"
          ? undefined
          : terminalScope
            ? getCurtailmentScopeSummary(terminalScope, {
                fallbackLabel: "Whole organization",
                getSiteLabel: (id) => siteNamesById[id],
              })
            : undefined;

  return {
    responseProfileId: "customPlan",
    reason: values.name,
    minDurationSec: values.minDurationSec,
    maxDurationSec: values.maxDurationSec,
    scopeType,
    scopeId,
    ...siteFields,
    buildingTargetIds: [...(values.buildingTargetIds ?? [])],
    rackTargetIds: [...(values.rackTargetIds ?? [])],
    groupTargetIds: [...(values.groupTargetIds ?? [])],
    deviceIdentifiers,
    minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
    curtailmentMode: values.actionType,
    minerSelectionStrategy: values.selectionStrategy,
    targetKw: values.targetKw,
    toleranceKw: values.toleranceKw,
    priority: values.priority,
    postEventCooldownSec: values.postEventCooldownSec,
    curtailBatchSize: values.curtailBatchSize,
    curtailBatchIntervalSec: values.curtailBatchIntervalSec,
    restoreBatchSize,
    restoreIntervalSec: values.restoreIntervalSec,
    facilityFanDeviceIds: [...(values.facilityFanDeviceIds ?? [])],
    fanOffDelaySec: values.fanOffDelaySec ?? "",
    fanRestoreDelaySec: values.fanRestoreDelaySec ?? "",
    includeMaintenance: values.includeMaintenance,
    forceIncludeAllPairedMiners: values.actionType === "fullFleet" && Boolean(values.forceIncludeAllPairedMiners),
  };
}

export function createCurtailmentResponseProfileOption(
  profile: EditableResponseProfile,
): CurtailmentResponseProfileOption {
  const {
    reason: _reason,
    minDurationSec: _min,
    maxDurationSec: _max,
    responseProfileId: _profile,
    ...values
  } = createCurtailmentFormValuesFromResponseProfile(normalizeResponseProfileFormValues(profile.formValues));
  return { id: profile.id, label: profile.name, revision: profile.revision, values };
}
export function secondsToDeadlineMinutes(value: string): string {
  const seconds = Number(value);

  if (!Number.isFinite(seconds) || seconds <= 0) {
    return "";
  }

  return Math.max(Math.ceil(seconds / 60), 1).toString();
}

export function getResponseProfileRestoreBehavior(
  values: CurtailmentSubmitValues,
): ResponseProfileFormValues["restoreBehavior"] {
  const restoreBatchSize = Number(values.restoreBatchSize);
  const restoreIntervalSec = Number(values.restoreIntervalSec || "0");

  return Number.isFinite(restoreBatchSize) &&
    restoreBatchSize === Number(immediateRestoreBatchSizeInputValue) &&
    Number.isFinite(restoreIntervalSec) &&
    restoreIntervalSec === 0
    ? "automaticImmediateRestore"
    : "automaticBatchRestore";
}

export function createResponseProfileFormValuesFromCurtailmentValues(
  values: CurtailmentSubmitValues,
): ResponseProfileFormValues {
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteFields = normalizeResponseProfileSiteFields({
    ...values,
    siteId: values.siteId ?? "",
    siteName: values.scopeId ?? "",
  });
  const deviceIdentifiers = hasAllMinersSelected ? [] : [...values.deviceIdentifiers];

  return {
    name: values.reason,
    actionType: values.curtailmentMode,
    targetKw: values.targetKw,
    toleranceKw: values.toleranceKw,
    priority: values.priority,
    postEventCooldownSec: values.postEventCooldownSec ?? "",
    scopeType: values.scopeType,
    ...cloneTopologyTargetIds(values),
    deviceIdentifiers,
    ...siteFields,
    selectionStrategy: values.minerSelectionStrategy,
    restoreBehavior: getResponseProfileRestoreBehavior(values),
    minDurationSec: values.minDurationSec,
    maxDurationSec: values.maxDurationSec,
    curtailBatchSize: values.curtailBatchSize,
    curtailBatchIntervalSec: values.curtailBatchIntervalSec,
    restoreBatchSize: values.restoreBatchSize,
    restoreIntervalSec: values.restoreIntervalSec,
    facilityFanDeviceIds: [...(values.facilityFanDeviceIds ?? [])],
    fanOffDelaySec: values.fanOffDelaySec ?? "",
    fanRestoreDelaySec: values.fanRestoreDelaySec ?? "",
    responseDeadlineMinutes: secondsToDeadlineMinutes(values.maxDurationSec),
    includeMaintenance: values.includeMaintenance,
    forceIncludeAllPairedMiners: values.curtailmentMode === "fullFleet" && Boolean(values.forceIncludeAllPairedMiners),
  };
}
