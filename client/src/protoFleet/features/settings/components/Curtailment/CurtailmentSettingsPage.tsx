import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { CurtailmentSourcesTable } from "./CurtailmentSourcesTable";
import { ResponseProfileCards } from "./ResponseProfileCards";
import { SourceModal, type SourceModalMode } from "./SourceModal";
import {
  getResponseProfileScopeSummary,
  getSelectedResponseProfileSiteIds,
} from "@/protoFleet/api/curtailmentResponseProfileUtils";

import {
  type AutomationRule,
  type AutomationRuleFormValues,
  type CurtailmentSiteOption,
  type CurtailmentSource,
  type CurtailmentSourceFormValues,
  type CurtailmentSubmitValues,
  DEFAULT_SOURCE_STALENESS_THRESHOLD_SEC,
  isEditableResponseProfile,
  isResponseProfileAutomationReady,
  type ResponseProfile,
  type ResponseProfileFormValues,
  type ResponseProfileModalMode,
} from "@/protoFleet/api/curtailmentTypes";

import { formatCurtailmentKw, isCurtailmentAdmin } from "@/protoFleet/api/curtailmentUtils";

import { useSites } from "@/protoFleet/api/sites";
import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import useCurtailmentAutomationRules from "@/protoFleet/api/useCurtailmentAutomationRules";
import useCurtailmentResponseProfiles from "@/protoFleet/api/useCurtailmentResponseProfiles";
import useInfrastructureDevices from "@/protoFleet/api/useInfrastructureDevices";
import useMqttCurtailmentSources from "@/protoFleet/api/useMqttCurtailmentSources";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import {
  createCurtailmentFormValuesFromResponseProfile,
  createResponseProfileFormValuesFromCurtailmentValues,
  createResponseProfileFormValuesFromProfile,
  createSiteOptions,
  normalizeResponseProfileFormValues,
} from "@/protoFleet/features/energy/curtailmentProfileModel";
import { getDefaultCurtailmentSiteScope } from "@/protoFleet/features/energy/curtailmentSiteScopeDefaults";
import CurtailmentStartModal from "@/protoFleet/features/energy/CurtailmentStartModal";
import type { FacilityFanDeviceOption } from "@/protoFleet/features/energy/FacilityFanSelectionModal";
import CurtailmentAutomationsContent from "@/protoFleet/features/settings/components/Curtailment/CurtailmentAutomations";
import {
  InfoToggleContent,
  SectionHeader,
} from "@/protoFleet/features/settings/components/Curtailment/CurtailmentSection";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useHasPermission, useRole } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { Info } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Callout, { intents } from "@/shared/components/Callout";
import { PopoverProvider } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import type { SelectOption } from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import "./CurtailmentSettingsPage.css";

const CURTAILMENT_PAGE_DESCRIPTION =
  "Configure response profiles, manage external signal sources, and define automations that trigger curtailment.";
const RESPONSE_PROFILES_DESCRIPTION = "Saved configurations that define how much power to shed and how to restore it.";
const SOURCES_DESCRIPTION = "MaestroOS MQTT brokers that publish curtailment signals.";
const PROTO_RIG_FALLBACK_DESCRIPTION =
  "Proto rigs can use compatible TCP MaestroOS sources as a local fallback. Fleet also sends commands to all targeted miners, confirms curtailment, tracks progress, and restores them.";

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

const emptyResponseProfileFormValues: ResponseProfileFormValues = {
  name: "",
  actionType: "fullFleet",
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  postEventCooldownSec: "",
  scopeType: "wholeOrg",
  buildingTargetIds: [],
  rackTargetIds: [],
  groupTargetIds: [],
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
  maxDurationSec: "",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  facilityFanDeviceIds: [],
  fanOffDelaySec: "",
  fanRestoreDelaySec: "",
  responseDeadlineMinutes: "",
  // Maintenance-flagged miners are excluded by default: force_include_maintenance
  // is admin-gated server-side, so defaulting it on would block non-admin
  // operators with curtailment:manage from saving any profile. "Target all
  // paired miners" opts the maintenance population in (admin-gated anyway).
  includeMaintenance: false,
  forceIncludeAllPairedMiners: false,
};

function getResponseProfileTargetSummary(values: ResponseProfileFormValues): string {
  const targetKw = formatCurtailmentKw(Number(values.targetKw), {
    maximumFractionDigits: 3,
    minimumFractionDigits: 0,
    includeUnit: false,
    normalizeNonFinite: false,
  });

  switch (values.actionType) {
    case "fixedKwReduction":
      return `${targetKw} kW target`;
    case "fullFleet":
    default:
      return "100% reduction";
  }
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
  const normalizedValues: ResponseProfileFormValues = {
    ...normalizeResponseProfileFormValues(values),
    name: values.name.trim(),
    targetKw: values.targetKw.trim(),
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
    formValues: normalizedValues,
    isAutomationReady: isResponseProfileAutomationReady(normalizedValues.scopeType),
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

function getErrorMessage(error: unknown, fallbackMessage: string): string {
  return error instanceof Error && error.message ? error.message : fallbackMessage;
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
  buildingScopeEnabled?: boolean;
  rackAndGroupScopeEnabled?: boolean;
  isLoadingSiteOptions?: boolean;
  siteScopeDisabledReason?: string;
  infrastructureDevices?: FacilityFanDeviceOption[];
  isLoadingInfrastructureDevices?: boolean;
  infrastructureDevicesError?: string | null;
  onRetryInfrastructureDevices?: () => void;
  onResponseProfileModalOpen?: () => void;
  onCreateResponseProfile?: (values: ResponseProfileFormValues) => Promise<ResponseProfile>;
  onUpdateResponseProfile?: (profile: ResponseProfile, values: ResponseProfileFormValues) => Promise<ResponseProfile>;
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
  onToggleAutomation?: (
    rule: AutomationRule,
    enabled: boolean,
    expectedResponseProfileRevision?: string,
  ) => Promise<AutomationRule | void>;
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
  buildingScopeEnabled = true,
  rackAndGroupScopeEnabled = true,
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
  const canManageSources = isCurtailmentAdmin(useRole());
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
  const knownAutomationRules = controlledAutomationRules ?? initialAutomationRules;
  const latestEditingResponseProfile = editingResponseProfile
    ? (responseProfiles.find((profile) => profile.id === editingResponseProfile.id) ?? editingResponseProfile)
    : null;
  const canTestResponseProfileCurtailment = Boolean(
    onTestResponseProfileCurtailment &&
    (latestEditingResponseProfile ? onUpdateResponseProfile : onCreateResponseProfile),
  );
  const responseProfileModalMode: ResponseProfileModalMode = editingResponseProfile ? "edit" : "create";
  const responseProfileModalInitialValues = useMemo(
    () =>
      latestEditingResponseProfile
        ? (createResponseProfileFormValuesFromProfile(latestEditingResponseProfile) ?? emptyResponseProfileFormValues)
        : emptyResponseProfileFormValues,
    [latestEditingResponseProfile],
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
  const isEditingResponseProfile = latestEditingResponseProfile
    ? updatingResponseProfileIds.has(latestEditingResponseProfile.id)
    : false;
  const facilityFanSelectionDisabledReason = latestEditingResponseProfile
    ? isLoadingAutomationRules
      ? "Checking whether an automation uses this profile. You can change infrastructure fans when this check is complete."
      : loadAutomationRulesError
        ? "We couldn't check whether an automation uses this profile. Reload the page before changing infrastructure fans."
        : knownAutomationRules.some((rule) => rule.responseProfileId === latestEditingResponseProfile.id)
          ? "An automation uses this profile. Update or delete the automation before changing infrastructure fans."
          : undefined
    : undefined;
  const isEditingSource = editingSource ? updatingSourceIds.has(editingSource.id) : false;

  const openCreateResponseProfileModal = useCallback(() => {
    onResponseProfileModalOpen?.();
    setResponseProfileActionError(null);
    setEditingResponseProfile(null);
    setIsResponseProfileModalOpen(true);
  }, [onResponseProfileModalOpen]);

  const openEditResponseProfileModal = useCallback(
    (profile: ResponseProfile) => {
      if (!isEditableResponseProfile(profile)) {
        return;
      }
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
      const profile = onCreateResponseProfile
        ? await onCreateResponseProfile(values)
        : createResponseProfileFromFormValues(values, responseProfiles);
      if (!controlledResponseProfiles) {
        setLocalResponseProfiles((currentProfiles) => [
          ...currentProfiles.filter((currentProfile) => currentProfile.id !== profile.id),
          profile,
        ]);
      }
      return profile;
    },
    [controlledResponseProfiles, onCreateResponseProfile, responseProfiles],
  );

  const handleSaveResponseProfile = useCallback(
    async (values: ResponseProfileFormValues) => {
      if (!latestEditingResponseProfile) {
        return handleCreateResponseProfile(values);
      }

      const updatedProfile = onUpdateResponseProfile
        ? await onUpdateResponseProfile(latestEditingResponseProfile, values)
        : createResponseProfileFromFormValues(values, responseProfiles, latestEditingResponseProfile);
      if (!controlledResponseProfiles) {
        setLocalResponseProfiles((currentProfiles) =>
          currentProfiles.map((currentProfile) =>
            currentProfile.id === updatedProfile.id ? updatedProfile : currentProfile,
          ),
        );
      }
      return updatedProfile;
    },
    [
      controlledResponseProfiles,
      latestEditingResponseProfile,
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

      const savedProfile = await handleSaveResponseProfile(responseProfileValues);
      if (!savedProfile.revision || !savedProfile.formValues) {
        throw new Error("Reload the response profile before running curtailment.");
      }
      await onTestResponseProfileCurtailment?.(savedProfile.formValues, {
        ...values,
        ...createCurtailmentFormValuesFromResponseProfile(savedProfile.formValues),
        responseProfileId: savedProfile.id,
        responseProfileRevision: savedProfile.revision,
      });
      closeResponseProfileModal();
    },
    [closeResponseProfileModal, handleSaveResponseProfile, onTestResponseProfileCurtailment],
  );

  const handleDeleteResponseProfile = useCallback(async () => {
    if (!latestEditingResponseProfile) {
      return;
    }

    await onDeleteResponseProfile?.(latestEditingResponseProfile);
    if (!controlledResponseProfiles) {
      setLocalResponseProfiles((currentProfiles) =>
        currentProfiles.filter((currentProfile) => currentProfile.id !== latestEditingResponseProfile.id),
      );
    }
  }, [controlledResponseProfiles, latestEditingResponseProfile, onDeleteResponseProfile]);

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

  const noDataElement = getSourcesEmptyState(loadSourcesError, isLoadingSources);

  return (
    <div className="flex flex-col gap-14" data-testid="settings-curtailment-page">
      <div className="flex flex-col gap-6">
        <SettingsPageHeader title="Curtailment" description={CURTAILMENT_PAGE_DESCRIPTION} />

        <Callout
          intent={intents.information}
          prefixIcon={<Info width={iconSizes.medium} />}
          subtitle={PROTO_RIG_FALLBACK_DESCRIPTION}
          testId="proto-rig-curtailment-fallback-notice"
          title="Proto rig fallback"
        />
      </div>

      <section className="curtailment-settings__section">
        <SectionHeader
          variant="settings"
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
          variant="settings"
          title="Sources"
          buttonText="Add source"
          onButtonClick={canManageSources ? openCreateSourceModal : undefined}
          infoToggle={<SourcesInfoToggle />}
        />
        <CurtailmentSourcesTable
          sources={sources}
          onToggle={toggleSource}
          updatingSourceIds={updatingSourceIds}
          canManageSources={canManageSources}
          noDataElement={noDataElement}
          onEdit={openEditSourceModal}
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
            ? `response-profile-${responseProfileModalMode}-${latestEditingResponseProfile?.id ?? "new"}-${latestEditingResponseProfile?.revision ?? "local"}`
            : "response-profile-modal-closed"
        }
        open={isResponseProfileModalOpen}
        variant="responseProfile"
        responseProfileMode={responseProfileModalMode}
        initialValues={responseProfileCurtailmentInitialValues}
        siteOptions={siteOptions}
        buildingScopeEnabled={buildingScopeEnabled}
        rackAndGroupScopeEnabled={rackAndGroupScopeEnabled}
        defaultSiteScope={responseProfileModalMode === "create" ? defaultResponseProfileSiteScope : undefined}
        siteScopeEnabled={siteOptions.length > 0 || isLoadingSiteOptions}
        isSiteScopeLoading={isLoadingSiteOptions}
        siteScopeDisabledReason={siteScopeDisabledReason}
        infrastructureDevices={infrastructureDevices}
        isLoadingInfrastructureDevices={isLoadingInfrastructureDevices}
        infrastructureDevicesError={infrastructureDevicesError}
        onRetryInfrastructureDevices={onRetryInfrastructureDevices}
        facilityFanSelectionDisabledReason={facilityFanSelectionDisabledReason}
        actionError={responseProfileActionError}
        onDismiss={closeResponseProfileModal}
        onSubmit={handleResponseProfileModalSubmit}
        onTestCurtailment={canTestResponseProfileCurtailment ? handleResponseProfileModalTestCurtailment : undefined}
        onDeleteResponseProfile={latestEditingResponseProfile ? handleResponseProfileModalDelete : undefined}
        isSubmitting={latestEditingResponseProfile ? isEditingResponseProfile : isSavingResponseProfile}
        isTestingCurtailment={isTestingResponseProfileCurtailment}
        isDeleting={latestEditingResponseProfile ? isDeletingResponseProfile || isEditingResponseProfile : false}
      />

      <SourceModal
        key={isSourceModalOpen ? `source-modal-${editingSource?.id ?? "new"}` : "source-modal-closed"}
        open={isSourceModalOpen ? canManageSources : false}
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
  const canReadRackCatalog = useHasPermission("rack:read");
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
    listResponseProfiles,
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
  } = useCurtailmentAutomationRules(canManageCurtailment, { refreshResponseProfiles: listResponseProfiles });
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
    async (rule: AutomationRule, enabled: boolean, expectedResponseProfileRevision?: string) => {
      try {
        return await setAutomationRuleEnabled(rule.id, enabled, expectedResponseProfileRevision);
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
      buildingScopeEnabled={canReadSiteCatalog}
      rackAndGroupScopeEnabled={canReadRackCatalog}
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
