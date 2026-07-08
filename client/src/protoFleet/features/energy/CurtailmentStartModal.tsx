import { type ReactElement, type ReactNode, useEffect, useMemo, useState } from "react";

import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import { formatCurtailmentKw as formatKw } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import {
  curtailmentNumericFieldLimits,
  parseOptionalUint32Field,
} from "@/protoFleet/features/energy/curtailmentNumericFields";
import {
  parseCurtailmentSiteId,
  supportsAllPairedTargeting,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import {
  createCurtailmentPlanPreview,
  getUnsupportedDeviceSetPreviewError,
  useCurtailmentPlanPreview,
} from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import MinerSelectionModal, {
  type MinerSelectionValue,
} from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import { Alert, LightningAlt, Question } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";
import { positions } from "@/shared/constants";

export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "site" | "deviceSet" | "explicitMiners";
export type CurtailmentSiteSelection = "none" | "allSites" | "site";
export type CurtailmentMinerSelectionMode = "subset" | "all";
export type ResponseProfileId = string;
export type CurtailmentMode = "fixedKwReduction" | "fullFleet";
export type MinerSelectionStrategy = "leastEfficientFirst";
export type CurtailmentStartModalMode = "create" | "edit";
export type CurtailmentStartModalVariant = "curtailment" | "responseProfile";
export type ResponseProfileModalMode = "create" | "edit";

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
  siteSelection?: CurtailmentSiteSelection;
  siteId?: string;
  siteIds?: string[];
  siteNamesById?: Record<string, string>;
  deviceSetIds: string[];
  deviceIdentifiers: string[];
  minerSelectionMode?: CurtailmentMinerSelectionMode;
  responseProfileId: ResponseProfileId;
  curtailmentMode: CurtailmentMode;
  minerSelectionStrategy: MinerSelectionStrategy;
  targetKw: string;
  toleranceKw: string;
  priority: CurtailmentPriority;
  minDurationSec: string;
  maxDurationSec: string;
  curtailBatchSize: string;
  curtailBatchIntervalSec: string;
  restoreBatchSize: string;
  restoreIntervalSec: string;
  reason: string;
  includeMaintenance: boolean;
  forceIncludeAllPairedMiners: boolean;
}

export type CurtailmentSubmitValues = CurtailmentFormValues;

export interface CurtailmentResponseProfileOption {
  id: ResponseProfileId;
  label: string;
  values: Partial<Omit<CurtailmentFormValues, "responseProfileId">>;
}

export interface CurtailmentSiteOption {
  id: string;
  name: string;
}

export interface CurtailmentPlanPreview {
  selectedMinerCount: number;
  unavailableMinerCount?: number;
  targetKw: number;
  estimatedReductionKw: number;
  curtailEstimate: string;
  restoreEstimate: string;
  scopeLabel: string;
}

export type CurtailmentFormErrors = Partial<Record<keyof CurtailmentFormValues, string>>;

type PendingCurtailmentConfirmation = {
  action: "run" | "test";
  values: CurtailmentSubmitValues;
};

type ForceInclusionFields = Pick<CurtailmentFormValues, "forceIncludeAllPairedMiners">;

interface CurtailmentStartModalProps {
  open: boolean;
  onDismiss: () => void;
  onSubmit: (values: CurtailmentSubmitValues) => void;
  /**
   * Called from edit mode when the operator requests a curtailment stop. The
   * parent owns confirmation and the stop-curtailment RPC.
   */
  onStopCurtailment?: () => void;
  onTestCurtailment?: (values: CurtailmentSubmitValues) => void;
  onDeleteResponseProfile?: () => void;
  mode?: CurtailmentStartModalMode;
  variant?: CurtailmentStartModalVariant;
  responseProfileMode?: ResponseProfileModalMode;
  initialValues?: Partial<CurtailmentFormValues>;
  responseProfiles?: CurtailmentResponseProfileOption[];
  siteOptions?: CurtailmentSiteOption[];
  defaultSiteScope?: CurtailmentSiteOption;
  siteScopeEnabled?: boolean;
  isSiteScopeLoading?: boolean;
  siteScopeDisabledReason?: string;
  errors?: CurtailmentFormErrors;
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  actionError?: string | null;
  isSubmitting?: boolean;
  isTestingCurtailment?: boolean;
  isDeleting?: boolean;
}

interface SectionProps {
  title: string;
  subtext?: string;
  children: ReactNode;
}

interface ReductionProgressBarProps {
  value: number;
  max: number;
}

interface PreviewPaneProps {
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  previewUnavailable?: string;
  isPreviewLoading?: boolean;
}

interface PreviewStateOptions {
  apiPreview: PreviewPaneProps;
  controlledPreview?: PreviewPaneProps;
  isEditMode: boolean;
  unsupportedDeviceSetPreviewError?: string;
}

interface ApplyToTarget {
  label: string;
  value: string;
}

type ParsedNumberField = { parsed?: number; error?: string };
type EditableCurtailmentField = "reason" | "restoreIntervalSec";
type SiteScopeRow = {
  id: string;
  label: string;
  isSelected: boolean;
  disabled?: boolean;
  "data-testid": string;
};

interface SiteScopeOptionProps {
  disabled?: boolean;
  isSelected: boolean;
  label: string;
  onChange: () => void;
  testId: string;
}

export const customResponseProfileId = "customPlan";
const responseProfileDescription = "Saved configurations that define how much power to shed and how to restore it.";
const fieldHelp = {
  curtailmentMode: "How power reduction is measured: fixed kW target or full shutdown.",
  fixedTargetReduction: "The amount to reduce based on the selected mode.",
  curtailBatchSize: "Number of miners to shut down in each wave.",
  curtailBatchInterval: "Seconds to wait between each curtailment wave.",
  restoreBatchSize:
    "Number of miners to bring back online in each wave. 0 or blank restores pending miners up to the safety limit.",
  restoreBatchInterval: "Seconds to wait between each restore wave. 0 or blank means no wait.",
} as const;
const defaultValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  siteSelection: "none",
  siteId: "",
  siteIds: [],
  siteNamesById: {},
  deviceSetIds: [],
  deviceIdentifiers: [],
  minerSelectionMode: "subset",
  responseProfileId: customResponseProfileId,
  // Whole-fleet shutdown is the primary operator flow; fixed-kW sizing is
  // the opt-in refinement (matches the response-profile form default).
  curtailmentMode: "fullFleet",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  reason: "",
  // Maintenance-flagged miners are excluded by default: force_include_maintenance
  // is admin-gated server-side, so sending it from every start would lock
  // non-admin operators with curtailment:manage out of Start entirely. Admins
  // opt the maintenance population in via "Target all paired miners".
  includeMaintenance: false,
  forceIncludeAllPairedMiners: false,
};
const editableCurtailmentFields: EditableCurtailmentField[] = ["reason", "restoreIntervalSec"];
// Full shutdown leads: whole-fleet curtailment is the primary operator flow
// (matches the response-profile default and DeviceSettingsModal ordering).
const curtailmentModeOptions = [
  { value: "fullFleet", label: "Full shutdown" },
  { value: "fixedKwReduction", label: "Fixed kW reduction" },
];
const getSiteScopeRowId = (siteId: string) => `site:${siteId}`;

function uniqueNonEmptyStrings(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

const getValidSiteScopeId = (siteId?: string): string | undefined => {
  const normalizedSiteId = siteId?.trim();
  if (!normalizedSiteId) {
    return undefined;
  }

  const parsedSiteId = parseCurtailmentSiteId(normalizedSiteId);
  return parsedSiteId?.toString() === normalizedSiteId ? normalizedSiteId : undefined;
};

function getValidSiteScopeIds(siteIds?: readonly string[], fallbackSiteId?: string): string[] {
  const normalizedSiteIds = siteIds?.flatMap((siteId) => {
    const validSiteId = getValidSiteScopeId(siteId);
    return validSiteId ? [validSiteId] : [];
  });

  if (normalizedSiteIds !== undefined && normalizedSiteIds.length > 0) {
    return uniqueNonEmptyStrings(normalizedSiteIds);
  }

  const validFallbackSiteId = getValidSiteScopeId(fallbackSiteId);
  return validFallbackSiteId ? [validFallbackSiteId] : [];
}

function getSelectedSiteIds(values: Pick<CurtailmentFormValues, "siteSelection" | "siteId" | "siteIds">): string[] {
  return values.siteSelection === "site" ? getValidSiteScopeIds(values.siteIds, values.siteId) : [];
}

function getSiteScopeIds(values: Pick<CurtailmentFormValues, "siteSelection" | "siteId" | "siteIds">): string[] {
  return values.siteSelection === "site" || values.siteSelection === "allSites"
    ? getValidSiteScopeIds(values.siteIds, values.siteId)
    : [];
}

function getSiteNameForId(values: Partial<CurtailmentFormValues>, siteId: string): string {
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

function hasAllMinersSelected(values: Pick<CurtailmentFormValues, "minerSelectionMode">): boolean {
  return values.minerSelectionMode === "all";
}

function getExplicitMinerCount(
  values: Pick<CurtailmentFormValues, "deviceIdentifiers" | "minerSelectionMode">,
): number {
  return hasAllMinersSelected(values) ? 0 : values.deviceIdentifiers.length;
}

function hasSelectedCurtailmentTarget(
  values: Pick<
    CurtailmentFormValues,
    "deviceIdentifiers" | "deviceSetIds" | "minerSelectionMode" | "siteId" | "siteIds" | "siteSelection"
  >,
): boolean {
  return (
    hasAllMinersSelected(values) ||
    getSiteScopeIds(values).length > 0 ||
    getExplicitMinerCount(values) > 0 ||
    values.deviceSetIds.length > 0
  );
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
    deviceSetIds: [],
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
  };
}

function withAllMinerScope(values: CurtailmentFormValues): CurtailmentFormValues {
  return {
    ...values,
    scopeType: "wholeOrg",
    scopeId: "whole-org",
    siteSelection: "allSites",
    siteId: "",
    siteIds: [],
    siteNamesById: {},
    deviceSetIds: [],
    deviceIdentifiers: [],
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
  const selectedSites = uniqueNonEmptyStrings(sites.map((site) => site.id)).map((siteId) => {
    const site = sites.find((candidate) => candidate.id === siteId);
    return {
      id: siteId,
      name: site?.name?.trim() || `Site ${siteId}`,
    };
  });
  const firstSite = selectedSites[0];
  const hadAllMinersSelected = hasAllMinersSelected(values);
  const hasSelectedMiners = !hadAllMinersSelected && getExplicitMinerCount(values) > 0;

  return {
    ...values,
    scopeType: hasSelectedMiners ? "explicitMiners" : selectedSites.length > 0 ? "site" : "wholeOrg",
    scopeId: selectedSites.length > 0 ? "All sites" : "whole-org",
    siteSelection: "allSites",
    siteId: firstSite?.id ?? "",
    siteIds: selectedSites.map((site) => site.id),
    siteNamesById: createSiteNamesById(selectedSites),
    deviceSetIds: [],
    deviceIdentifiers: hadAllMinersSelected ? [] : values.deviceIdentifiers,
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
    deviceSetIds: [],
  };
}

function withSiteScopes(values: CurtailmentFormValues, sites: readonly CurtailmentSiteOption[]): CurtailmentFormValues {
  const selectedSites = uniqueNonEmptyStrings(sites.map((site) => site.id)).map((siteId) => {
    const site = sites.find((candidate) => candidate.id === siteId);
    return {
      id: siteId,
      name: site?.name?.trim() || `Site ${siteId}`,
    };
  });
  if (selectedSites.length === 0) {
    return withNoSiteScope(values);
  }

  const hadAllMinersSelected = hasAllMinersSelected(values);
  const hasSelectedMiners = !hadAllMinersSelected && getExplicitMinerCount(values) > 0;
  const firstSite = selectedSites[0];

  return {
    ...values,
    scopeType: hasSelectedMiners ? "explicitMiners" : "site",
    scopeId: getSiteScopeLabel(selectedSites),
    siteSelection: "site",
    siteId: firstSite.id,
    siteIds: selectedSites.map((site) => site.id),
    siteNamesById: createSiteNamesById(selectedSites),
    deviceSetIds: [],
    deviceIdentifiers: hadAllMinersSelected ? [] : values.deviceIdentifiers,
    minerSelectionMode: "subset",
  };
}

function withResponseProfileScope(values: CurtailmentFormValues): CurtailmentFormValues {
  const siteIds = getSelectedSiteIds(values);

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

  if (getExplicitMinerCount(values) > 0) {
    return {
      ...values,
      scopeType: "explicitMiners",
      scopeId: undefined,
      siteSelection: "none",
      siteId: "",
      siteIds: [],
      siteNamesById: {},
      deviceSetIds: [],
    };
  }

  return withWholeFleetScope(values);
}

function withDefaultSiteScope(
  values: CurtailmentFormValues,
  defaultSiteScope?: CurtailmentSiteOption,
): CurtailmentFormValues {
  return defaultSiteScope && !hasSelectedCurtailmentTarget(values)
    ? withSiteScopes(values, [defaultSiteScope])
    : values;
}

function hasResponseProfileScopeValues(responseProfileValues: CurtailmentResponseProfileOption["values"]): boolean {
  return (
    "scopeType" in responseProfileValues ||
    "scopeId" in responseProfileValues ||
    "siteSelection" in responseProfileValues ||
    "siteId" in responseProfileValues ||
    "siteIds" in responseProfileValues ||
    "siteNamesById" in responseProfileValues ||
    "deviceSetIds" in responseProfileValues ||
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
  delete behaviorValues.deviceSetIds;
  delete behaviorValues.deviceIdentifiers;
  delete behaviorValues.minerSelectionMode;

  return behaviorValues;
}

function withSelectedResponseProfileValues(
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
  const minerSelectionMode = responseProfileValues.minerSelectionMode ?? "subset";
  const siteSelection = responseProfileValues.siteSelection ?? (siteIds.length > 0 ? "site" : "none");
  const scopeType =
    responseProfileValues.scopeType ??
    (siteSelection === "allSites"
      ? "wholeOrg"
      : siteIds.length > 0
        ? "site"
        : responseProfileValues.deviceSetIds?.length
          ? "deviceSet"
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

  if (scopeType === "deviceSet") {
    return withWholeFleetScope(nextValues);
  }

  if (scopeType === "explicitMiners") {
    return {
      ...nextValues,
      scopeType,
      scopeId: undefined,
      siteSelection: "none",
      siteId: "",
      siteIds: [],
      siteNamesById: {},
      deviceSetIds: [],
      deviceIdentifiers,
      minerSelectionMode,
    };
  }

  return withWholeFleetScope(nextValues);
}

function isCurtailmentMode(value: string): value is CurtailmentMode {
  return value === "fixedKwReduction" || value === "fullFleet";
}

function getForceInclusionConfirmationKey(values: CurtailmentFormValues): string {
  // Maintenance inclusion is no longer surfaced in the UI, so the only user-driven
  // force-inclusion is targeting all paired miners. Mirror the request builders'
  // predicate: a stale flag that the builders will strip (wrong mode or a
  // non-closed-loop scope) must not prompt a force-inclusion confirmation for a
  // request that won't force-include anything.
  return values.forceIncludeAllPairedMiners && supportsAllPairedTargeting(values) ? "all-paired" : "";
}

function getInitialValues(
  initialValues?: Partial<CurtailmentFormValues>,
  variant: CurtailmentStartModalVariant = "curtailment",
  defaultSiteScope?: CurtailmentSiteOption,
): CurtailmentFormValues {
  const values = {
    ...defaultValues,
    ...initialValues,
  };
  if (initialValues?.siteSelection === undefined && getValidSiteScopeIds(values.siteIds, values.siteId).length > 0) {
    values.siteSelection = "site";
  }

  const valuesWithDefaultSiteScope = withDefaultSiteScope(values, defaultSiteScope);

  return variant === "responseProfile"
    ? withResponseProfileScope(valuesWithDefaultSiteScope)
    : valuesWithDefaultSiteScope;
}

function parseRequiredPositiveNumberField(value: string, fieldLabel: string): ParsedNumberField {
  const trimmed = value.trim();
  if (trimmed === "") {
    return { error: `Enter ${fieldLabel}.` };
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return { error: `Enter ${fieldLabel} greater than 0.` };
  }

  return { parsed };
}

function parseOptionalNonNegativeNumberField(value: string, fieldLabel: string): ParsedNumberField {
  const trimmed = value.trim();
  if (trimmed === "") {
    return {};
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return { error: `Enter ${fieldLabel} of 0 or more.` };
  }

  return { parsed };
}

function parseComparableUint32Field(value: string, max: number): number {
  const parsedField = parseOptionalUint32Field(value, { label: "value", max });
  return parsedField.parsed ?? 0;
}

function hasEditableCurtailmentChanges(values: CurtailmentFormValues, initialValues: CurtailmentFormValues): boolean {
  return editableCurtailmentFields.some((field) => {
    if (field === "reason") {
      return values.reason.trim() !== initialValues.reason.trim();
    }

    return (
      parseComparableUint32Field(values.restoreIntervalSec, curtailmentNumericFieldLimits.restoreIntervalSec) !==
      parseComparableUint32Field(initialValues.restoreIntervalSec, curtailmentNumericFieldLimits.restoreIntervalSec)
    );
  });
}

function validateCurtailmentFormValues(
  values: CurtailmentFormValues,
  mode: CurtailmentStartModalMode = "create",
  initialValues: CurtailmentFormValues = defaultValues,
  variant: CurtailmentStartModalVariant = "curtailment",
): CurtailmentFormErrors {
  const localErrors: CurtailmentFormErrors = {};
  const isEditMode = mode === "edit";
  const isResponseProfileVariant = variant === "responseProfile";
  const shouldValidateCurtailBatchFields = !isEditMode || isResponseProfileVariant;
  const restoreInterval = parseOptionalUint32Field(values.restoreIntervalSec, {
    label: "batch interval",
    max: curtailmentNumericFieldLimits.restoreIntervalSec,
  });
  const curtailBatchSize = parseOptionalUint32Field(values.curtailBatchSize, {
    label: "batch size",
    max: curtailmentNumericFieldLimits.curtailBatchSize,
  });
  const curtailBatchInterval = parseOptionalUint32Field(values.curtailBatchIntervalSec, {
    label: "batch interval",
    max: curtailmentNumericFieldLimits.curtailBatchIntervalSec,
  });

  if (values.reason.trim() === "") {
    localErrors.reason = variant === "responseProfile" ? "Enter a profile name." : "Enter a reason.";
  }
  if (restoreInterval.error) {
    localErrors.restoreIntervalSec = restoreInterval.error;
  }
  if (shouldValidateCurtailBatchFields && curtailBatchSize.error) {
    localErrors.curtailBatchSize = curtailBatchSize.error;
  }
  if (shouldValidateCurtailBatchFields && curtailBatchSize.error === undefined && curtailBatchSize.parsed === 0) {
    localErrors.curtailBatchSize = "Enter batch size greater than 0.";
  }
  if (shouldValidateCurtailBatchFields && curtailBatchInterval.error) {
    localErrors.curtailBatchIntervalSec = curtailBatchInterval.error;
  }
  if (
    shouldValidateCurtailBatchFields &&
    curtailBatchInterval.error === undefined &&
    curtailBatchSize.parsed === undefined &&
    curtailBatchInterval.parsed !== undefined
  ) {
    localErrors.curtailBatchIntervalSec = "Enter batch size before adding a batch interval.";
  }
  if (
    isEditMode &&
    restoreInterval.error === undefined &&
    values.restoreIntervalSec.trim() === "" &&
    initialValues.restoreIntervalSec.trim() !== ""
  ) {
    localErrors.restoreIntervalSec = "Restore interval cannot be cleared.";
  }
  if (isEditMode) {
    return localErrors;
  }

  const targetKw =
    values.curtailmentMode === "fixedKwReduction"
      ? parseRequiredPositiveNumberField(values.targetKw, "a target reduction")
      : {};
  const toleranceKw =
    values.curtailmentMode === "fixedKwReduction"
      ? parseOptionalNonNegativeNumberField(values.toleranceKw, "a tolerance")
      : {};
  const restoreBatchSize = parseOptionalUint32Field(values.restoreBatchSize, {
    label: "batch size",
    max: curtailmentNumericFieldLimits.restoreBatchSize,
  });

  if (targetKw.error) {
    localErrors.targetKw = targetKw.error;
  }
  if (toleranceKw.error) {
    localErrors.toleranceKw = toleranceKw.error;
  }
  if (restoreBatchSize.error) {
    localErrors.restoreBatchSize = restoreBatchSize.error;
  }
  return localErrors;
}

function Section({ title, subtext, children }: SectionProps): ReactElement {
  return (
    <section className="grid gap-3">
      <div className="grid">
        <div className="text-emphasis-300 text-text-primary">{title}</div>
        {subtext ? <div className="text-300 text-text-primary-70">{subtext}</div> : null}
      </div>
      {children}
    </section>
  );
}

function SiteScopeOption({
  disabled = false,
  isSelected,
  label,
  onChange,
  testId,
}: SiteScopeOptionProps): ReactElement {
  return (
    <label
      className={`flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left text-300 ${
        disabled
          ? "cursor-not-allowed text-text-primary-50"
          : "hover:bg-surface-base-hover focus-visible:bg-surface-base-hover text-text-primary"
      }`}
      data-testid={testId}
    >
      <Checkbox checked={isSelected} disabled={disabled} onChange={onChange} />
      <span className="min-w-0 truncate">{label}</span>
    </label>
  );
}

interface FieldInfoToggleProps {
  ariaLabel: string;
  body: string;
  testId: string;
  popoverTestId: string;
}

function FieldInfoToggleContent({ ariaLabel, body, testId, popoverTestId }: FieldInfoToggleProps): ReactElement {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  return (
    <div ref={triggerRef} className="relative">
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        data-testid={testId}
        className="flex h-6 w-6 items-center justify-center rounded-full text-text-primary-50 transition-colors hover:text-text-primary-70 focus-visible:ring-2 focus-visible:ring-core-primary-20 focus-visible:outline-hidden"
        onClick={(event) => {
          event.stopPropagation();
          setIsOpen((current) => !current);
        }}
      >
        <Question className="h-4 w-4" />
      </button>
      {isOpen ? (
        <Popover
          position={positions["bottom right"]}
          size={popoverSizes.normal}
          offset={8}
          className="!space-y-0 !rounded-2xl !bg-surface-elevated-base !p-6 !shadow-300 !backdrop-blur-none"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={[`[data-testid='${testId}']`]}
          testId={popoverTestId}
        >
          <p className="text-300 leading-6 text-text-primary-70">{body}</p>
        </Popover>
      ) : null}
    </div>
  );
}

function FieldInfoToggle(props: FieldInfoToggleProps): ReactElement {
  return (
    <PopoverProvider>
      <FieldInfoToggleContent {...props} />
    </PopoverProvider>
  );
}

function clampPercentage(value: number): number {
  return Math.min(Math.max(value, 0), 100);
}

function ReductionProgressBar({ value, max }: ReductionProgressBarProps): ReactElement {
  const reductionPercentage = max > 0 ? clampPercentage((value / max) * 100) : 0;

  return (
    <div className="flex h-4 w-full gap-2 overflow-hidden">
      <div className="rounded-full bg-core-accent-fill" style={{ width: `${reductionPercentage}%` }} />
      <div className="min-w-0 flex-1 rounded-full bg-core-primary-20" />
    </div>
  );
}

function PreviewPane({
  preview,
  previewError,
  previewUnavailable,
  isPreviewLoading = false,
}: PreviewPaneProps): ReactElement {
  if (previewError) {
    return (
      <div className="flex min-h-40 flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-6 py-10 text-300 text-text-primary-70 laptop:px-16">
        <div className="flex max-w-[420px] gap-2">
          <Alert className="mt-0.5 shrink-0 text-text-primary-50" width="w-4" />
          <div>{previewError}</div>
        </div>
      </div>
    );
  }

  if (previewUnavailable) {
    return (
      <div className="flex min-h-40 flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-6 py-10 text-center text-300 text-text-primary-70 laptop:px-16">
        {previewUnavailable}
      </div>
    );
  }

  if (!preview) {
    if (isPreviewLoading) {
      return (
        <div
          className="flex min-h-40 flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-6 py-10 text-text-primary-70 laptop:px-16"
          role="status"
          aria-label="Loading curtailment preview"
        >
          <ProgressCircular indeterminate dataTestId="curtailment-preview-loading" />
        </div>
      );
    }

    return (
      <div className="flex min-h-40 flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-6 py-10 text-center text-300 text-text-primary-70 laptop:px-16">
        Configure your curtailment to see a preview.
      </div>
    );
  }

  return (
    <div className="flex min-h-[360px] flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-8 py-12 laptop:min-h-0 laptop:px-16 laptop:py-6">
      <div className="flex w-full max-w-[620px] flex-col gap-4">
        <div className="grid gap-6">
          <div className="grid gap-1">
            <div className="text-heading-300 text-text-primary">Curtailment target reduction</div>
            <div className="text-heading-300 text-text-primary">
              {formatKw(preview.estimatedReductionKw)} of {formatKw(preview.targetKw)}
            </div>
          </div>
          <ReductionProgressBar value={preview.estimatedReductionKw} max={preview.targetKw} />
        </div>

        <div className="grid gap-2">
          <div className="text-heading-100 text-text-primary">{formatCurtailmentPreviewSummary(preview)}</div>
          <div className="text-heading-100 text-text-primary-50">
            {preview.curtailEstimate} to curtail, {preview.restoreEstimate} to restore
          </div>
          {preview.unavailableMinerCount !== undefined && preview.unavailableMinerCount > 0 ? (
            <div className="text-300 text-text-primary-50">
              {formatCountLabel(preview.unavailableMinerCount, "miner")} currently unavailable
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function getSelectedMinerIds(values: CurtailmentFormValues): string[] {
  return hasAllMinersSelected(values) ? [] : values.deviceIdentifiers;
}

function formatCountLabel(count: number, singular: string): string {
  return getTargetButtonLabel(count, singular);
}

function formatCurtailmentPreviewSummary(preview: CurtailmentPlanPreview): string {
  return `Curtail ${formatCountLabel(preview.selectedMinerCount, "miner").toLowerCase()} ${preview.scopeLabel} immediately`;
}

function formatScopeLabelForSentence(scopeLabel: string): string {
  return scopeLabel === "All sites" ? "all sites" : scopeLabel;
}

function formatCurtailmentConfirmationTarget(values: CurtailmentFormValues, selectedMinerCount?: number): string {
  const selectedSiteIds = getSiteScopeIds(values);
  if (hasAllMinersSelected(values) || (values.siteSelection === "allSites" && selectedSiteIds.length === 0)) {
    return "the whole fleet";
  }

  const selectedMiners =
    getExplicitMinerCount(values) > 0 ? formatCountLabel(values.deviceIdentifiers.length, "miner").toLowerCase() : "";
  const selectedSiteLabel =
    values.siteSelection === "allSites"
      ? "all sites"
      : selectedSiteIds.length === 1
        ? values.scopeId
        : formatCountLabel(selectedSiteIds.length, "site").toLowerCase();
  const selectedSite =
    selectedSiteIds.length > 0
      ? `miners in ${selectedSiteLabel ? formatScopeLabelForSentence(selectedSiteLabel) : "the selected sites"}`
      : "";

  if (selectedMiners && selectedSite) {
    return `${selectedMiners} and ${selectedSite}`;
  }

  if (selectedMiners) {
    return selectedMiners;
  }

  if (values.scopeType === "deviceSet") {
    return `miners in ${formatCountLabel(values.deviceSetIds.length, "device set").toLowerCase()}`;
  }

  if (selectedSite) {
    return selectedSite;
  }

  if (values.curtailmentMode === "fullFleet") {
    return "the whole fleet";
  }

  if (selectedMinerCount !== undefined && selectedMinerCount > 0) {
    return formatCountLabel(selectedMinerCount, "miner").toLowerCase();
  }

  return "miners across the fleet";
}

function getCurtailmentConfirmationCopy(
  pendingConfirmation: PendingCurtailmentConfirmation | null,
  selectedMinerCount?: number,
) {
  if (!pendingConfirmation) {
    return null;
  }

  const target = formatCurtailmentConfirmationTarget(pendingConfirmation.values, selectedMinerCount);
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
function getForceInclusionConfirmationCopy() {
  return {
    title: "Force include all paired miners?",
    body: "This will keep targeting paired miners even when they are offline, sleeping, or waiting for authentication, and includes miners flagged for maintenance.",
    confirmText: "Force include",
  };
}

function getMinerApplyToTarget(values: CurtailmentFormValues): ApplyToTarget {
  return {
    label: "Miners",
    value: hasAllMinersSelected(values)
      ? "All miners"
      : values.deviceIdentifiers.length > 0
        ? formatCountLabel(values.deviceIdentifiers.length, "miner")
        : getTargetButtonLabel(0, "miner"),
  };
}

function getSiteApplyToTarget(values: CurtailmentFormValues): ApplyToTarget {
  const selectedSiteIds = getSelectedSiteIds(values);
  if (selectedSiteIds.length > 0) {
    return {
      label: "Sites",
      value:
        selectedSiteIds.length === 1
          ? (values.scopeId ?? `Site ${selectedSiteIds[0]}`)
          : formatCountLabel(selectedSiteIds.length, "site"),
    };
  }

  if (values.siteSelection === "allSites") {
    return {
      label: "Sites",
      value: "All sites",
    };
  }

  return {
    label: "Sites",
    value: "Select",
  };
}

function getPreviewState({
  apiPreview,
  controlledPreview,
  isEditMode,
  unsupportedDeviceSetPreviewError,
}: PreviewStateOptions): PreviewPaneProps {
  if (isEditMode) {
    return controlledPreview ?? { preview: undefined, previewError: undefined, isPreviewLoading: false };
  }

  if (unsupportedDeviceSetPreviewError) {
    return { preview: undefined, previewError: unsupportedDeviceSetPreviewError, isPreviewLoading: false };
  }

  return controlledPreview ?? apiPreview;
}

function responseProfilePreviewState(previewState: PreviewPaneProps): PreviewPaneProps {
  if (!previewState.previewError) {
    return previewState;
  }

  return {
    preview: undefined,
    previewUnavailable: "Current fleet state is unavailable for preview.",
    isPreviewLoading: previewState.isPreviewLoading,
  };
}

function CurtailmentStartModalContent({
  open,
  onDismiss,
  onSubmit,
  onStopCurtailment,
  onTestCurtailment,
  onDeleteResponseProfile,
  mode = "create",
  variant = "curtailment",
  responseProfileMode = "create",
  initialValues,
  responseProfiles = [],
  siteOptions = [],
  defaultSiteScope,
  siteScopeEnabled = true,
  isSiteScopeLoading = false,
  siteScopeDisabledReason,
  errors,
  preview,
  previewError,
  actionError,
  isSubmitting = false,
  isTestingCurtailment = false,
  isDeleting = false,
}: CurtailmentStartModalProps): ReactElement {
  const [initialFormValues] = useState<CurtailmentFormValues>(() =>
    getInitialValues(initialValues, variant, defaultSiteScope),
  );
  const [values, setValues] = useState<CurtailmentFormValues>(() => initialFormValues);
  const [showForceInclusionConfirmation, setShowForceInclusionConfirmation] = useState(false);
  const [pendingForceInclusionValues, setPendingForceInclusionValues] = useState<Partial<ForceInclusionFields> | null>(
    null,
  );
  const [confirmedForceInclusionKey, setConfirmedForceInclusionKey] = useState("");
  const [submitAfterForceInclusionConfirmation, setSubmitAfterForceInclusionConfirmation] = useState<
    PendingCurtailmentConfirmation["action"] | null
  >(null);
  const [pendingCurtailmentConfirmation, setPendingCurtailmentConfirmation] =
    useState<PendingCurtailmentConfirmation | null>(null);
  const [showMinerSelectionModal, setShowMinerSelectionModal] = useState(false);
  const [showSiteScopeModal, setShowSiteScopeModal] = useState(false);
  const [draftSelectedSiteIds, setDraftSelectedSiteIds] = useState<string[]>([]);
  const [editedFields, setEditedFields] = useState<ReadonlySet<keyof CurtailmentFormValues>>(() => new Set());
  const isEditMode = mode === "edit";
  const isResponseProfileVariant = variant === "responseProfile";
  const isResponseProfileEditMode = isResponseProfileVariant && responseProfileMode === "edit";
  const isLiveCurtailmentEditMode = isEditMode && !isResponseProfileVariant;
  const shouldResetResponseProfileOnEdit = !isResponseProfileVariant && !isEditMode;
  const resetResponseProfileSelection = (nextValues: CurtailmentFormValues): CurtailmentFormValues => {
    if (!shouldResetResponseProfileOnEdit || nextValues.responseProfileId === customResponseProfileId) {
      return nextValues;
    }

    return {
      ...nextValues,
      responseProfileId: customResponseProfileId,
    };
  };
  const updateValue = <Key extends keyof CurtailmentFormValues>(key: Key, value: CurtailmentFormValues[Key]) => {
    setEditedFields((current) => (current.has(key) ? current : new Set(current).add(key)));
    setValues((current) => {
      const nextValues = { ...current, [key]: value };

      return key === "reason" ? nextValues : resetResponseProfileSelection(nextValues);
    });
  };
  const updateCurtailmentMode = (curtailmentMode: CurtailmentMode) => {
    setEditedFields((current) => (current.has("curtailmentMode") ? current : new Set(current).add("curtailmentMode")));
    setValues((current) => {
      const nextValues = {
        ...current,
        curtailmentMode,
        forceIncludeAllPairedMiners: curtailmentMode === "fullFleet" ? current.forceIncludeAllPairedMiners : false,
      };

      return resetResponseProfileSelection(nextValues);
    });
  };
  const updateValues = (
    updater: (current: CurtailmentFormValues) => CurtailmentFormValues,
    options: { resetResponseProfileSelection?: boolean } = {},
  ) =>
    setValues((current) => {
      const nextValues = updater(current);
      return options.resetResponseProfileSelection ? resetResponseProfileSelection(nextValues) : nextValues;
    });
  const validationMode: CurtailmentStartModalMode = isLiveCurtailmentEditMode ? "edit" : "create";
  const isBusy = isSubmitting || isTestingCurtailment || isDeleting;
  const localErrors = useMemo(
    () => validateCurtailmentFormValues(values, validationMode, initialFormValues, variant),
    [initialFormValues, validationMode, values, variant],
  );
  const visibleLocalErrors = useMemo(() => {
    const visibleErrors: CurtailmentFormErrors = {};

    editedFields.forEach((field) => {
      if (localErrors[field] !== undefined) {
        visibleErrors[field] = localErrors[field];
      }
    });

    return visibleErrors;
  }, [editedFields, localErrors]);
  const effectiveErrors = { ...errors, ...visibleLocalErrors };
  const canSelectSiteScope = siteScopeEnabled && !siteScopeDisabledReason;
  const selectedSiteIds = useMemo(() => getSelectedSiteIds(values), [values]);
  const effectiveValues = useMemo(() => {
    if (values.siteSelection === "site" && selectedSiteIds.length > 0) {
      const siteOptionsById = new Map(siteOptions.map((siteOption) => [siteOption.id, siteOption]));
      return withSiteScopes(
        values,
        selectedSiteIds.map((siteId) => {
          const selectedSiteOption = siteOptionsById.get(siteId);
          return {
            id: siteId,
            name: selectedSiteOption?.name ?? getSiteNameForId(values, siteId),
          };
        }),
      );
    }

    return values;
  }, [selectedSiteIds, siteOptions, values]);
  const unsupportedDeviceSetPreviewError = getUnsupportedDeviceSetPreviewError(effectiveValues);
  const controlledPreviewValue = preview
    ? createCurtailmentPlanPreview(effectiveValues, {
        selectedMinerCount: preview.selectedMinerCount,
        unavailableMinerCount: preview.unavailableMinerCount,
        targetKw: preview.targetKw,
        estimatedReductionKw: preview.estimatedReductionKw,
      })
    : undefined;
  const controlledPreview =
    preview !== undefined || previewError !== undefined
      ? { preview: controlledPreviewValue, previewError, isPreviewLoading: false }
      : undefined;
  const apiPreview = useCurtailmentPlanPreview({
    open,
    values: effectiveValues,
    disabled: isLiveCurtailmentEditMode || controlledPreview !== undefined,
  });
  const previewState = getPreviewState({
    apiPreview,
    controlledPreview,
    isEditMode: isLiveCurtailmentEditMode,
    unsupportedDeviceSetPreviewError,
  });

  const hasLocalFormError = Object.keys(localErrors).length > 0;
  const hasExternalFormError = Object.keys(errors ?? {}).length > 0;
  const hasBlockingRunPreviewState =
    previewState.previewError !== undefined || (!isResponseProfileVariant && previewState.isPreviewLoading);
  const hasBlockingSubmitPreviewState = !isResponseProfileVariant && hasBlockingRunPreviewState;
  const hasEditableChanges = !isLiveCurtailmentEditMode || hasEditableCurtailmentChanges(values, initialFormValues);
  const isSubmitDisabled = isBusy || hasBlockingSubmitPreviewState || hasExternalFormError || !hasEditableChanges;
  const displayedPreviewState = isResponseProfileVariant ? responseProfilePreviewState(previewState) : previewState;
  const selectedMinerIds = getSelectedMinerIds(effectiveValues);
  const minerApplyToTarget = getMinerApplyToTarget(effectiveValues);
  const siteApplyToTarget = getSiteApplyToTarget(effectiveValues);
  const isFullFleetMode = values.curtailmentMode === "fullFleet";
  const curtailmentBehaviorSubtext = isLiveCurtailmentEditMode
    ? undefined
    : "Fleet will automatically curtail the least efficient miners first.";
  const curtailmentTargetGridClassName = isFullFleetMode ? "grid gap-3" : "grid gap-3 tablet:grid-cols-2";
  const curtailBatchSizeTestId = isResponseProfileVariant
    ? "response-profile-curtail-batch-size"
    : "curtailment-curtail-batch-size";
  const curtailBatchIntervalTestId = isResponseProfileVariant
    ? "response-profile-curtail-batch-interval"
    : "curtailment-curtail-batch-interval";
  const shouldShowPreviewPane =
    !isLiveCurtailmentEditMode ||
    displayedPreviewState.preview !== undefined ||
    displayedPreviewState.previewError !== undefined ||
    displayedPreviewState.previewUnavailable !== undefined;
  const previewPane = shouldShowPreviewPane ? <PreviewPane {...displayedPreviewState} /> : null;
  const curtailmentConfirmationCopy = getCurtailmentConfirmationCopy(
    pendingCurtailmentConfirmation,
    previewState.preview?.selectedMinerCount,
  );
  const forceInclusionConfirmationCopy = getForceInclusionConfirmationCopy();
  const useSinglePaneLayout = isLiveCurtailmentEditMode && previewPane === null;
  const paneContainerClassName = useSinglePaneLayout
    ? "flex min-h-[calc(100dvh-200px)] w-full flex-1 flex-col laptop:px-10"
    : undefined;
  const primaryPaneClassName = useSinglePaneLayout ? "mx-auto w-full max-w-[720px] laptop:pl-0" : undefined;
  const secondaryPaneClassName = useSinglePaneLayout
    ? "!hidden"
    : "!hidden !bg-transparent laptop:!flex laptop:!pl-0 laptop:!rounded-[24px]";
  const nameFieldId = isResponseProfileVariant ? "response-profile-name" : "curtailment-reason";
  const nameFieldLabel = isResponseProfileVariant ? "Profile name" : "Reason";
  const modalTitle = isResponseProfileVariant
    ? isResponseProfileEditMode
      ? "Edit response profile"
      : "Create response profile"
    : isEditMode
      ? "Manage curtailment"
      : "New curtailment";
  const closeAriaLabel = isResponseProfileVariant
    ? isResponseProfileEditMode
      ? "Close response profile editor"
      : "Close response profile creator"
    : isEditMode
      ? "Close curtailment editor"
      : "Close curtailment planner";
  const primaryButtonText = isResponseProfileVariant ? "Save profile" : isEditMode ? "Save" : "Run curtailment";
  const shouldShowResponseProfileSelector = !isResponseProfileVariant && !isEditMode;
  const scopeSiteOptions = useMemo(() => {
    if (!canSelectSiteScope) {
      return [];
    }

    return siteOptions;
  }, [canSelectSiteScope, siteOptions]);
  const siteScopeOptionById = useMemo(
    () => new Map(scopeSiteOptions.map((siteOption) => [siteOption.id, siteOption])),
    [scopeSiteOptions],
  );
  const selectableSiteIds = useMemo(() => scopeSiteOptions.map((siteOption) => siteOption.id), [scopeSiteOptions]);
  const draftSelectedSiteIdSet = useMemo(() => new Set(draftSelectedSiteIds), [draftSelectedSiteIds]);
  const siteScopeRows = useMemo(() => {
    const siteRows: SiteScopeRow[] = scopeSiteOptions.map((siteOption) => ({
      id: getSiteScopeRowId(siteOption.id),
      label: siteOption.name,
      isSelected: draftSelectedSiteIdSet.has(siteOption.id),
      disabled: !canSelectSiteScope,
      "data-testid": `response-profile-scope-site-${siteOption.id}`,
    }));

    for (const currentSiteId of getSelectedSiteIds(values)) {
      if (siteScopeOptionById.has(currentSiteId)) {
        continue;
      }
      siteRows.push({
        id: getSiteScopeRowId(currentSiteId),
        label: getSiteNameForId(values, currentSiteId),
        isSelected: draftSelectedSiteIdSet.has(currentSiteId),
        disabled: true,
        "data-testid": `response-profile-scope-site-${currentSiteId}`,
      });
    }

    if (siteRows.length === 0 && (isSiteScopeLoading || siteScopeDisabledReason)) {
      siteRows.push({
        id: "site-unavailable",
        label: isSiteScopeLoading ? "Loading sites..." : (siteScopeDisabledReason ?? "Site scope unavailable"),
        isSelected: false,
        disabled: true,
        "data-testid": "response-profile-scope-site-unavailable",
      });
    }

    return siteRows;
  }, [
    canSelectSiteScope,
    draftSelectedSiteIdSet,
    isSiteScopeLoading,
    scopeSiteOptions,
    siteScopeOptionById,
    siteScopeDisabledReason,
    values,
  ]);
  const responseProfileSelectOptions = useMemo(
    () => [
      { value: customResponseProfileId, label: "Custom plan" },
      ...responseProfiles.map((profile) => ({ value: profile.id, label: profile.label })),
    ],
    [responseProfiles],
  );
  const selectedResponseProfileValue = responseProfileSelectOptions.some(
    (option) => option.value === values.responseProfileId,
  )
    ? values.responseProfileId
    : customResponseProfileId;

  const handleResponseProfileChange = (responseProfileId: string) => {
    if (responseProfileId === customResponseProfileId) {
      setValues((current) => ({
        ...current,
        responseProfileId: customResponseProfileId,
      }));
      return;
    }

    const responseProfile = responseProfiles.find((profile) => profile.id === responseProfileId);
    if (!responseProfile) {
      return;
    }

    setEditedFields(new Set());
    setConfirmedForceInclusionKey("");
    setValues((current) => ({
      ...withSelectedResponseProfileValues(current, responseProfile.values),
      responseProfileId: responseProfile.id,
    }));
  };

  const openSiteScopeModal = () => {
    const nextDraftSiteIds =
      effectiveValues.siteSelection === "allSites" ? selectableSiteIds : getSelectedSiteIds(effectiveValues);
    setDraftSelectedSiteIds(nextDraftSiteIds);
    setShowSiteScopeModal(true);
  };

  const handleSiteScopeToggle = (scopeRowId: string) => {
    if (!scopeRowId.startsWith("site:")) {
      return;
    }

    const siteId = scopeRowId.slice("site:".length);
    setDraftSelectedSiteIds((current) => {
      const next = new Set(current);

      if (next.has(siteId)) {
        next.delete(siteId);
      } else {
        next.add(siteId);
      }

      return [...next];
    });
  };

  const handleSaveSiteScope = () => {
    const selectedSiteIdsForSave = getValidSiteScopeIds(draftSelectedSiteIds);
    const allSelectableSitesSelected =
      selectableSiteIds.length > 0 &&
      selectedSiteIdsForSave.length === selectableSiteIds.length &&
      selectedSiteIdsForSave.every((siteId) => siteScopeOptionById.has(siteId));

    updateValues(
      (current) => {
        if (selectedSiteIdsForSave.length === 0) {
          return withNoSiteScope(current);
        }

        if (allSelectableSitesSelected) {
          return withAllSitesScope(
            current,
            selectedSiteIdsForSave.map((siteId) => ({
              id: siteId,
              name: siteScopeOptionById.get(siteId)?.name ?? getSiteNameForId(current, siteId),
            })),
          );
        }

        return withSiteScopes(
          current,
          selectedSiteIdsForSave.map((siteId) => ({
            id: siteId,
            name: siteScopeOptionById.get(siteId)?.name ?? getSiteNameForId(current, siteId),
          })),
        );
      },
      { resetResponseProfileSelection: true },
    );
    setShowSiteScopeModal(false);
  };

  const handleMinerSelection = (selection: MinerSelectionValue) => {
    if (selection.allSelected) {
      updateValues(withAllMinerScope, { resetResponseProfileSelection: true });
      return;
    }

    const deviceIdentifiers = selection.selectedMinerIds;
    const hasSelectedMiners = deviceIdentifiers.length > 0;

    updateValues(
      (current) => {
        const scopedCurrent = current;
        const hasSelectedSite = getSiteScopeIds(scopedCurrent).length > 0;
        return {
          ...scopedCurrent,
          scopeType: hasSelectedMiners ? "explicitMiners" : hasSelectedSite ? "site" : "wholeOrg",
          scopeId: hasSelectedMiners
            ? hasSelectedSite
              ? scopedCurrent.scopeId
              : undefined
            : hasSelectedSite
              ? scopedCurrent.scopeId
              : "whole-org",
          deviceSetIds: [],
          deviceIdentifiers,
          minerSelectionMode: "subset",
        };
      },
      { resetResponseProfileSelection: true },
    );
  };

  const closeForceInclusionConfirmation = () => {
    setSubmitAfterForceInclusionConfirmation(null);
    setPendingForceInclusionValues(null);
    setShowForceInclusionConfirmation(false);
  };

  const closeCurtailmentConfirmation = () => {
    setPendingCurtailmentConfirmation(null);
  };

  const requestCurtailmentConfirmation = (
    action: PendingCurtailmentConfirmation["action"],
    confirmationValues: CurtailmentSubmitValues,
  ) => {
    setPendingCurtailmentConfirmation({ action, values: confirmationValues });
  };

  const showLocalFormErrors = () => {
    setEditedFields(
      (current) => new Set([...current, ...(Object.keys(localErrors) as (keyof CurtailmentFormValues)[])]),
    );
  };

  const requestForceInclusionConfirmation = (
    pendingValues: Partial<ForceInclusionFields>,
    submitAfterConfirmation: PendingCurtailmentConfirmation["action"] | null = null,
  ) => {
    setPendingForceInclusionValues(pendingValues);
    setSubmitAfterForceInclusionConfirmation(submitAfterConfirmation);
    setShowForceInclusionConfirmation(true);
  };

  const requiresForceInclusionConfirmation = (candidateValues: CurtailmentFormValues): boolean => {
    const forceInclusionKey = getForceInclusionConfirmationKey(candidateValues);
    return forceInclusionKey !== "" && forceInclusionKey !== confirmedForceInclusionKey;
  };

  const confirmCurtailmentAction = () => {
    if (!pendingCurtailmentConfirmation) {
      return;
    }

    const { action, values: confirmedValues } = pendingCurtailmentConfirmation;
    setPendingCurtailmentConfirmation(null);

    if (action === "test") {
      onTestCurtailment?.(confirmedValues);
      return;
    }

    onSubmit(confirmedValues);
  };

  const handleSubmit = () => {
    if (isBusy) {
      return;
    }

    if (hasLocalFormError) {
      showLocalFormErrors();
      return;
    }

    if (isSubmitDisabled) {
      return;
    }

    if (!isResponseProfileVariant && !isEditMode && requiresForceInclusionConfirmation(effectiveValues)) {
      requestForceInclusionConfirmation({}, "run");
      return;
    }

    if (!isResponseProfileVariant && !isEditMode) {
      requestCurtailmentConfirmation("run", effectiveValues);
      return;
    }

    onSubmit(effectiveValues);
  };

  const requestResponseProfileCurtailment = () => {
    if (isBusy) {
      return;
    }

    if (hasLocalFormError) {
      showLocalFormErrors();
      return;
    }

    if (hasBlockingRunPreviewState || hasExternalFormError) {
      return;
    }

    if (requiresForceInclusionConfirmation(effectiveValues)) {
      requestForceInclusionConfirmation({}, "test");
      return;
    }

    requestCurtailmentConfirmation("test", effectiveValues);
  };

  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [];

  if (isLiveCurtailmentEditMode && onStopCurtailment) {
    buttons.push({
      text: "Stop curtailment",
      variant: variants.secondaryDanger,
      onClick: onStopCurtailment,
      disabled: isBusy,
    });
  }

  if (isResponseProfileEditMode && onDeleteResponseProfile) {
    buttons.push({
      text: "Delete",
      variant: variants.secondaryDanger,
      onClick: onDeleteResponseProfile,
      disabled: isBusy,
      loading: isDeleting,
    });
  }

  if (isResponseProfileVariant && onTestCurtailment) {
    buttons.push({
      text: "Run curtailment",
      variant: variants.secondary,
      onClick: requestResponseProfileCurtailment,
      disabled: isBusy || hasBlockingRunPreviewState || hasExternalFormError,
      loading: isTestingCurtailment,
    });
  }

  buttons.push({
    text: primaryButtonText,
    variant: variants.primary,
    onClick: handleSubmit,
    disabled: isSubmitDisabled,
    loading: isSubmitting,
  });

  const confirmForceInclusion = () => {
    const nextValues = resetResponseProfileSelection({
      ...effectiveValues,
      ...pendingForceInclusionValues,
    });

    setConfirmedForceInclusionKey(getForceInclusionConfirmationKey(nextValues));
    setValues(nextValues);
    setPendingForceInclusionValues(null);
    setShowForceInclusionConfirmation(false);

    if (submitAfterForceInclusionConfirmation) {
      const pendingAction = submitAfterForceInclusionConfirmation;
      setSubmitAfterForceInclusionConfirmation(null);
      requestCurtailmentConfirmation(pendingAction, nextValues);
    }
  };

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title={modalTitle}
        closeAriaLabel={closeAriaLabel}
        onDismiss={onDismiss}
        isBusy={isBusy}
        buttons={buttons}
        abovePanes={previewPane ? <div className="px-6 pb-6 laptop:hidden">{previewPane}</div> : null}
        primaryPane={
          <section className="flex flex-col gap-12 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
            {actionError ? (
              <div
                className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical"
                data-testid="curtailment-action-error"
              >
                {actionError}
              </div>
            ) : null}
            {isResponseProfileVariant ? (
              <Section title="Profile" subtext={responseProfileDescription}>
                <Input
                  id={nameFieldId}
                  label={nameFieldLabel}
                  initValue={values.reason}
                  type="text"
                  error={effectiveErrors.reason}
                  onChange={(value) => updateValue("reason", value)}
                />
              </Section>
            ) : (
              <div className="grid gap-3">
                {shouldShowResponseProfileSelector ? (
                  <Section title="Response profile">
                    <Select
                      id="curtailment-response-profile"
                      label="Profile"
                      value={selectedResponseProfileValue}
                      options={responseProfileSelectOptions}
                      forceBelow
                      showSelectedIndicator={false}
                      testId="curtailment-response-profile-select"
                      onChange={handleResponseProfileChange}
                    />
                  </Section>
                ) : null}
                <Input
                  id={nameFieldId}
                  label={nameFieldLabel}
                  initValue={values.reason}
                  type="text"
                  error={effectiveErrors.reason}
                  onChange={(value) => updateValue("reason", value)}
                />
              </div>
            )}

            <Section title="Curtail behavior" subtext={curtailmentBehaviorSubtext}>
              <div className="grid gap-3">
                <div className={curtailmentTargetGridClassName}>
                  <Select
                    id="curtailment-mode"
                    label="Curtailment mode"
                    value={values.curtailmentMode}
                    options={curtailmentModeOptions}
                    disabled={isLiveCurtailmentEditMode}
                    forceBelow
                    showSelectedIndicator={false}
                    suffixAction={
                      <FieldInfoToggle
                        ariaLabel="About curtailment mode"
                        body={fieldHelp.curtailmentMode}
                        testId="curtailment-mode-info-button"
                        popoverTestId="curtailment-mode-info-popover"
                      />
                    }
                    onChange={(value) => {
                      if (isCurtailmentMode(value)) {
                        updateCurtailmentMode(value);
                      }
                    }}
                  />
                  {!isFullFleetMode ? (
                    <Input
                      id="curtailment-target-kw"
                      label="Fixed target reduction (kW)"
                      initValue={values.targetKw}
                      disabled={isLiveCurtailmentEditMode}
                      inputMode="decimal"
                      error={effectiveErrors.targetKw}
                      suffixAction={
                        <FieldInfoToggle
                          ariaLabel="About fixed target reduction"
                          body={fieldHelp.fixedTargetReduction}
                          testId="fixed-target-reduction-info-button"
                          popoverTestId="fixed-target-reduction-info-popover"
                        />
                      }
                      onChange={(value) => updateValue("targetKw", value)}
                    />
                  ) : null}
                </div>
                <div className="grid gap-3 tablet:grid-cols-2">
                  <Input
                    id="curtailment-batch-size"
                    label="Batch size (miners)"
                    initValue={values.curtailBatchSize}
                    disabled={isLiveCurtailmentEditMode}
                    inputMode="numeric"
                    error={effectiveErrors.curtailBatchSize}
                    testId={curtailBatchSizeTestId}
                    suffixAction={
                      <FieldInfoToggle
                        ariaLabel="About curtail batch size"
                        body={fieldHelp.curtailBatchSize}
                        testId="curtail-batch-size-info-button"
                        popoverTestId="curtail-batch-size-info-popover"
                      />
                    }
                    onChange={(value) => updateValue("curtailBatchSize", value)}
                  />
                  <Input
                    id="curtailment-batch-interval"
                    label="Batch interval (sec)"
                    initValue={values.curtailBatchIntervalSec}
                    disabled={isLiveCurtailmentEditMode}
                    inputMode="numeric"
                    error={effectiveErrors.curtailBatchIntervalSec}
                    testId={curtailBatchIntervalTestId}
                    suffixAction={
                      <FieldInfoToggle
                        ariaLabel="About curtail batch interval"
                        body={fieldHelp.curtailBatchInterval}
                        testId="curtail-batch-interval-info-button"
                        popoverTestId="curtail-batch-interval-info-popover"
                      />
                    }
                    onChange={(value) => updateValue("curtailBatchIntervalSec", value)}
                  />
                </div>
              </div>
            </Section>

            <Section title="Restore behavior">
              <div className="grid gap-3 tablet:grid-cols-2">
                <Input
                  id="curtailment-restore-batch-size"
                  label="Batch size (miners)"
                  initValue={values.restoreBatchSize}
                  disabled={isLiveCurtailmentEditMode}
                  inputMode="numeric"
                  error={effectiveErrors.restoreBatchSize}
                  testId={
                    isResponseProfileVariant ? "response-profile-restore-batch-size" : "curtailment-restore-batch-size"
                  }
                  suffixAction={
                    <FieldInfoToggle
                      ariaLabel="About restore batch size"
                      body={fieldHelp.restoreBatchSize}
                      testId="restore-batch-size-info-button"
                      popoverTestId="restore-batch-size-info-popover"
                    />
                  }
                  onChange={(value) => updateValue("restoreBatchSize", value)}
                />
                <Input
                  id="curtailment-restore-batch-interval"
                  label="Batch interval (sec)"
                  initValue={values.restoreIntervalSec}
                  inputMode="numeric"
                  error={effectiveErrors.restoreIntervalSec}
                  testId={
                    isResponseProfileVariant
                      ? "response-profile-restore-batch-interval"
                      : "curtailment-restore-batch-interval"
                  }
                  suffixAction={
                    <FieldInfoToggle
                      ariaLabel="About restore batch interval"
                      body={fieldHelp.restoreBatchInterval}
                      testId="restore-batch-interval-info-button"
                      popoverTestId="restore-batch-interval-info-popover"
                    />
                  }
                  onChange={(value) => updateValue("restoreIntervalSec", value)}
                />
              </div>
            </Section>

            <Section
              title="Apply to"
              subtext="Applies to all miners by default. Use the options below to narrow the scope."
            >
              <div className="grid">
                <TargetSelectButton
                  label={siteApplyToTarget.label}
                  value={siteApplyToTarget.value}
                  disabled={isLiveCurtailmentEditMode}
                  onClick={openSiteScopeModal}
                />
                <TargetSelectButton
                  label={minerApplyToTarget.label}
                  value={minerApplyToTarget.value}
                  disabled={isLiveCurtailmentEditMode}
                  onClick={() => setShowMinerSelectionModal(true)}
                />
              </div>
            </Section>

            {/*
              The "Include miners in maintenance" checkbox is intentionally hidden from the UI.
              includeMaintenance defaults to false so non-admin operators with curtailment:manage
              can still start curtailments (force_include_maintenance is admin-gated server-side).
              "Target all paired miners" is the only operator-controllable inclusion option, and
              enabling it also opts in maintenance-flagged miners via the request builders.
              Re-add the checkbox here if maintenance ever needs to become independently togglable.

              The checkbox only renders for closed-loop scopes (whole org / sites): the all-paired
              policy's release/reopen ownership loop does not run for explicit miner selections,
              and the server rejects that combination.
            */}
            {isFullFleetMode && supportsAllPairedTargeting(values) ? (
              <Section title="Miners">
                <label
                  className={`flex items-start gap-3 text-left ${
                    isLiveCurtailmentEditMode ? "cursor-not-allowed" : "cursor-pointer"
                  }`}
                >
                  <Checkbox
                    checked={values.forceIncludeAllPairedMiners}
                    disabled={isLiveCurtailmentEditMode}
                    onChange={(event) => {
                      if (!isResponseProfileVariant && event.currentTarget.checked) {
                        requestForceInclusionConfirmation({ forceIncludeAllPairedMiners: true });
                        return;
                      }

                      setConfirmedForceInclusionKey("");
                      updateValue("forceIncludeAllPairedMiners", event.currentTarget.checked);
                    }}
                  />
                  <span>
                    <span className="block text-300 text-text-primary">Target all paired miners</span>
                  </span>
                </label>
              </Section>
            ) : null}
          </section>
        }
        secondaryPane={previewPane}
        paneContainerClassName={paneContainerClassName}
        primaryPaneClassName={primaryPaneClassName}
        secondaryPaneClassName={secondaryPaneClassName}
      />
      <Dialog
        open={showForceInclusionConfirmation}
        title={forceInclusionConfirmationCopy.title}
        testId="curtailment-force-inclusion-confirmation"
        onDismiss={closeForceInclusionConfirmation}
        icon={
          <DialogIcon intent="warning">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            onClick: closeForceInclusionConfirmation,
            variant: variants.secondary,
          },
          {
            text: forceInclusionConfirmationCopy.confirmText,
            onClick: confirmForceInclusion,
            variant: variants.danger,
          },
        ]}
      >
        <div className="text-300 text-text-primary-70">{forceInclusionConfirmationCopy.body}</div>
      </Dialog>

      <Dialog
        open={pendingCurtailmentConfirmation !== null}
        title={curtailmentConfirmationCopy?.title ?? "Run curtailment?"}
        testId="curtailment-run-confirmation"
        onDismiss={closeCurtailmentConfirmation}
        icon={
          <DialogIcon intent="warning">
            <LightningAlt />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            onClick: closeCurtailmentConfirmation,
            variant: variants.secondary,
            disabled: isBusy,
          },
          {
            text: curtailmentConfirmationCopy?.confirmText ?? "Run curtailment",
            onClick: confirmCurtailmentAction,
            variant: variants.primary,
            disabled: isBusy,
            loading: pendingCurtailmentConfirmation?.action === "test" ? isTestingCurtailment : isSubmitting,
          },
        ]}
      >
        <div className="text-300 text-text-primary-70">{curtailmentConfirmationCopy?.body}</div>
      </Dialog>

      {showMinerSelectionModal ? (
        <MinerSelectionModal
          open={showMinerSelectionModal}
          allMinersSelected={hasAllMinersSelected(effectiveValues)}
          selectedMinerIds={selectedMinerIds}
          onDismiss={() => setShowMinerSelectionModal(false)}
          onSave={(selection) => {
            handleMinerSelection(selection);
            setShowMinerSelectionModal(false);
          }}
        />
      ) : null}

      {showSiteScopeModal ? (
        <Modal
          open={showSiteScopeModal}
          title="Select sites"
          divider={false}
          buttons={[
            {
              text: "Done",
              variant: variants.primary,
              onClick: handleSaveSiteScope,
              dismissModalOnClick: false,
            },
          ]}
          onDismiss={() => setShowSiteScopeModal(false)}
        >
          <div className="flex flex-col">
            {siteScopeRows.map((siteScopeRow) => (
              <SiteScopeOption
                key={siteScopeRow.id}
                disabled={siteScopeRow.disabled}
                isSelected={siteScopeRow.isSelected}
                label={siteScopeRow.label}
                testId={siteScopeRow["data-testid"]}
                onChange={() => handleSiteScopeToggle(siteScopeRow.id)}
              />
            ))}
            {siteScopeRows.some((siteScopeRow) => !siteScopeRow.disabled) ? (
              <ModalSelectAllFooter
                label={
                  selectableSiteIds.length > 0 && draftSelectedSiteIds.length === selectableSiteIds.length
                    ? `All ${selectableSiteIds.length} ${selectableSiteIds.length === 1 ? "site" : "sites"} selected`
                    : `${draftSelectedSiteIds.length} ${draftSelectedSiteIds.length === 1 ? "site" : "sites"} selected`
                }
                onSelectAll={() => setDraftSelectedSiteIds(selectableSiteIds)}
                onSelectNone={() => setDraftSelectedSiteIds([])}
              />
            ) : null}
          </div>
        </Modal>
      ) : null}
    </>
  );
}

function CurtailmentStartModal(props: CurtailmentStartModalProps): ReactElement | null {
  if (!props.open) {
    return null;
  }

  return <CurtailmentStartModalContent {...props} />;
}

export default CurtailmentStartModal;
