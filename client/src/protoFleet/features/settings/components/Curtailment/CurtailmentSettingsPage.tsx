import { type KeyboardEvent, type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import clsx from "clsx";

import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import useCurtailmentAutomationRules from "@/protoFleet/api/useCurtailmentAutomationRules";
import useCurtailmentResponseProfiles, {
  getResponseProfileScopeLabelForActionType,
} from "@/protoFleet/api/useCurtailmentResponseProfiles";
import useInfrastructureDevices from "@/protoFleet/api/useInfrastructureDevices";
import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { immediateRestoreBatchSizeInputValue } from "@/protoFleet/features/energy/curtailmentNumericFields";
import { getDefaultCurtailmentSiteScope } from "@/protoFleet/features/energy/curtailmentSiteScopeDefaults";
import CurtailmentStartModal, {
  type CurtailmentFormValues,
  type CurtailmentSiteOption,
  type CurtailmentSubmitValues,
  type ResponseProfileModalMode,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import type { FacilityFanDeviceOption } from "@/protoFleet/features/energy/FacilityFanSelectionModal";
import CurtailmentAutomationsContent from "@/protoFleet/features/settings/components/Curtailment/CurtailmentAutomations";
import { isInputEnterSaveEvent } from "@/protoFleet/features/settings/components/Curtailment/keyboard";
import {
  type AutomationRule,
  type AutomationRuleFormValues,
  type CurtailmentHealth,
  type CurtailmentSource,
  type CurtailmentSourceFormValues,
  DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC,
  MAX_SOURCE_STALENESS_THRESHOLD_SEC,
  type ResponseProfile,
  type ResponseProfileFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { Alert, Info, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Card, { cardType } from "@/shared/components/Card";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import type { SelectOption } from "@/shared/components/Select";
import Switch from "@/shared/components/Switch";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { classNameToSelectors } from "@/shared/utils/cssUtils";
import "./CurtailmentSettingsPage.css";

const CURTAILMENT_PAGE_DESCRIPTION =
  "Configure response profiles, manage external signal sources, and define automations that trigger curtailment.";
const RESPONSE_PROFILES_DESCRIPTION = "Saved configurations that define how much power to shed and how to restore it.";
const SOURCES_DESCRIPTION = "MaestroOS MQTT brokers that publish curtailment signals.";
const SOURCE_CONNECTION_FAILURE_MESSAGE =
  "We couldn't connect with your source. Review your source details and try again.";
const MAX_BROKER_PORT = 65_535;

const responseProfileSelectionStrategyOptions: SelectOption[] = [
  { value: "leastEfficientFirst", label: "Least efficient first" },
];

const responseProfileRestoreBehaviorOptions: SelectOption[] = [
  { value: "automaticBatchRestore", label: "Restore in batches" },
  { value: "automaticImmediateRestore", label: "Restore immediately" },
];

const responseProfileSelectionStrategyLabel = Object.fromEntries(
  responseProfileSelectionStrategyOptions.map((option) => [option.value, option.label]),
) as Record<ResponseProfileFormValues["selectionStrategy"], string>;

const responseProfileRestoreBehaviorLabel = Object.fromEntries(
  responseProfileRestoreBehaviorOptions.map((option) => [option.value, option.label]),
) as Record<ResponseProfileFormValues["restoreBehavior"], string>;

const curtailmentSourceCols = {
  name: "name",
  lastSignalValue: "lastSignalValue",
  lastSignalUpdate: "lastSignalUpdate",
  health: "health",
  enabled: "enabled",
} as const;

type CurtailmentSourceColumn = (typeof curtailmentSourceCols)[keyof typeof curtailmentSourceCols];

const activeCurtailmentSourceCols: CurtailmentSourceColumn[] = [
  curtailmentSourceCols.name,
  curtailmentSourceCols.lastSignalValue,
  curtailmentSourceCols.lastSignalUpdate,
  curtailmentSourceCols.health,
  curtailmentSourceCols.enabled,
];

const curtailmentSourceColTitles: ColTitles<CurtailmentSourceColumn> = {
  name: "Name",
  lastSignalValue: "Last signal",
  lastSignalUpdate: "Updated",
  health: "Connection",
  enabled: "",
};

const curtailmentSourceColumnAriaLabels: Partial<Record<CurtailmentSourceColumn, string>> = {
  enabled: "Enabled",
};

const curtailmentSourceColumnsExemptFromDisabledStyling = new Set<CurtailmentSourceColumn>([
  curtailmentSourceCols.enabled,
]);

const curtailmentSourcesTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "phone:[&_thead_th:last-child]:w-14",
  "phone:[&_thead_th:last-child>div]:w-14",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:box-border",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:flex",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:justify-end",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:w-14",
].join(" ");

const sourceHealthDotClassName: Record<CurtailmentHealth, string> = {
  connected: "bg-intent-success-fill",
  waitingForSignal: "bg-intent-warning-fill",
  noSignal: "bg-intent-critical-fill",
  offline: "bg-intent-critical-fill",
};

const emptySourceFormValues: CurtailmentSourceFormValues = {
  name: "",
  brokerPrimaryHost: "",
  brokerSecondaryHost: "",
  brokerPort: "",
  topic: "",
  username: "",
  password: "",
  stalenessThresholdSec: DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC.toString(),
};

const emptyCurtailmentSources: CurtailmentSource[] = [];
const emptyResponseProfiles: ResponseProfile[] = [];
const emptyAutomationRules: AutomationRule[] = [];
const emptyUpdatingSourceIds = new Set<string>();
const emptyUpdatingResponseProfileIds = new Set<string>();
const emptyUpdatingAutomationRuleIds = new Set<string>();
const savedPasswordPlaceholder = "......";

type SourceModalMode = "create" | "edit";

const emptyResponseProfileFormValues: ResponseProfileFormValues = {
  name: "",
  actionType: "fullFleet",
  targetKw: "",
  deviceIdentifiers: [],
  minerSelectionMode: "subset",
  siteSelection: "none",
  siteId: "",
  siteName: "",
  siteIds: [],
  siteNamesById: {},
  selectionStrategy: "leastEfficientFirst",
  restoreBehavior: "automaticBatchRestore",
  minDurationSec: "",
  maxDurationSec: "900",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  facilityFanDeviceIds: [],
  fanOffDelaySec: "",
  fanRestoreDelaySec: "",
  responseDeadlineMinutes: "15",
  // Maintenance-flagged miners are excluded by default: force_include_maintenance
  // is admin-gated server-side, so defaulting it on would block non-admin
  // operators with curtailment:manage from saving any profile. "Target all
  // paired miners" opts the maintenance population in (admin-gated anyway).
  includeMaintenance: false,
  forceIncludeAllPairedMiners: false,
};

const sourceInputIds = {
  name: "source-name",
  brokerPrimaryHost: "source-host-primary",
  brokerSecondaryHost: "source-host-backup",
  brokerPort: "source-port",
  topic: "source-topic",
  username: "source-username",
  password: "source-password",
  stalenessThresholdSec: "source-staleness-threshold",
} as const;

const sourceInputIdToFormKey: Record<string, keyof CurtailmentSourceFormValues> = {
  [sourceInputIds.name]: "name",
  [sourceInputIds.brokerPrimaryHost]: "brokerPrimaryHost",
  [sourceInputIds.brokerSecondaryHost]: "brokerSecondaryHost",
  [sourceInputIds.brokerPort]: "brokerPort",
  [sourceInputIds.topic]: "topic",
  [sourceInputIds.username]: "username",
  [sourceInputIds.password]: "password",
  [sourceInputIds.stalenessThresholdSec]: "stalenessThresholdSec",
};

function isPositiveInteger(value: string): boolean {
  return /^[1-9]\d*$/.test(value.trim());
}

function getOptionValueByLabel<TValue extends string>(
  options: SelectOption[],
  label: string,
  fallbackValue: TValue,
): TValue {
  return (options.find((option) => option.label === label)?.value ?? fallbackValue) as TValue;
}

function getResponseProfileTargetSummary(values: ResponseProfileFormValues): string {
  const targetKw = Number(values.targetKw).toLocaleString();

  switch (values.actionType) {
    case "fixedKwReduction":
      return `${targetKw} kW target`;
    case "fullFleet":
    default:
      return "100% reduction";
  }
}

function getResponseProfileDeadlineSummary(values: ResponseProfileFormValues): string {
  const minutes = Number(values.responseDeadlineMinutes);
  return minutes === 1 ? "Within 1 min" : `Within ${minutes} min`;
}

function getResponseProfileScopeSummary(values: ResponseProfileFormValues): string {
  if (values.minerSelectionMode === "all") {
    return getResponseProfileScopeLabelForActionType(values.actionType);
  }

  const siteIds = getSelectedResponseProfileSiteIds(values);
  if (values.siteSelection === "allSites") {
    return "All sites";
  }

  const minerCount = values.deviceIdentifiers.length;
  const siteSummary =
    siteIds.length === 1
      ? getResponseProfileSiteNameForId(values, siteIds[0]) || `Site ${siteIds[0]}`
      : `${siteIds.length} sites`;
  if (values.siteSelection === "site" && siteIds.length > 0 && minerCount > 0) {
    return `${siteSummary} + ${minerCount} ${minerCount === 1 ? "miner" : "miners"}`;
  }

  if (values.siteSelection === "site" && siteIds.length > 0) {
    return siteSummary;
  }

  if (minerCount > 0) {
    return `${minerCount} ${minerCount === 1 ? "miner" : "miners"}`;
  }

  return getResponseProfileScopeLabelForActionType(values.actionType);
}

function uniqueNonEmptyStrings(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function getSelectedResponseProfileSiteIds(
  values: Pick<ResponseProfileFormValues, "siteSelection" | "siteId" | "siteIds">,
): string[] {
  const siteIds = uniqueNonEmptyStrings(
    values.siteIds !== undefined && values.siteIds.length > 0 ? values.siteIds : values.siteId ? [values.siteId] : [],
  );

  return values.siteSelection === "site" ||
    values.siteSelection === "allSites" ||
    (values.siteSelection === undefined && siteIds.length > 0)
    ? siteIds
    : [];
}

function getResponseProfileSiteNameForId(values: Partial<ResponseProfileFormValues>, siteId: string): string {
  return values.siteNamesById?.[siteId]?.trim() || (values.siteId === siteId ? values.siteName?.trim() : "") || "";
}

function getResponseProfileSiteNamesById(
  values: ResponseProfileFormValues,
  siteIds: readonly string[],
): Record<string, string> {
  return Object.fromEntries(
    siteIds.map((siteId) => [siteId, getResponseProfileSiteNameForId(values, siteId) || `Site ${siteId}`]),
  );
}

function createSiteOptions(sites: SiteWithCounts[]): CurtailmentSiteOption[] {
  return sites
    .flatMap((siteWithCounts) => {
      const site = siteWithCounts.site;
      const id = site?.id ? site.id.toString() : "";

      return id ? [{ id, name: site?.name || `Site ${id}` }] : [];
    })
    .sort((left, right) => left.name.localeCompare(right.name, undefined, { sensitivity: "base" }));
}

function secondsToDeadlineMinutes(value: string): string {
  const seconds = Number(value);

  if (!Number.isFinite(seconds) || seconds <= 0) {
    return emptyResponseProfileFormValues.responseDeadlineMinutes;
  }

  return Math.max(Math.ceil(seconds / 60), 1).toString();
}

function minutesToSeconds(value: string): string {
  const minutes = Number(value);

  if (!Number.isFinite(minutes) || minutes <= 0) {
    return emptyResponseProfileFormValues.maxDurationSec;
  }

  return String(minutes * 60);
}

function createResponseProfileId(name: string, existingProfiles: ResponseProfile[]): string {
  const baseSlug =
    name
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "response-profile";
  const existingIds = new Set(existingProfiles.map((profile) => profile.id));

  let candidate = baseSlug;
  let suffix = 2;
  while (existingIds.has(candidate)) {
    candidate = `${baseSlug}-${suffix}`;
    suffix += 1;
  }

  return candidate;
}

function createResponseProfileFromFormValues(
  values: ResponseProfileFormValues,
  existingProfiles: ResponseProfile[],
  existingProfile?: ResponseProfile,
): ResponseProfile {
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteIds = hasAllMinersSelected ? [] : getSelectedResponseProfileSiteIds(values);
  const siteId = siteIds[0] ?? "";
  const normalizedValues: ResponseProfileFormValues = {
    ...values,
    name: values.name.trim(),
    targetKw: values.targetKw.trim(),
    deviceIdentifiers: hasAllMinersSelected ? [] : [...values.deviceIdentifiers],
    minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
    siteSelection: hasAllMinersSelected
      ? "allSites"
      : values.siteSelection === "allSites"
        ? "allSites"
        : siteIds.length > 0
          ? "site"
          : "none",
    siteId,
    siteName: siteId ? getResponseProfileSiteNameForId(values, siteId) : "",
    siteIds,
    siteNamesById: getResponseProfileSiteNamesById(values, siteIds),
    minDurationSec: values.minDurationSec.trim(),
    maxDurationSec: values.maxDurationSec.trim(),
    curtailBatchSize: values.curtailBatchSize.trim(),
    curtailBatchIntervalSec: values.curtailBatchIntervalSec.trim(),
    restoreBatchSize: values.restoreBatchSize.trim(),
    restoreIntervalSec: values.restoreIntervalSec.trim(),
    facilityFanDeviceIds: [...new Set(values.facilityFanDeviceIds ?? [])],
    fanOffDelaySec: values.fanOffDelaySec?.trim() ?? "",
    fanRestoreDelaySec: values.fanRestoreDelaySec?.trim() ?? "",
    responseDeadlineMinutes: values.responseDeadlineMinutes.trim(),
  };

  return {
    id: existingProfile?.id ?? createResponseProfileId(normalizedValues.name, existingProfiles),
    name: normalizedValues.name,
    targetSummary: getResponseProfileTargetSummary(normalizedValues),
    scope: getResponseProfileScopeSummary(normalizedValues),
    selectionStrategy: responseProfileSelectionStrategyLabel[normalizedValues.selectionStrategy],
    restoreBehavior: responseProfileRestoreBehaviorLabel[normalizedValues.restoreBehavior],
    deadlineSummary: getResponseProfileDeadlineSummary(normalizedValues),
    formValues: normalizedValues,
  };
}

function removeResponseProfileScope(values: ResponseProfileFormValues): ResponseProfileFormValues {
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteIds = hasAllMinersSelected ? [] : getSelectedResponseProfileSiteIds(values);
  const siteId = siteIds[0] ?? "";

  return {
    ...values,
    deviceIdentifiers: hasAllMinersSelected ? [] : [...values.deviceIdentifiers],
    minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
    siteSelection: hasAllMinersSelected
      ? "allSites"
      : values.siteSelection === "allSites"
        ? "allSites"
        : siteIds.length > 0
          ? "site"
          : "none",
    siteId,
    siteName: siteId ? getResponseProfileSiteNameForId(values, siteId) : "",
    siteIds,
    siteNamesById: getResponseProfileSiteNamesById(values, siteIds),
  };
}

function createResponseProfileFormValuesFromProfile(profile: ResponseProfile): ResponseProfileFormValues {
  if (profile.formValues) {
    return removeResponseProfileScope(profile.formValues);
  }

  const targetKwMatch = profile.targetSummary.match(/(\d+(?:\.\d+)?)/);
  const actionType = targetKwMatch ? "fixedKwReduction" : "fullFleet";

  return {
    name: profile.name,
    actionType,
    targetKw: targetKwMatch?.[1] ?? "",
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
    siteSelection: "none",
    siteId: "",
    siteName: "",
    siteIds: [],
    siteNamesById: {},
    selectionStrategy: getOptionValueByLabel(
      responseProfileSelectionStrategyOptions,
      profile.selectionStrategy,
      emptyResponseProfileFormValues.selectionStrategy,
    ),
    restoreBehavior: getOptionValueByLabel(
      responseProfileRestoreBehaviorOptions,
      profile.restoreBehavior,
      emptyResponseProfileFormValues.restoreBehavior,
    ),
    minDurationSec: emptyResponseProfileFormValues.minDurationSec,
    maxDurationSec: minutesToSeconds(
      profile.deadlineSummary.match(/(\d+)/)?.[1] ?? emptyResponseProfileFormValues.responseDeadlineMinutes,
    ),
    curtailBatchSize: emptyResponseProfileFormValues.curtailBatchSize,
    curtailBatchIntervalSec: emptyResponseProfileFormValues.curtailBatchIntervalSec,
    restoreBatchSize:
      profile.restoreBehavior === responseProfileRestoreBehaviorLabel.automaticImmediateRestore
        ? immediateRestoreBatchSizeInputValue
        : emptyResponseProfileFormValues.restoreBatchSize,
    restoreIntervalSec: emptyResponseProfileFormValues.restoreIntervalSec,
    facilityFanDeviceIds: emptyResponseProfileFormValues.facilityFanDeviceIds,
    fanOffDelaySec: emptyResponseProfileFormValues.fanOffDelaySec,
    fanRestoreDelaySec: emptyResponseProfileFormValues.fanRestoreDelaySec,
    responseDeadlineMinutes:
      profile.deadlineSummary.match(/(\d+)/)?.[1] ?? emptyResponseProfileFormValues.responseDeadlineMinutes,
    includeMaintenance: emptyResponseProfileFormValues.includeMaintenance,
    forceIncludeAllPairedMiners: emptyResponseProfileFormValues.forceIncludeAllPairedMiners,
  };
}

function createCurtailmentFormValuesFromResponseProfile(
  values: ResponseProfileFormValues,
): Partial<CurtailmentFormValues> {
  const restoreBatchSize =
    values.restoreBatchSize ||
    (values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSizeInputValue : "");
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteIds = hasAllMinersSelected ? [] : getSelectedResponseProfileSiteIds(values);
  const siteId = siteIds[0] ?? "";
  const siteNamesById = getResponseProfileSiteNamesById(values, siteIds);
  const siteName = siteId ? siteNamesById[siteId] || `Site ${siteId}` : "";
  const deviceIdentifiers = hasAllMinersSelected ? [] : [...values.deviceIdentifiers];
  const siteSelection = hasAllMinersSelected
    ? "allSites"
    : values.siteSelection === "allSites"
      ? "allSites"
      : siteIds.length > 0
        ? "site"
        : "none";

  return {
    scopeType: hasAllMinersSelected
      ? "wholeOrg"
      : deviceIdentifiers.length > 0
        ? "explicitMiners"
        : siteIds.length > 0
          ? "site"
          : "wholeOrg",
    scopeId: hasAllMinersSelected
      ? "whole-org"
      : siteIds.length > 0
        ? siteIds.length === 1
          ? siteName
          : `${siteIds.length} sites`
        : deviceIdentifiers.length > 0
          ? undefined
          : "whole-org",
    siteSelection,
    siteId,
    siteIds,
    siteNamesById,
    deviceSetIds: [],
    deviceIdentifiers,
    minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
    responseProfileId: "customPlan",
    curtailmentMode: values.actionType,
    minerSelectionStrategy: values.selectionStrategy,
    targetKw: values.targetKw,
    minDurationSec: values.minDurationSec,
    maxDurationSec: values.maxDurationSec || minutesToSeconds(values.responseDeadlineMinutes),
    curtailBatchSize: values.curtailBatchSize,
    curtailBatchIntervalSec: values.curtailBatchIntervalSec,
    restoreBatchSize,
    restoreIntervalSec: values.restoreIntervalSec,
    facilityFanDeviceIds: [...(values.facilityFanDeviceIds ?? [])],
    fanOffDelaySec: values.fanOffDelaySec ?? "",
    fanRestoreDelaySec: values.fanRestoreDelaySec ?? "",
    reason: values.name,
    includeMaintenance: values.includeMaintenance,
    forceIncludeAllPairedMiners: values.actionType === "fullFleet" && Boolean(values.forceIncludeAllPairedMiners),
  };
}

function getResponseProfileRestoreBehavior(
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

function createResponseProfileFormValuesFromCurtailmentValues(
  values: CurtailmentSubmitValues,
): ResponseProfileFormValues {
  const hasAllMinersSelected = values.minerSelectionMode === "all";
  const siteIds =
    hasAllMinersSelected || (values.siteSelection !== "site" && values.siteSelection !== "allSites")
      ? []
      : uniqueNonEmptyStrings(values.siteIds ?? (values.siteId ? [values.siteId] : []));
  const siteId = siteIds[0] ?? "";
  const siteNamesById = Object.fromEntries(
    siteIds.map((currentSiteId) => [
      currentSiteId,
      values.siteNamesById?.[currentSiteId] ??
        (values.siteId === currentSiteId ? values.scopeId : undefined) ??
        `Site ${currentSiteId}`,
    ]),
  );
  const siteName = siteId ? siteNamesById[siteId] : "";
  const deviceIdentifiers = hasAllMinersSelected ? [] : [...values.deviceIdentifiers];

  return {
    name: values.reason,
    actionType: values.curtailmentMode,
    targetKw: values.targetKw,
    deviceIdentifiers,
    minerSelectionMode: hasAllMinersSelected ? "all" : "subset",
    siteSelection: hasAllMinersSelected ? "allSites" : values.siteSelection,
    siteId: hasAllMinersSelected ? "" : siteId,
    siteName: hasAllMinersSelected ? "" : siteName,
    siteIds: hasAllMinersSelected ? [] : siteIds,
    siteNamesById: hasAllMinersSelected ? {} : siteNamesById,
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

function createSourceFormValuesFromSource(source: CurtailmentSource): CurtailmentSourceFormValues {
  return {
    name: source.name,
    brokerPrimaryHost: source.brokerPrimaryHost ?? source.brokerHosts[0] ?? "",
    brokerSecondaryHost: source.brokerSecondaryHost ?? source.brokerHosts[1] ?? "",
    brokerPort: source.port.toString(),
    topic: source.topic,
    username: source.username,
    password: "",
    stalenessThresholdSec: (source.stalenessThresholdSec || DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC).toString(),
  };
}

function applySourceFormValues(source: CurtailmentSource, values: CurtailmentSourceFormValues): CurtailmentSource {
  const brokerPrimaryHost = values.brokerPrimaryHost.trim();
  const brokerSecondaryHost = values.brokerSecondaryHost.trim();

  return {
    ...source,
    name: values.name.trim(),
    brokerHosts: [brokerPrimaryHost, brokerSecondaryHost].filter(Boolean),
    brokerPrimaryHost,
    brokerSecondaryHost,
    port: Number(values.brokerPort),
    topic: values.topic.trim(),
    username: values.username.trim(),
    stalenessThresholdSec: Number(values.stalenessThresholdSec),
  };
}

function sourceCredentialFieldsChanged(
  values: CurtailmentSourceFormValues,
  initialValues: CurtailmentSourceFormValues,
): boolean {
  return (
    values.brokerPrimaryHost.trim() !== initialValues.brokerPrimaryHost.trim() ||
    values.brokerSecondaryHost.trim() !== initialValues.brokerSecondaryHost.trim() ||
    values.brokerPort.trim() !== initialValues.brokerPort.trim() ||
    values.username.trim() !== initialValues.username.trim()
  );
}

function validateSourceFormValues(values: CurtailmentSourceFormValues, passwordRequired: boolean): SourceFormErrors {
  const errors: SourceFormErrors = {};

  if (values.name.trim() === "") {
    errors.name = "Enter a configuration name.";
  }
  if (values.brokerPrimaryHost.trim() === "") {
    errors.brokerPrimaryHost = "Enter broker host 1.";
  }
  if (values.brokerSecondaryHost.trim() === "") {
    errors.brokerSecondaryHost = "Enter broker host 2.";
  }
  if (values.topic.trim() === "") {
    errors.topic = "Enter a topic.";
  }
  if (values.username.trim() === "") {
    errors.username = "Enter a username.";
  }
  if (passwordRequired && values.password === "") {
    errors.password = "Enter a password.";
  }
  if (values.brokerPort.trim() === "") {
    errors.brokerPort = "Enter a port.";
  } else if (!isPositiveInteger(values.brokerPort)) {
    errors.brokerPort = "Enter port as a whole number greater than 0.";
  } else if (Number(values.brokerPort) > MAX_BROKER_PORT) {
    errors.brokerPort = `Enter port of ${MAX_BROKER_PORT.toLocaleString()} or less.`;
  }
  if (values.stalenessThresholdSec.trim() === "") {
    errors.stalenessThresholdSec = "Enter a timeout.";
  } else if (!isPositiveInteger(values.stalenessThresholdSec)) {
    errors.stalenessThresholdSec = "Enter timeout as a whole number greater than 0.";
  } else if (Number(values.stalenessThresholdSec) > MAX_SOURCE_STALENESS_THRESHOLD_SEC) {
    errors.stalenessThresholdSec = `Enter timeout of ${MAX_SOURCE_STALENESS_THRESHOLD_SEC.toLocaleString()} seconds or less.`;
  }

  return errors;
}

function getErrorMessage(error: unknown, fallbackMessage: string): string {
  return error instanceof Error && error.message ? error.message : fallbackMessage;
}

const sourceHealthLabel: Record<CurtailmentHealth, string> = {
  connected: "Connected",
  waitingForSignal: "Waiting for signal",
  noSignal: "No signal",
  offline: "Offline",
};

function formatSourceHealth(health: CurtailmentSource["health"]): string {
  return sourceHealthLabel[health];
}

type InfoToggleContentProps = {
  ariaLabel: string;
  description: string;
  testId: string;
  triggerClassName: string;
};

function InfoToggleContent({ ariaLabel, description, testId, triggerClassName }: InfoToggleContentProps): ReactElement {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();
  const closeIgnoreSelectors = classNameToSelectors(triggerClassName);

  return (
    <div ref={triggerRef} className={`${triggerClassName} relative`}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup
        ariaExpanded={isOpen}
        ariaLabel={ariaLabel}
        prefixIcon={<Info width={iconSizes.small} className="text-text-primary-70" />}
        onClick={() => setIsOpen((current) => !current)}
      />
      {isOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.normal}
          offset={8}
          className="!space-y-0"
          closePopover={() => setIsOpen(false)}
          closeIgnoreSelectors={closeIgnoreSelectors}
          testId={testId}
        >
          <p className="text-300 text-text-primary-70">{description}</p>
        </Popover>
      ) : null}
    </div>
  );
}

function ResponseProfilesInfoToggle(): ReactElement {
  return (
    <PopoverProvider>
      <InfoToggleContent
        ariaLabel="About response profiles"
        description={RESPONSE_PROFILES_DESCRIPTION}
        testId="curtailment-response-profiles-info-popover"
        triggerClassName="curtailment-response-profiles-info-trigger"
      />
    </PopoverProvider>
  );
}

function SourcesInfoToggle(): ReactElement {
  return (
    <PopoverProvider>
      <InfoToggleContent
        ariaLabel="About sources"
        description={SOURCES_DESCRIPTION}
        testId="curtailment-sources-info-popover"
        triggerClassName="curtailment-sources-info-trigger"
      />
    </PopoverProvider>
  );
}

function SourcesEmptyState(): ReactElement {
  return (
    <SettingsEmptyState
      size="section"
      title="No sources configured"
      description="Add a MaestroOS MQTT source to receive curtailment signals."
    />
  );
}

function SourcesLoadingState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full items-center justify-center py-14">
      <ProgressCircular indeterminate dataTestId="curtailment-sources-loading" />
    </div>
  );
}

type SourcesErrorStateProps = {
  message: string;
};

function SourcesErrorState({ message }: SourcesErrorStateProps): ReactElement {
  return <SettingsEmptyState size="section" title="Unable to load sources" description={message} />;
}

function ResponseProfilesEmptyState(): ReactElement {
  return (
    <SettingsEmptyState
      size="section"
      title="No response profiles configured"
      description="Add a profile to reuse curtailment actions across automation rules."
    />
  );
}

function ResponseProfilesLoadingState(): ReactElement {
  return (
    <div className="flex min-h-[220px] w-full items-center justify-center py-14">
      <ProgressCircular indeterminate dataTestId="curtailment-response-profiles-loading" />
    </div>
  );
}

type ResponseProfilesErrorStateProps = {
  message: string;
};

function ResponseProfilesErrorState({ message }: ResponseProfilesErrorStateProps): ReactElement {
  return <SettingsEmptyState size="section" title="Unable to load response profiles" description={message} />;
}

type ResponseProfileCardProps = {
  profile: ResponseProfile;
  onEdit: (profile: ResponseProfile) => void;
};

function ResponseProfileCard({ profile, onEdit }: ResponseProfileCardProps): ReactElement {
  return (
    <Card
      title={<span data-testid="response-profile-name">{profile.name}</span>}
      type={cardType.default}
      className="curtailment-response-profile-card bg-surface-elevated-base shadow-100"
      headerTone="neutral"
      headerClassName="items-start bg-surface-elevated-base px-6 pt-6 pb-0"
      titleClassName="truncate text-emphasis-300 leading-5 font-semibold text-text-primary"
      bodyClassName="px-6 pt-0 pb-1"
      testId="response-profile-card"
      headerAction={
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Edit"
          className="!h-8 !px-3 !py-0"
          onClick={() => onEdit(profile)}
          testId={`response-profile-edit-${profile.id}`}
        />
      }
    >
      <div className="space-y-0 text-300 leading-[18px] text-text-primary-50">
        <p className="truncate">{profile.targetSummary}</p>
        <p className="truncate">{profile.scope}</p>
      </div>
    </Card>
  );
}

type ResponseProfileCardsProps = {
  profiles: ResponseProfile[];
  isLoading?: boolean;
  loadError?: string | null;
  onEdit: (profile: ResponseProfile) => void;
};

function ResponseProfileCards({
  profiles,
  isLoading = false,
  loadError = null,
  onEdit,
}: ResponseProfileCardsProps): ReactElement {
  if (loadError) {
    return <ResponseProfilesErrorState message={loadError} />;
  }

  if (isLoading) {
    return <ResponseProfilesLoadingState />;
  }

  if (profiles.length === 0) {
    return <ResponseProfilesEmptyState />;
  }

  return (
    <div className="curtailment-response-profile-grid" data-testid="response-profile-card-grid">
      {profiles.map((profile) => (
        <ResponseProfileCard key={profile.id} profile={profile} onEdit={onEdit} />
      ))}
    </div>
  );
}

type SourceModalProps = {
  open: boolean;
  mode?: SourceModalMode;
  initialValues?: CurtailmentSourceFormValues;
  hasSavedPassword?: boolean;
  onDismiss: () => void;
  onSave?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onTestConnection?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onDelete?: () => Promise<void>;
  saving?: boolean;
  testingConnection?: boolean;
  deleting?: boolean;
};

type SourceFormErrors = Partial<Record<keyof CurtailmentSourceFormValues, string>>;
type SourceValidationIntent = "save" | "testConnection";

function SourceModal({
  open,
  mode = "create",
  initialValues = emptySourceFormValues,
  hasSavedPassword = false,
  onDismiss,
  onSave,
  onTestConnection,
  onDelete,
  saving = false,
  testingConnection = false,
  deleting = false,
}: SourceModalProps): ReactElement {
  const [values, setValues] = useState<CurtailmentSourceFormValues>(() => initialValues);
  const [passwordPlaceholderActive, setPasswordPlaceholderActive] = useState(() => mode === "edit" && hasSavedPassword);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [validationIntent, setValidationIntent] = useState<SourceValidationIntent | null>(null);
  const [showConnectionCallout, setShowConnectionCallout] = useState(false);
  const [connectionError, setConnectionError] = useState(false);
  const isEditMode = mode === "edit";
  const isBusy = saving || deleting || testingConnection;
  const passwordRequired = !isEditMode || sourceCredentialFieldsChanged(values, initialValues);
  const saveValidationErrors = useMemo(
    () => validateSourceFormValues(values, passwordRequired),
    [passwordRequired, values],
  );
  const testConnectionValidationErrors = useMemo(() => validateSourceFormValues(values, true), [values]);
  const visibleValidationErrors =
    validationIntent === "testConnection"
      ? testConnectionValidationErrors
      : validationIntent === "save"
        ? saveValidationErrors
        : {};
  const canSave = Object.keys(saveValidationErrors).length === 0;
  const canTestConnection = Object.keys(testConnectionValidationErrors).length === 0;
  const showSavedPasswordPlaceholder = isEditMode && hasSavedPassword && passwordPlaceholderActive;
  const passwordInputValue = showSavedPasswordPlaceholder ? savedPasswordPlaceholder : values.password;
  const showConnectionSuccessCallout = showConnectionCallout && !testingConnection && !connectionError;
  const showConnectionFailureCallout = showConnectionCallout && !testingConnection && connectionError;

  const updateSourceValue = useCallback((value: string, id: string) => {
    const formKey = sourceInputIdToFormKey[id];
    if (!formKey) {
      return;
    }

    setValues((currentValues) => ({
      ...currentValues,
      [formKey]: value,
    }));
    setShowConnectionCallout(false);
  }, []);

  const handlePasswordFocus = useCallback(() => {
    if (!showSavedPasswordPlaceholder) {
      return;
    }

    setPasswordPlaceholderActive(false);
    setValues((currentValues) => ({
      ...currentValues,
      password: "",
    }));
  }, [showSavedPasswordPlaceholder]);

  const handleSave = useCallback(async () => {
    if (isBusy) {
      return;
    }

    if (!canSave) {
      if (showSavedPasswordPlaceholder && saveValidationErrors.password) {
        setPasswordPlaceholderActive(false);
      }
      setValidationIntent("save");
      return;
    }

    try {
      setSaveError(null);
      await onSave?.(values);
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to save source."));
    }
  }, [canSave, isBusy, onDismiss, onSave, saveValidationErrors, showSavedPasswordPlaceholder, values]);

  const handleFormKeyDown = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (!isInputEnterSaveEvent(event)) {
        return;
      }

      event.preventDefault();
      void handleSave();
    },
    [handleSave],
  );

  const handleTestConnection = useCallback(async () => {
    if (isBusy || !onTestConnection) {
      return;
    }

    if (!canTestConnection) {
      if (showSavedPasswordPlaceholder && testConnectionValidationErrors.password) {
        setPasswordPlaceholderActive(false);
      }
      setValidationIntent("testConnection");
      return;
    }

    try {
      setSaveError(null);
      setConnectionError(false);
      await onTestConnection(values);
      setConnectionError(false);
    } catch {
      setConnectionError(true);
    } finally {
      setShowConnectionCallout(true);
    }
  }, [
    canTestConnection,
    isBusy,
    onTestConnection,
    showSavedPasswordPlaceholder,
    testConnectionValidationErrors,
    values,
  ]);

  const handleDelete = useCallback(async () => {
    if (!onDelete || isBusy) {
      return;
    }

    try {
      setSaveError(null);
      await onDelete();
      onDismiss();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to delete source."));
    }
  }, [isBusy, onDelete, onDismiss]);

  return (
    <Modal
      open={open}
      title={isEditMode ? "Edit source" : "Add source"}
      description={SOURCES_DESCRIPTION}
      onDismiss={onDismiss}
      size={modalSizes.standard}
      divider={false}
      testId="curtailment-source-modal"
      buttons={[
        ...(isEditMode && onDelete
          ? [
              {
                text: "Delete",
                variant: variants.secondaryDanger,
                disabled: isBusy,
                loading: deleting,
                dismissModalOnClick: false,
                onClick: () => void handleDelete(),
              },
            ]
          : []),
        {
          text: "Test connection",
          variant: variants.secondary,
          className: "whitespace-nowrap overflow-clip",
          testId: "curtailment-source-test-connection-button",
          disabled: isBusy || !onTestConnection,
          loading: testingConnection,
          dismissModalOnClick: false,
          onClick: () => void handleTestConnection(),
        },
        {
          text: "Save",
          variant: variants.primary,
          disabled: isBusy,
          loading: saving,
          dismissModalOnClick: false,
          onClick: () => void handleSave(),
        },
      ]}
      bodyClassName="text-text-primary"
    >
      <div className="grid gap-3 pb-2" onKeyDown={handleFormKeyDown}>
        <DismissibleCalloutWrapper
          icon={<Success />}
          intent={intents.success}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showConnectionSuccessCallout}
          title="Source connection successful"
          testId="curtailment-source-connected-callout"
        />
        <DismissibleCalloutWrapper
          icon={<Alert width={iconSizes.medium} />}
          intent={intents.danger}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showConnectionFailureCallout}
          title={SOURCE_CONNECTION_FAILURE_MESSAGE}
          testId="curtailment-source-not-connected-callout"
        />
        {saveError ? (
          <div className="rounded-lg bg-intent-critical-10 px-4 py-3 text-300 text-text-critical">{saveError}</div>
        ) : null}
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.name}
            label="Configuration name"
            initValue={values.name}
            error={visibleValidationErrors.name}
            onChange={updateSourceValue}
          />
          <Input id="source-type" label="Integration" initValue="MaestroOS" disabled />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPrimaryHost}
            label="Broker host 1"
            initValue={values.brokerPrimaryHost}
            error={visibleValidationErrors.brokerPrimaryHost}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.brokerSecondaryHost}
            label="Broker host 2"
            initValue={values.brokerSecondaryHost}
            error={visibleValidationErrors.brokerSecondaryHost}
            onChange={updateSourceValue}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.brokerPort}
            label="Port"
            type="number"
            inputMode="numeric"
            initValue={values.brokerPort}
            error={visibleValidationErrors.brokerPort}
            onChange={updateSourceValue}
            tooltip={{
              body: "Default MQTT port for MaestroOS is 1883.",
              position: positions["top right"],
              widthClassName: "w-72",
            }}
          />
          <Input
            id={sourceInputIds.topic}
            label="Topic"
            initValue={values.topic}
            error={visibleValidationErrors.topic}
            onChange={updateSourceValue}
            tooltip={{
              body: "The MQTT topic to subscribe to on MaestroOS for curtailment signals.",
              widthClassName: "w-72",
            }}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.username}
            label="Username"
            initValue={values.username}
            error={visibleValidationErrors.username}
            onChange={updateSourceValue}
          />
          <Input
            id={sourceInputIds.password}
            label="Password"
            type="password"
            initValue={passwordInputValue}
            error={visibleValidationErrors.password}
            onChange={updateSourceValue}
            onFocus={handlePasswordFocus}
            hidePasswordToggle={showSavedPasswordPlaceholder}
          />
        </div>
        <div className="grid gap-4 laptop:grid-cols-2">
          <Input
            id={sourceInputIds.stalenessThresholdSec}
            label="No signal timeout"
            type="number"
            inputMode="numeric"
            initValue={values.stalenessThresholdSec}
            error={visibleValidationErrors.stalenessThresholdSec}
            onChange={updateSourceValue}
            units="sec"
            tooltip={{
              body: "When no MQTT signal is received for this duration, the source is treated as OFF.",
              widthClassName: "w-72",
            }}
          />
        </div>
      </div>
    </Modal>
  );
}

type SectionHeaderProps = {
  title: string;
  buttonText: string;
  onButtonClick: () => void;
  infoToggle?: ReactElement;
};

function SectionHeader({ title, buttonText, onButtonClick, infoToggle }: SectionHeaderProps): ReactElement {
  return (
    <div className="curtailment-section-header">
      <div className="curtailment-section-header__title">
        <h2 className="curtailment-section-header__label">{title}</h2>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {infoToggle}
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text={buttonText}
          onClick={onButtonClick}
          className="curtailment-settings__action-button"
        />
      </div>
    </div>
  );
}

type CurtailmentSourceColConfigOptions = {
  onToggle: (sourceId: string) => void;
  updatingSourceIds: ReadonlySet<string>;
};

function createCurtailmentSourceColConfig({
  onToggle,
  updatingSourceIds,
}: CurtailmentSourceColConfigOptions): ColConfig<CurtailmentSource, string, CurtailmentSourceColumn> {
  return {
    [curtailmentSourceCols.name]: {
      component: (source) => (
        <span className="block max-w-full truncate text-emphasis-300 text-text-primary">{source.name}</span>
      ),
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.lastSignalValue]: {
      component: (source) => <span className="truncate text-text-primary">{source.lastTarget}</span>,
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.lastSignalUpdate]: {
      component: (source) => <span className="truncate text-text-primary">{source.lastSeen}</span>,
      width: "w-[23.5%] phone:hidden",
    },
    [curtailmentSourceCols.health]: {
      component: (source) => (
        <div className="inline-flex items-center gap-1.5">
          <span
            className={clsx(
              "curtailment-source-health-dot h-2 w-2 shrink-0 rounded-full",
              sourceHealthDotClassName[source.health],
              source.health === "connected" && "curtailment-source-health-dot--connected",
            )}
          />
          <span className="truncate text-text-primary">{formatSourceHealth(source.health)}</span>
        </div>
      ),
      width: "w-[23.5%] phone:w-auto",
    },
    [curtailmentSourceCols.enabled]: {
      component: (source) => (
        <div className="flex justify-end" data-interactive>
          <Switch
            checked={source.enabled}
            setChecked={() => onToggle(source.id)}
            disabled={updatingSourceIds.has(source.id)}
          />
        </div>
      ),
      width: "w-[6%] phone:w-14",
    },
  };
}

type CurtailmentSettingsContentProps = {
  initialResponseProfiles?: ResponseProfile[];
  initialResponseProfileModalOpen?: boolean;
  initialSources?: CurtailmentSource[];
  initialSourceModalOpen?: boolean;
  initialAutomationRules?: AutomationRule[];
  responseProfiles?: ResponseProfile[];
  sources?: CurtailmentSource[];
  automationRules?: AutomationRule[];
  isLoadingResponseProfiles?: boolean;
  loadResponseProfilesError?: string | null;
  isLoadingSources?: boolean;
  loadSourcesError?: string | null;
  isLoadingAutomationRules?: boolean;
  loadAutomationRulesError?: string | null;
  isSavingResponseProfile?: boolean;
  isTestingResponseProfileCurtailment?: boolean;
  isDeletingResponseProfile?: boolean;
  isSavingSource?: boolean;
  isTestingSourceConnection?: boolean;
  isSavingAutomationRule?: boolean;
  updatingResponseProfileIds?: ReadonlySet<string>;
  updatingSourceIds?: ReadonlySet<string>;
  updatingAutomationRuleIds?: ReadonlySet<string>;
  siteOptions?: CurtailmentSiteOption[];
  defaultResponseProfileSiteScope?: CurtailmentSiteOption;
  isLoadingSiteOptions?: boolean;
  siteScopeDisabledReason?: string;
  infrastructureDevices?: FacilityFanDeviceOption[];
  isLoadingInfrastructureDevices?: boolean;
  infrastructureDevicesError?: string | null;
  onRetryInfrastructureDevices?: () => void;
  onResponseProfileModalOpen?: () => void;
  onCreateResponseProfile?: (values: ResponseProfileFormValues) => Promise<ResponseProfile | void>;
  onUpdateResponseProfile?: (
    profile: ResponseProfile,
    values: ResponseProfileFormValues,
  ) => Promise<ResponseProfile | void>;
  onTestResponseProfileCurtailment?: (
    values: ResponseProfileFormValues,
    curtailmentValues: CurtailmentSubmitValues,
  ) => Promise<void>;
  onDeleteResponseProfile?: (profile: ResponseProfile) => Promise<void>;
  onCreateSource?: (values: CurtailmentSourceFormValues) => Promise<CurtailmentSource | void>;
  onUpdateSource?: (
    source: CurtailmentSource,
    values: CurtailmentSourceFormValues,
  ) => Promise<CurtailmentSource | void>;
  onTestSourceConnection?: (values: CurtailmentSourceFormValues) => Promise<void>;
  onToggleSource?: (source: CurtailmentSource, enabled: boolean) => Promise<CurtailmentSource | void>;
  onDeleteSource?: (source: CurtailmentSource) => Promise<void>;
  onCreateAutomation?: (values: AutomationRuleFormValues) => Promise<AutomationRule | void>;
  onUpdateAutomation?: (rule: AutomationRule, values: AutomationRuleFormValues) => Promise<AutomationRule | void>;
  onToggleAutomation?: (rule: AutomationRule, enabled: boolean) => Promise<AutomationRule | void>;
  onDeleteAutomation?: (rule: AutomationRule) => Promise<void>;
};

function getSourcesEmptyState(loadSourcesError: string | null, isLoadingSources: boolean): ReactElement {
  if (loadSourcesError) {
    return <SourcesErrorState message={loadSourcesError} />;
  }

  if (isLoadingSources) {
    return <SourcesLoadingState />;
  }

  return <SourcesEmptyState />;
}

export function CurtailmentSettingsContent({
  initialResponseProfiles = emptyResponseProfiles,
  initialResponseProfileModalOpen = false,
  initialSources = emptyCurtailmentSources,
  initialSourceModalOpen = false,
  initialAutomationRules = emptyAutomationRules,
  responseProfiles: controlledResponseProfiles,
  sources: controlledSources,
  automationRules: controlledAutomationRules,
  isLoadingResponseProfiles = false,
  loadResponseProfilesError = null,
  isLoadingSources = false,
  loadSourcesError = null,
  isLoadingAutomationRules = false,
  loadAutomationRulesError = null,
  isSavingResponseProfile = false,
  isTestingResponseProfileCurtailment = false,
  isDeletingResponseProfile = false,
  isSavingSource = false,
  isTestingSourceConnection = false,
  isSavingAutomationRule = false,
  updatingResponseProfileIds = emptyUpdatingResponseProfileIds,
  updatingSourceIds = emptyUpdatingSourceIds,
  updatingAutomationRuleIds = emptyUpdatingAutomationRuleIds,
  siteOptions = [],
  defaultResponseProfileSiteScope,
  isLoadingSiteOptions = false,
  siteScopeDisabledReason,
  infrastructureDevices = [],
  isLoadingInfrastructureDevices = false,
  infrastructureDevicesError = null,
  onRetryInfrastructureDevices,
  onResponseProfileModalOpen,
  onCreateResponseProfile,
  onUpdateResponseProfile,
  onTestResponseProfileCurtailment,
  onDeleteResponseProfile,
  onCreateSource,
  onUpdateSource,
  onTestSourceConnection,
  onToggleSource,
  onDeleteSource,
  onCreateAutomation,
  onUpdateAutomation,
  onToggleAutomation,
  onDeleteAutomation,
}: CurtailmentSettingsContentProps): ReactElement {
  const [localResponseProfiles, setLocalResponseProfiles] = useState<ResponseProfile[]>(() => [
    ...initialResponseProfiles,
  ]);
  const [isResponseProfileModalOpen, setIsResponseProfileModalOpen] = useState(initialResponseProfileModalOpen);
  const [editingResponseProfile, setEditingResponseProfile] = useState<ResponseProfile | null>(null);
  const [responseProfileActionError, setResponseProfileActionError] = useState<string | null>(null);
  const [localSources, setLocalSources] = useState<CurtailmentSource[]>(() => [...initialSources]);
  const [isSourceModalOpen, setIsSourceModalOpen] = useState(initialSourceModalOpen);
  const [editingSource, setEditingSource] = useState<CurtailmentSource | null>(null);
  const responseProfiles = controlledResponseProfiles ?? localResponseProfiles;
  const sources = controlledSources ?? localSources;
  const responseProfileModalMode: ResponseProfileModalMode = editingResponseProfile ? "edit" : "create";
  const responseProfileModalInitialValues = useMemo(
    () =>
      editingResponseProfile
        ? createResponseProfileFormValuesFromProfile(editingResponseProfile)
        : emptyResponseProfileFormValues,
    [editingResponseProfile],
  );
  const responseProfileCurtailmentInitialValues = useMemo(
    () => createCurtailmentFormValuesFromResponseProfile(responseProfileModalInitialValues),
    [responseProfileModalInitialValues],
  );
  const sourceModalMode: SourceModalMode = editingSource ? "edit" : "create";
  const sourceModalInitialValues = useMemo(
    () => (editingSource ? createSourceFormValuesFromSource(editingSource) : emptySourceFormValues),
    [editingSource],
  );
  const isEditingResponseProfile = editingResponseProfile
    ? updatingResponseProfileIds.has(editingResponseProfile.id)
    : false;
  const isEditingSource = editingSource ? updatingSourceIds.has(editingSource.id) : false;

  const openCreateResponseProfileModal = useCallback(() => {
    onResponseProfileModalOpen?.();
    setResponseProfileActionError(null);
    setEditingResponseProfile(null);
    setIsResponseProfileModalOpen(true);
  }, [onResponseProfileModalOpen]);

  const openEditResponseProfileModal = useCallback(
    (profile: ResponseProfile) => {
      onResponseProfileModalOpen?.();
      setResponseProfileActionError(null);
      setEditingResponseProfile(profile);
      setIsResponseProfileModalOpen(true);
    },
    [onResponseProfileModalOpen],
  );

  const closeResponseProfileModal = useCallback(() => {
    setResponseProfileActionError(null);
    setIsResponseProfileModalOpen(false);
    setEditingResponseProfile(null);
  }, []);

  const openCreateSourceModal = useCallback(() => {
    setEditingSource(null);
    setIsSourceModalOpen(true);
  }, []);

  const openEditSourceModal = useCallback((source: CurtailmentSource) => {
    setEditingSource(source);
    setIsSourceModalOpen(true);
  }, []);

  const closeSourceModal = useCallback(() => {
    setIsSourceModalOpen(false);
    setEditingSource(null);
  }, []);

  const handleCreateResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      const createdProfile = await onCreateResponseProfile?.(values);
      if (!controlledResponseProfiles) {
        setLocalResponseProfiles((currentProfiles) => {
          const profile = createdProfile ?? createResponseProfileFromFormValues(values, currentProfiles);
          return [...currentProfiles.filter((currentProfile) => currentProfile.id !== profile.id), profile];
        });
      }
    },
    [controlledResponseProfiles, onCreateResponseProfile],
  );

  const handleSaveResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      if (!editingResponseProfile) {
        await handleCreateResponseProfile(values);
        return;
      }

      const updatedProfile =
        (await onUpdateResponseProfile?.(editingResponseProfile, values)) ??
        createResponseProfileFromFormValues(values, responseProfiles, editingResponseProfile);
      if (!controlledResponseProfiles) {
        setLocalResponseProfiles((currentProfiles) =>
          currentProfiles.map((currentProfile) =>
            currentProfile.id === updatedProfile.id ? updatedProfile : currentProfile,
          ),
        );
      }
    },
    [
      controlledResponseProfiles,
      editingResponseProfile,
      handleCreateResponseProfile,
      onUpdateResponseProfile,
      responseProfiles,
    ],
  );

  const handleSaveResponseProfileFromCurtailment = useCallback(
    async (values: CurtailmentSubmitValues) => {
      await handleSaveResponseProfile(createResponseProfileFormValuesFromCurtailmentValues(values));
      closeResponseProfileModal();
    },
    [closeResponseProfileModal, handleSaveResponseProfile],
  );

  const handleTestResponseProfileCurtailmentFromCurtailment = useCallback(
    async (values: CurtailmentSubmitValues) => {
      const responseProfileValues = createResponseProfileFormValuesFromCurtailmentValues(values);

      await handleSaveResponseProfile(responseProfileValues);
      await onTestResponseProfileCurtailment?.(responseProfileValues, values);
      closeResponseProfileModal();
    },
    [closeResponseProfileModal, handleSaveResponseProfile, onTestResponseProfileCurtailment],
  );

  const handleDeleteResponseProfile = useCallback(async () => {
    if (!editingResponseProfile) {
      return;
    }

    await onDeleteResponseProfile?.(editingResponseProfile);
    if (!controlledResponseProfiles) {
      setLocalResponseProfiles((currentProfiles) =>
        currentProfiles.filter((currentProfile) => currentProfile.id !== editingResponseProfile.id),
      );
    }
  }, [controlledResponseProfiles, editingResponseProfile, onDeleteResponseProfile]);

  const handleDeleteResponseProfileFromCurtailment = useCallback(async () => {
    await handleDeleteResponseProfile();
    closeResponseProfileModal();
  }, [closeResponseProfileModal, handleDeleteResponseProfile]);

  const handleResponseProfileModalSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      setResponseProfileActionError(null);
      void handleSaveResponseProfileFromCurtailment(values).catch((error) => {
        setResponseProfileActionError(getErrorMessage(error, "Failed to save response profile."));
      });
    },
    [handleSaveResponseProfileFromCurtailment],
  );

  const handleResponseProfileModalTestCurtailment = useCallback(
    (values: CurtailmentSubmitValues) => {
      setResponseProfileActionError(null);
      void handleTestResponseProfileCurtailmentFromCurtailment(values).catch((error) => {
        setResponseProfileActionError(getErrorMessage(error, "Failed to run curtailment."));
      });
    },
    [handleTestResponseProfileCurtailmentFromCurtailment],
  );

  const handleResponseProfileModalDelete = useCallback(() => {
    setResponseProfileActionError(null);
    void handleDeleteResponseProfileFromCurtailment().catch((error) => {
      setResponseProfileActionError(getErrorMessage(error, "Failed to delete response profile."));
    });
  }, [handleDeleteResponseProfileFromCurtailment]);

  const toggleSource = useCallback(
    (sourceId: string) => {
      const source = sources.find((currentSource) => currentSource.id === sourceId);
      if (!source) {
        return;
      }

      const nextEnabled = !source.enabled;
      if (onToggleSource) {
        void onToggleSource(source, nextEnabled).catch(() => {});
        return;
      }

      setLocalSources((currentSources) =>
        currentSources.map((currentSource) =>
          currentSource.id === sourceId ? { ...currentSource, enabled: nextEnabled } : currentSource,
        ),
      );
    },
    [onToggleSource, sources],
  );

  const handleCreateSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      const createdSource = await onCreateSource?.(values);
      if (!controlledSources && createdSource) {
        setLocalSources((currentSources) => [
          ...currentSources.filter((currentSource) => currentSource.id !== createdSource.id),
          createdSource,
        ]);
      }
    },
    [controlledSources, onCreateSource],
  );

  const handleSaveSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      if (!editingSource) {
        await handleCreateSource(values);
        return;
      }

      const updatedSource =
        (await onUpdateSource?.(editingSource, values)) ?? applySourceFormValues(editingSource, values);
      if (!controlledSources) {
        setLocalSources((currentSources) =>
          currentSources.map((currentSource) =>
            currentSource.id === updatedSource.id ? updatedSource : currentSource,
          ),
        );
      }
    },
    [controlledSources, editingSource, handleCreateSource, onUpdateSource],
  );

  const handleDeleteSource = useCallback(async () => {
    if (!editingSource) {
      return;
    }

    await onDeleteSource?.(editingSource);
    if (!controlledSources) {
      setLocalSources((currentSources) =>
        currentSources.filter((currentSource) => currentSource.id !== editingSource.id),
      );
    }
  }, [controlledSources, editingSource, onDeleteSource]);

  const sourceColConfig = useMemo(
    () =>
      createCurtailmentSourceColConfig({
        onToggle: toggleSource,
        updatingSourceIds,
      }),
    [toggleSource, updatingSourceIds],
  );

  const noDataElement = getSourcesEmptyState(loadSourcesError, isLoadingSources);

  return (
    <div className="flex flex-col gap-14" data-testid="settings-curtailment-page">
      <SettingsPageHeader title="Curtailment" description={CURTAILMENT_PAGE_DESCRIPTION} />

      <section className="curtailment-settings__section">
        <SectionHeader
          title="Response profiles"
          buttonText="Create profile"
          onButtonClick={openCreateResponseProfileModal}
          infoToggle={<ResponseProfilesInfoToggle />}
        />
        <ResponseProfileCards
          profiles={responseProfiles}
          isLoading={isLoadingResponseProfiles}
          loadError={loadResponseProfilesError}
          onEdit={openEditResponseProfileModal}
        />
      </section>

      <section className="curtailment-settings__section">
        <SectionHeader
          title="Sources"
          buttonText="Add source"
          onButtonClick={openCreateSourceModal}
          infoToggle={<SourcesInfoToggle />}
        />
        <List<CurtailmentSource, string, CurtailmentSourceColumn>
          activeCols={activeCurtailmentSourceCols}
          colTitles={curtailmentSourceColTitles}
          columnHeaderAriaLabels={curtailmentSourceColumnAriaLabels}
          colConfig={sourceColConfig}
          items={sources}
          itemKey="id"
          total={sources.length}
          hideTotal
          itemName={{ singular: "source", plural: "sources" }}
          stickyFirstColumn={false}
          isRowDisabled={(source) => !source.enabled}
          columnsExemptFromDisabledStyling={curtailmentSourceColumnsExemptFromDisabledStyling}
          tableClassName={curtailmentSourcesTableClassName}
          noDataElement={noDataElement}
          applyColumnWidthsToCells
          onRowClick={openEditSourceModal}
        />
      </section>

      <CurtailmentAutomationsContent
        initialAutomationRules={initialAutomationRules}
        automationRules={controlledAutomationRules}
        sources={sources}
        responseProfiles={responseProfiles}
        isLoading={isLoadingAutomationRules}
        loadError={loadAutomationRulesError}
        isCreating={isSavingAutomationRule}
        updatingRuleIds={updatingAutomationRuleIds}
        isLoadingSources={isLoadingSources}
        loadSourcesError={loadSourcesError}
        isLoadingResponseProfiles={isLoadingResponseProfiles}
        loadResponseProfilesError={loadResponseProfilesError}
        onCreateAutomation={onCreateAutomation}
        onUpdateAutomation={onUpdateAutomation}
        onToggleAutomation={onToggleAutomation}
        onDeleteAutomation={onDeleteAutomation}
      />

      <CurtailmentStartModal
        key={
          isResponseProfileModalOpen
            ? `response-profile-${responseProfileModalMode}-${editingResponseProfile?.id ?? "new"}`
            : "response-profile-modal-closed"
        }
        open={isResponseProfileModalOpen}
        variant="responseProfile"
        responseProfileMode={responseProfileModalMode}
        initialValues={responseProfileCurtailmentInitialValues}
        siteOptions={siteOptions}
        defaultSiteScope={responseProfileModalMode === "create" ? defaultResponseProfileSiteScope : undefined}
        siteScopeEnabled={siteOptions.length > 0 || isLoadingSiteOptions}
        isSiteScopeLoading={isLoadingSiteOptions}
        siteScopeDisabledReason={siteScopeDisabledReason}
        infrastructureDevices={infrastructureDevices}
        isLoadingInfrastructureDevices={isLoadingInfrastructureDevices}
        infrastructureDevicesError={infrastructureDevicesError}
        onRetryInfrastructureDevices={onRetryInfrastructureDevices}
        actionError={responseProfileActionError}
        onDismiss={closeResponseProfileModal}
        onSubmit={handleResponseProfileModalSubmit}
        onTestCurtailment={onTestResponseProfileCurtailment ? handleResponseProfileModalTestCurtailment : undefined}
        onDeleteResponseProfile={editingResponseProfile ? handleResponseProfileModalDelete : undefined}
        isSubmitting={editingResponseProfile ? isEditingResponseProfile : isSavingResponseProfile}
        isTestingCurtailment={isTestingResponseProfileCurtailment}
        isDeleting={editingResponseProfile ? isDeletingResponseProfile || isEditingResponseProfile : false}
      />

      <SourceModal
        key={isSourceModalOpen ? `source-modal-${editingSource?.id ?? "new"}` : "source-modal-closed"}
        open={isSourceModalOpen}
        mode={sourceModalMode}
        initialValues={sourceModalInitialValues}
        hasSavedPassword={editingSource?.hasPassword ?? false}
        onDismiss={closeSourceModal}
        onSave={handleSaveSource}
        onTestConnection={onTestSourceConnection}
        onDelete={editingSource ? handleDeleteSource : undefined}
        saving={editingSource ? isEditingSource : isSavingSource}
        testingConnection={isTestingSourceConnection}
        deleting={isEditingSource}
      />
    </div>
  );
}

function CurtailmentSettingsPage(): ReactElement {
  const canManageCurtailment = useHasPermission("curtailment:manage");
  const canReadSiteCatalog = useHasPermission("site:read");
  const navigate = useNavigate();
  const { activeSite } = useActiveSite({});
  const { listSites } = useSites();
  const { startCurtailment } = useCurtailmentApi();
  const [isTestingResponseProfileCurtailment, setIsTestingResponseProfileCurtailment] = useState(false);
  const [siteOptions, setSiteOptions] = useState<CurtailmentSiteOption[]>([]);
  const [isLoadingSiteOptions, setIsLoadingSiteOptions] = useState(false);
  const [hasLoadedSiteOptions, setHasLoadedSiteOptions] = useState(false);
  const [siteOptionsLoadError, setSiteOptionsLoadError] = useState<string | null>(null);
  const siteOptionsAbortControllerRef = useRef<AbortController | null>(null);
  const canLoadSiteOptions = canManageCurtailment && canReadSiteCatalog;
  const siteNameById = useMemo(() => {
    if (!canLoadSiteOptions) {
      return undefined;
    }

    return new Map(siteOptions.map(({ id, name }) => [id, name]));
  }, [canLoadSiteOptions, siteOptions]);
  const {
    responseProfiles,
    isLoading: isLoadingResponseProfiles,
    isCreating: isCreatingResponseProfile,
    updatingProfileIds,
    loadError: responseProfilesLoadError,
    createResponseProfile,
    updateResponseProfile,
    deleteResponseProfile,
  } = useCurtailmentResponseProfiles(canManageCurtailment, { siteNameById });
  const {
    sources,
    isLoading,
    isCreating,
    updatingSourceIds,
    loadError,
    createSource,
    updateSource,
    testConnection,
    isTestingConnection,
    setSourceEnabled,
    deleteSource,
  } = useMqttCurtailmentSources(canManageCurtailment);
  const {
    automationRules,
    isLoading: isLoadingAutomationRules,
    isCreating: isCreatingAutomationRule,
    updatingRuleIds: updatingAutomationRuleIds,
    loadError: automationRulesLoadError,
    createAutomationRule,
    updateAutomationRule,
    setAutomationRuleEnabled,
    deleteAutomationRule,
  } = useCurtailmentAutomationRules(canManageCurtailment);
  const {
    devices: infrastructureDevices,
    isLoading: isLoadingInfrastructureDevices,
    loadError: infrastructureDevicesError,
    listDevices: listInfrastructureDevices,
  } = useInfrastructureDevices(canLoadSiteOptions, undefined, true);
  const retryInfrastructureDevices = useCallback(() => {
    void listInfrastructureDevices().catch(() => {});
  }, [listInfrastructureDevices]);

  const ensureSiteOptionsLoaded = useCallback(() => {
    if (!canLoadSiteOptions || isLoadingSiteOptions || (hasLoadedSiteOptions && !siteOptionsLoadError)) {
      return;
    }

    siteOptionsAbortControllerRef.current?.abort();
    const abortController = new AbortController();
    siteOptionsAbortControllerRef.current = abortController;
    const { signal } = abortController;

    setIsLoadingSiteOptions(true);
    setSiteOptionsLoadError(null);
    void listSites({
      signal,
      onSuccess: (sites) => {
        if (!signal.aborted) {
          setSiteOptions(createSiteOptions(sites));
          setHasLoadedSiteOptions(true);
        }
      },
      onError: (message) => {
        if (!signal.aborted) {
          setSiteOptionsLoadError(message);
        }
      },
      onFinally: () => {
        if (!signal.aborted) {
          setIsLoadingSiteOptions(false);
        }
      },
    });
  }, [canLoadSiteOptions, hasLoadedSiteOptions, isLoadingSiteOptions, listSites, siteOptionsLoadError]);

  useEffect(() => {
    return () => {
      siteOptionsAbortControllerRef.current?.abort();
    };
  }, []);

  const hasSiteScopedResponseProfiles = useMemo(
    () =>
      responseProfiles.some(
        (profile) => getSelectedResponseProfileSiteIds(profile.formValues ?? emptyResponseProfileFormValues).length > 0,
      ),
    [responseProfiles],
  );

  useEffect(() => {
    if (!hasSiteScopedResponseProfiles || siteOptionsLoadError) {
      return undefined;
    }

    const timeoutId = window.setTimeout(ensureSiteOptionsLoaded, 0);
    return () => window.clearTimeout(timeoutId);
  }, [ensureSiteOptionsLoaded, hasSiteScopedResponseProfiles, siteOptionsLoadError]);

  useEffect(() => {
    if (!loadError) {
      return;
    }

    pushToast({
      message: loadError,
      status: STATUSES.error,
    });
  }, [loadError]);

  useEffect(() => {
    if (!responseProfilesLoadError) {
      return;
    }

    pushToast({
      message: responseProfilesLoadError,
      status: STATUSES.error,
    });
  }, [responseProfilesLoadError]);

  useEffect(() => {
    if (!automationRulesLoadError) {
      return;
    }

    pushToast({
      message: automationRulesLoadError,
      status: STATUSES.error,
    });
  }, [automationRulesLoadError]);

  const handleCreateResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      const profile = await createResponseProfile(values);
      pushToast({
        message: "Response profile added",
        status: STATUSES.success,
      });
      return profile;
    },
    [createResponseProfile],
  );

  const handleUpdateResponseProfile = useCallback(
    async (profile: ResponseProfile, values: ResponseProfileFormValues) => {
      const updatedProfile = await updateResponseProfile(profile.id, values);
      pushToast({
        message: "Response profile saved",
        status: STATUSES.success,
      });
      return updatedProfile;
    },
    [updateResponseProfile],
  );

  const handleTestResponseProfileCurtailment = useCallback(
    async (_values: ResponseProfileFormValues, curtailmentValues: CurtailmentSubmitValues) => {
      setIsTestingResponseProfileCurtailment(true);

      try {
        await startCurtailment(curtailmentValues);
        setIsTestingResponseProfileCurtailment(false);
        navigate(scopedPath("/energy", useFleetStore.getState().ui.activeSite));
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to run curtailment."),
          status: STATUSES.error,
        });
        setIsTestingResponseProfileCurtailment(false);
        throw error;
      }
    },
    [navigate, startCurtailment],
  );

  const handleDeleteResponseProfile = useCallback(
    async (profile: ResponseProfile) => {
      await deleteResponseProfile(profile.id);
      pushToast({
        message: "Response profile deleted",
        status: STATUSES.success,
      });
    },
    [deleteResponseProfile],
  );

  const handleCreateSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      const source = await createSource(values);
      pushToast({
        message: "Source added",
        status: STATUSES.success,
      });
      return source;
    },
    [createSource],
  );

  const handleToggleSource = useCallback(
    async (source: CurtailmentSource, enabled: boolean) => {
      try {
        return await setSourceEnabled(source.id, enabled);
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update source."),
          status: STATUSES.error,
        });
        throw error;
      }
    },
    [setSourceEnabled],
  );

  const handleUpdateSource = useCallback(
    async (source: CurtailmentSource, values: CurtailmentSourceFormValues) => {
      const updatedSource = await updateSource(source.id, values);
      pushToast({
        message: "Source saved",
        status: STATUSES.success,
      });
      return updatedSource;
    },
    [updateSource],
  );

  const handleTestSourceConnection = useCallback(
    async (values: CurtailmentSourceFormValues) => {
      await testConnection(values);
    },
    [testConnection],
  );

  const handleDeleteSource = useCallback(
    async (source: CurtailmentSource) => {
      await deleteSource(source.id);
      pushToast({
        message: "Source deleted",
        status: STATUSES.success,
      });
    },
    [deleteSource],
  );

  const handleCreateAutomation = useCallback(
    async (values: AutomationRuleFormValues) => {
      const rule = await createAutomationRule(values);
      pushToast({
        message: "Automation added",
        status: STATUSES.success,
      });
      return rule;
    },
    [createAutomationRule],
  );

  const handleUpdateAutomation = useCallback(
    async (rule: AutomationRule, values: AutomationRuleFormValues) => {
      const updatedRule = await updateAutomationRule(rule.id, values);
      pushToast({
        message: "Automation saved",
        status: STATUSES.success,
      });
      return updatedRule;
    },
    [updateAutomationRule],
  );

  const handleToggleAutomation = useCallback(
    async (rule: AutomationRule, enabled: boolean) => {
      try {
        return await setAutomationRuleEnabled(rule.id, enabled);
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update automation."),
          status: STATUSES.error,
        });
        throw error;
      }
    },
    [setAutomationRuleEnabled],
  );

  const handleDeleteAutomation = useCallback(
    async (rule: AutomationRule) => {
      await deleteAutomationRule(rule.id);
      pushToast({
        message: "Automation deleted",
        status: STATUSES.success,
      });
    },
    [deleteAutomationRule],
  );

  if (!canManageCurtailment) {
    return <Navigate to="/settings/network" replace />;
  }

  const effectiveSiteOptions = canLoadSiteOptions ? siteOptions : [];
  const defaultResponseProfileSiteScope = canLoadSiteOptions
    ? getDefaultCurtailmentSiteScope(activeSite, effectiveSiteOptions)
    : undefined;
  const siteScopeDisabledReason = canReadSiteCatalog
    ? (siteOptionsLoadError ?? undefined)
    : "Site scope is not available for the current user.";

  return (
    <CurtailmentSettingsContent
      responseProfiles={responseProfiles}
      sources={sources}
      automationRules={automationRules}
      isLoadingResponseProfiles={isLoadingResponseProfiles}
      loadResponseProfilesError={responseProfilesLoadError}
      isLoadingSources={isLoading}
      loadSourcesError={loadError}
      isLoadingAutomationRules={isLoadingAutomationRules}
      loadAutomationRulesError={automationRulesLoadError}
      isSavingResponseProfile={isCreatingResponseProfile}
      isTestingResponseProfileCurtailment={isTestingResponseProfileCurtailment}
      isSavingSource={isCreating}
      isTestingSourceConnection={isTestingConnection}
      isSavingAutomationRule={isCreatingAutomationRule}
      updatingResponseProfileIds={updatingProfileIds}
      updatingSourceIds={updatingSourceIds}
      updatingAutomationRuleIds={updatingAutomationRuleIds}
      siteOptions={effectiveSiteOptions}
      defaultResponseProfileSiteScope={defaultResponseProfileSiteScope}
      isLoadingSiteOptions={canLoadSiteOptions ? isLoadingSiteOptions : false}
      siteScopeDisabledReason={siteScopeDisabledReason}
      infrastructureDevices={infrastructureDevices}
      isLoadingInfrastructureDevices={isLoadingInfrastructureDevices}
      infrastructureDevicesError={infrastructureDevicesError}
      onRetryInfrastructureDevices={retryInfrastructureDevices}
      onResponseProfileModalOpen={ensureSiteOptionsLoaded}
      onCreateResponseProfile={handleCreateResponseProfile}
      onUpdateResponseProfile={handleUpdateResponseProfile}
      onTestResponseProfileCurtailment={handleTestResponseProfileCurtailment}
      onDeleteResponseProfile={handleDeleteResponseProfile}
      onCreateSource={handleCreateSource}
      onUpdateSource={handleUpdateSource}
      onTestSourceConnection={handleTestSourceConnection}
      onToggleSource={handleToggleSource}
      onDeleteSource={handleDeleteSource}
      onCreateAutomation={handleCreateAutomation}
      onUpdateAutomation={handleUpdateAutomation}
      onToggleAutomation={handleToggleAutomation}
      onDeleteAutomation={handleDeleteAutomation}
    />
  );
}

export default CurtailmentSettingsPage;
