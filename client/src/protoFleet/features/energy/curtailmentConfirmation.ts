import { getExplicitMinerCount, getSiteScopeIds, hasAllMinersSelected } from "./curtailmentFormScope";
import type {
  CurtailmentFormValues,
  CurtailmentPlanPreview,
  CurtailmentSubmitValues,
} from "@/protoFleet/api/curtailmentTypes";
import { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import {
  formatCurtailmentAppliesToSummary,
  formatCurtailmentFacilityFanCount,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export type PendingCurtailmentConfirmation = {
  action: "run" | "test";
  values: CurtailmentSubmitValues;
};

export type ForceInclusionFields = Pick<CurtailmentFormValues, "forceIncludeAllPairedMiners">;

export function getSelectedMinerIds(values: CurtailmentFormValues): string[] {
  return hasAllMinersSelected(values) ? [] : values.deviceIdentifiers;
}

export function formatCountLabel(count: number, singular: string): string {
  return getTargetButtonLabel(count, singular);
}

function getFacilityFanDeviceCount(values: Pick<CurtailmentFormValues, "facilityFanDeviceIds">): number {
  return values.facilityFanDeviceIds?.length ?? 0;
}

export function formatCurtailmentPreviewSummary(preview: CurtailmentPlanPreview): string {
  const appliesToSummary = formatCurtailmentAppliesToSummary(
    preview.selectedMinerCount,
    preview.facilityFanDeviceCount,
  ).toLowerCase();

  return `Curtail ${appliesToSummary} ${preview.scopeLabel} immediately`;
}

function formatScopeLabelForSentence(scopeLabel: string): string {
  return scopeLabel === "All sites" ? "all sites" : scopeLabel;
}

function formatCurtailmentConfirmationTarget(values: CurtailmentFormValues, selectedMinerCount?: number): string {
  if (hasAllMinersSelected(values)) {
    return "the whole fleet";
  }

  if (values.scopeType === "explicitMiners" && getExplicitMinerCount(values) > 0) {
    return formatCountLabel(values.deviceIdentifiers.length, "miner").toLowerCase();
  }

  if (values.scopeType === "building" && (values.buildingTargetIds?.length ?? 0) > 0) {
    return `miners in ${formatCountLabel(values.buildingTargetIds?.length ?? 0, "building").toLowerCase()}`;
  }

  if (values.scopeType === "rack" && (values.rackTargetIds?.length ?? 0) > 0) {
    return `miners in ${formatCountLabel(values.rackTargetIds?.length ?? 0, "rack").toLowerCase()}`;
  }

  if (values.scopeType === "group" && (values.groupTargetIds?.length ?? 0) > 0) {
    return `miners in ${formatCountLabel(values.groupTargetIds?.length ?? 0, "group").toLowerCase()}`;
  }

  if (values.scopeType === "site") {
    const selectedSiteIds = getSiteScopeIds(values);
    if (selectedSiteIds.length > 0) {
      const selectedSiteLabel =
        values.siteSelection === "allSites"
          ? "all sites"
          : selectedSiteIds.length === 1
            ? values.scopeId
            : formatCountLabel(selectedSiteIds.length, "site").toLowerCase();

      return `miners in ${selectedSiteLabel ? formatScopeLabelForSentence(selectedSiteLabel) : "the selected sites"}`;
    }
  }

  if (values.curtailmentMode === "fullFleet") {
    return "the whole fleet";
  }

  if (selectedMinerCount !== undefined && selectedMinerCount > 0) {
    return formatCountLabel(selectedMinerCount, "miner").toLowerCase();
  }

  return "miners across the fleet";
}

function formatCurtailmentConfirmationTargetWithInfrastructure(
  values: CurtailmentFormValues,
  selectedMinerCount?: number,
): string {
  const target = formatCurtailmentConfirmationTarget(values, selectedMinerCount);
  const facilityFanDeviceCount = getFacilityFanDeviceCount(values);

  if (facilityFanDeviceCount <= 0) {
    return target;
  }

  return `${target} and ${formatCurtailmentFacilityFanCount(facilityFanDeviceCount)}`;
}

export function getCurtailmentConfirmationCopy(
  pendingConfirmation: PendingCurtailmentConfirmation | null,
  selectedMinerCount?: number,
) {
  if (!pendingConfirmation) {
    return null;
  }

  const target = formatCurtailmentConfirmationTargetWithInfrastructure(pendingConfirmation.values, selectedMinerCount);
  const body =
    pendingConfirmation.action === "test"
      ? `This will save the profile, then trigger curtailment for ${target}. Schedules stay suppressed until miners are restored.`
      : `This will curtail ${target} immediately. Schedules stay suppressed until miners are restored.`;

  return {
    title: "Run curtailment?",
    body,
    confirmText: "Run curtailment",
  };
}

// The maintenance-inclusion toggle is hidden from the UI (see the "Miners" section in the form),
// so "Target all paired miners" is the only user-driven force-inclusion and the confirmation copy
// always describes the all-paired case.
export function getForceInclusionConfirmationCopy() {
  return {
    title: "Force include all paired miners?",
    body: "This will keep targeting paired miners even when they are offline, sleeping, or waiting for authentication, and includes miners flagged for maintenance.",
    confirmText: "Force include",
  };
}
