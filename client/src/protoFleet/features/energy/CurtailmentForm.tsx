import { type ReactElement, type ReactNode, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import {
  ForceInclusionFields,
  formatCountLabel,
  formatCurtailmentPreviewSummary,
  getCurtailmentConfirmationCopy,
  getForceInclusionConfirmationCopy,
  getSelectedMinerIds,
  PendingCurtailmentConfirmation,
} from "./curtailmentConfirmation";
import {
  CurtailmentTargetPath,
  getCurtailmentTargetPath,
  getSelectedSiteIds,
  getSiteNameForId,
  getValidSiteScopeIds,
  hasAllMinersSelected,
  hasResponseProfileScopeValues,
  parseCurtailmentTargetIds,
  withAllMinerScope,
  withSelectedResponseProfileValues,
  withSiteScopes,
  withTargetPathScope,
  withTerminalScope,
} from "./curtailmentFormScope";
import { hasEditableCurtailmentChanges, validateCurtailmentFormValues } from "./curtailmentFormValidation";
import type { CurtailmentModalShell, CurtailmentStartModalProps } from "./curtailmentModalTypes";
import { useCurtailmentForm } from "./useCurtailmentForm";
import { responseProfileExecutionConflictMessage } from "@/protoFleet/api/curtailmentProfileErrors";

import type {
  CurtailmentFormErrors,
  CurtailmentFormValues,
  CurtailmentMode,
  CurtailmentPlanPreview,
  CurtailmentStartModalMode,
  CurtailmentSubmitValues,
} from "@/protoFleet/api/curtailmentTypes";

import { formatCurtailmentKw as formatKw, isCurtailmentAdmin } from "@/protoFleet/api/curtailmentUtils";
import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import {
  BuildingSelectionModal,
  GroupSelectionModal,
  MinerSelectionModal,
  type MinerSelectionValue,
  RackSelectionModal,
} from "@/protoFleet/components/TargetSelectionModal";
import {
  customResponseProfileId,
  supportsAllPairedTargeting,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import FacilityFanSelectionModal, {
  type FacilityFanSelectionValue,
} from "@/protoFleet/features/energy/FacilityFanSelectionModal";
import {
  createCurtailmentPlanPreview,
  useCurtailmentPlanPreview,
} from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import { useRole } from "@/protoFleet/store";
import { Alert, LightningAlt, Question } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";
import { positions } from "@/shared/constants";

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
}

interface ApplyToTarget {
  label: string;
  value: string;
}

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
// Full shutdown leads: whole-fleet curtailment is the primary operator flow
// (matches the response-profile default and DeviceSettingsModal ordering).
const curtailmentModeOptions = [
  { value: "fullFleet", label: "Full shutdown" },
  { value: "fixedKwReduction", label: "Fixed kW reduction" },
];
const getSiteScopeRowId = (siteId: string) => `site:${siteId}`;

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
          : "text-text-primary hover:bg-surface-base-hover focus-visible:bg-surface-base-hover"
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

function getInfrastructureApplyToTarget(values: CurtailmentFormValues): ApplyToTarget {
  const selectedDeviceCount = values.facilityFanDeviceIds?.length ?? 0;

  return {
    label: "Infrastructure",
    value: selectedDeviceCount > 0 ? formatCountLabel(selectedDeviceCount, "device") : "Select",
  };
}

function getSiteApplyToTarget(
  values: Pick<CurtailmentFormValues, "scopeId" | "siteId" | "siteIds" | "siteSelection">,
): ApplyToTarget {
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

function getTopologyApplyToTarget(label: "Buildings" | "Racks" | "Groups", ids: readonly string[]): ApplyToTarget {
  const singular = label.slice(0, -1).toLowerCase();
  return {
    label,
    value: ids.length > 0 ? formatCountLabel(ids.length, singular) : "Select",
  };
}

function getPreviewState({ apiPreview, controlledPreview, isEditMode }: PreviewStateOptions): PreviewPaneProps {
  if (isEditMode) {
    return controlledPreview ?? { preview: undefined, previewError: undefined, isPreviewLoading: false };
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

export function CurtailmentForm({
  shell,
  open,
  onDismiss,
  onSubmit,
  onStopCurtailment,
  onTestCurtailment,
  onDeleteResponseProfile,
  initialValues,
  responseProfiles = [],
  siteOptions = [],
  infrastructureDevices = [],
  isLoadingInfrastructureDevices = false,
  infrastructureDevicesError = null,
  onRetryInfrastructureDevices,
  facilityFanSelectionDisabledReason,
  defaultSiteScope,
  siteScopeEnabled = true,
  buildingScopeEnabled = true,
  rackAndGroupScopeEnabled = true,
  isSiteScopeLoading = false,
  siteScopeDisabledReason,
  errors,
  preview,
  previewError,
  actionError,
  isSubmitting = false,
  isTestingCurtailment = false,
  isDeleting = false,
}: CurtailmentStartModalProps & { shell: CurtailmentModalShell }): ReactElement {
  const {
    variant,
    isResponseProfileVariant,
    isResponseProfileEditMode,
    isLiveCurtailmentEditMode,
    shouldResetResponseProfileOnEdit,
    nameFieldId,
    nameFieldLabel,
    modalTitle,
    closeAriaLabel,
    primaryButtonText,
    shouldShowResponseProfileSelector,
  } = shell;
  const canUseAdminControls = isCurtailmentAdmin(useRole());
  const {
    initialFormValues,
    values,
    setValues,
    targetPath,
    setTargetPath,
    showForceInclusionConfirmation,
    setShowForceInclusionConfirmation,
    pendingForceInclusionValues,
    setPendingForceInclusionValues,
    confirmedForceInclusionKey,
    setConfirmedForceInclusionKey,
    submitAfterForceInclusionConfirmation,
    setSubmitAfterForceInclusionConfirmation,
    pendingCurtailmentConfirmation,
    setPendingCurtailmentConfirmation,
    showMinerSelectionModal,
    setShowMinerSelectionModal,
    showBuildingSelectionModal,
    setShowBuildingSelectionModal,
    showRackSelectionModal,
    setShowRackSelectionModal,
    showGroupSelectionModal,
    setShowGroupSelectionModal,
    showSiteScopeModal,
    setShowSiteScopeModal,
    showFacilityFanSelectionModal,
    setShowFacilityFanSelectionModal,
    draftSelectedSiteIds,
    setDraftSelectedSiteIds,
    editedFields,
    setEditedFields,
  } = useCurtailmentForm(initialValues, variant, defaultSiteScope);
  const hasRestrictedAllPairedTargeting =
    !canUseAdminControls &&
    !isLiveCurtailmentEditMode &&
    values.forceIncludeAllPairedMiners &&
    supportsAllPairedTargeting(values);
  const resetResponseProfileSelection = (nextValues: CurtailmentFormValues): CurtailmentFormValues => {
    if (!shouldResetResponseProfileOnEdit || nextValues.responseProfileId === customResponseProfileId) {
      return nextValues;
    }

    return {
      ...nextValues,
      responseProfileId: customResponseProfileId,
      responseProfileRevision: undefined,
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
    () => validateCurtailmentFormValues(values, validationMode, initialFormValues, variant, canUseAdminControls),
    [canUseAdminControls, initialFormValues, validationMode, values, variant],
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
  const controlledPreviewValue = preview
    ? createCurtailmentPlanPreview(effectiveValues, {
        selectedMinerCount: preview.selectedMinerCount,
        facilityFanDeviceCount: preview.facilityFanDeviceCount,
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
    disabled: isLiveCurtailmentEditMode || hasRestrictedAllPairedTargeting || controlledPreview !== undefined,
  });
  const previewState = getPreviewState({
    apiPreview,
    controlledPreview,
    isEditMode: isLiveCurtailmentEditMode,
  });

  const selectedProfile = responseProfiles.find((profile) => profile.id === values.responseProfileId);
  const selectedProfileChanged =
    selectedProfile !== undefined && selectedProfile.revision !== values.responseProfileRevision;
  const canReloadSelectedProfile =
    selectedProfile !== undefined &&
    (selectedProfileChanged ||
      apiPreview.previewError === responseProfileExecutionConflictMessage ||
      actionError === responseProfileExecutionConflictMessage);
  const hasLocalFormError = Object.keys(localErrors).length > 0;
  const hasExternalFormError = Object.keys(errors ?? {}).length > 0;
  const hasBlockingRunPreviewState =
    previewState.previewError !== undefined || (!isResponseProfileVariant && previewState.isPreviewLoading);
  const hasBlockingSubmitPreviewState = !isResponseProfileVariant && hasBlockingRunPreviewState;
  const hasEditableChanges = !isLiveCurtailmentEditMode || hasEditableCurtailmentChanges(values, initialFormValues);
  const isSubmitDisabled =
    isBusy ||
    hasRestrictedAllPairedTargeting ||
    selectedProfileChanged ||
    hasBlockingSubmitPreviewState ||
    hasExternalFormError ||
    !hasEditableChanges;
  const displayedPreviewState = isResponseProfileVariant ? responseProfilePreviewState(previewState) : previewState;
  const isPrimarySubmitDisabled = isSubmitDisabled;
  const isRunCurtailmentDisabled =
    isBusy || hasRestrictedAllPairedTargeting || hasBlockingRunPreviewState || hasExternalFormError;
  const selectedMinerIds = getSelectedMinerIds(effectiveValues);
  const minerApplyToTarget = getMinerApplyToTarget(effectiveValues);
  const targetPathSiteValues = {
    scopeId:
      targetPath.siteSelection === "allSites"
        ? "All sites"
        : targetPath.siteIds.length === 1
          ? (siteOptions.find((site) => site.id === targetPath.siteIds[0])?.name ??
            targetPath.siteNamesById[targetPath.siteIds[0]])
          : undefined,
    siteId: targetPath.siteIds[0],
    siteIds: targetPath.siteIds,
    siteSelection: targetPath.siteSelection,
  };
  const siteApplyToTarget = getSiteApplyToTarget(targetPathSiteValues);
  const buildingApplyToTarget = getTopologyApplyToTarget("Buildings", targetPath.buildingIds);
  const rackApplyToTarget = getTopologyApplyToTarget("Racks", targetPath.rackIds);
  const groupApplyToTarget = getTopologyApplyToTarget("Groups", targetPath.groupIds);
  const targetPathScope = useMemo(
    () => ({
      siteIds: parseCurtailmentTargetIds(targetPath.siteIds),
      includeUnassigned: false,
    }),
    [targetPath.siteIds],
  );
  const targetPathBuildingIds = useMemo(
    () => parseCurtailmentTargetIds(targetPath.buildingIds),
    [targetPath.buildingIds],
  );
  const minerInitialFilter = useMemo(
    () =>
      create(MinerListFilterSchema, {
        buildingIds: targetPathBuildingIds,
        rackIds: parseCurtailmentTargetIds(targetPath.rackIds),
        groupIds: parseCurtailmentTargetIds(targetPath.groupIds),
      }),
    [targetPath.groupIds, targetPath.rackIds, targetPathBuildingIds],
  );
  const infrastructureApplyToTarget = getInfrastructureApplyToTarget(effectiveValues);
  const isFacilityFanSelectionDisabled = facilityFanSelectionDisabledReason !== undefined;
  const isInfrastructureApplyToDisabled = isLiveCurtailmentEditMode || isFacilityFanSelectionDisabled;
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

    for (const currentSiteId of targetPath.siteIds) {
      if (siteScopeOptionById.has(currentSiteId)) {
        continue;
      }
      siteRows.push({
        id: getSiteScopeRowId(currentSiteId),
        label: targetPath.siteNamesById[currentSiteId] ?? getSiteNameForId(values, currentSiteId),
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
    targetPath.siteIds,
    targetPath.siteNamesById,
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
        responseProfileRevision: undefined,
      }));
      return;
    }

    const responseProfile = responseProfiles.find((profile) => profile.id === responseProfileId);
    if (!responseProfile) {
      return;
    }

    const selectedValues = {
      ...withSelectedResponseProfileValues(values, responseProfile.values),
      responseProfileId: responseProfile.id,
      responseProfileRevision: responseProfile.revision,
    };
    setEditedFields(new Set());
    setConfirmedForceInclusionKey("");
    if (hasResponseProfileScopeValues(responseProfile.values)) {
      setTargetPath(getCurtailmentTargetPath(selectedValues));
    }
    setValues(selectedValues);
  };

  const openSiteScopeModal = () => {
    const nextDraftSiteIds = targetPath.siteSelection === "allSites" ? selectableSiteIds : targetPath.siteIds;
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

    const siteNamesById = Object.fromEntries(
      selectedSiteIdsForSave.map((siteId) => [
        siteId,
        siteScopeOptionById.get(siteId)?.name ?? targetPath.siteNamesById[siteId] ?? `Site ${siteId}`,
      ]),
    );
    const nextPath: CurtailmentTargetPath = {
      siteSelection: selectedSiteIdsForSave.length === 0 ? "none" : allSelectableSitesSelected ? "allSites" : "site",
      siteIds: selectedSiteIdsForSave,
      siteNamesById,
      buildingIds: selectedSiteIdsForSave.length === 0 ? targetPath.buildingIds : [],
      rackIds: selectedSiteIdsForSave.length === 0 ? targetPath.rackIds : [],
      groupIds: selectedSiteIdsForSave.length === 0 ? targetPath.groupIds : [],
    };
    setTargetPath(nextPath);
    updateValues(
      (current) =>
        selectedSiteIdsForSave.length === 0 && current.scopeType === "explicitMiners"
          ? current
          : withTargetPathScope(current, nextPath),
      { resetResponseProfileSelection: true },
    );
    setShowSiteScopeModal(false);
  };

  const applyTargetPath = (nextPath: CurtailmentTargetPath) => {
    setTargetPath(nextPath);
    updateValues((current) => withTargetPathScope(current, nextPath), { resetResponseProfileSelection: true });
  };

  const handleBuildingSelection = (buildingIds: string[]) => {
    if (buildingIds.length === 0 && values.scopeType !== "building") {
      return;
    }
    applyTargetPath({ ...targetPath, buildingIds, rackIds: [], groupIds: [] });
  };

  const handleRackSelection = (rackIds: string[]) => {
    if (rackIds.length === 0 && values.scopeType !== "rack") {
      return;
    }
    applyTargetPath({ ...targetPath, rackIds, groupIds: [] });
  };

  const handleGroupSelection = (groupIds: string[]) => {
    if (groupIds.length === 0 && values.scopeType !== "group") {
      return;
    }
    applyTargetPath({ ...targetPath, buildingIds: [], rackIds: [], groupIds });
  };

  const handleMinerSelection = (selection: MinerSelectionValue) => {
    if (selection.allSelected) {
      setTargetPath({
        siteSelection: siteOptions.length > 0 ? "allSites" : "none",
        siteIds: [],
        siteNamesById: {},
        buildingIds: [],
        rackIds: [],
        groupIds: [],
      });
      updateValues(withAllMinerScope, { resetResponseProfileSelection: true });
      return;
    }

    const deviceIdentifiers = selection.selectedMinerIds;
    const hasSelectedMiners = deviceIdentifiers.length > 0;

    updateValues(
      (current) =>
        hasSelectedMiners
          ? withTerminalScope(current, { type: "explicitMiners", ids: deviceIdentifiers })
          : withTargetPathScope(current, targetPath),
      { resetResponseProfileSelection: true },
    );
  };

  const handleFacilityFanSelection = (selection: FacilityFanSelectionValue) => {
    setEditedFields(
      (current) =>
        new Set<keyof CurtailmentFormValues>([
          ...current,
          "facilityFanDeviceIds",
          "fanOffDelaySec",
          "fanRestoreDelaySec",
        ]),
    );
    updateValues(
      (current) => ({
        ...current,
        facilityFanDeviceIds: selection.selectedDeviceIds,
        fanOffDelaySec: selection.fanOffDelaySec,
        fanRestoreDelaySec: selection.fanRestoreDelaySec,
      }),
      { resetResponseProfileSelection: true },
    );
    setShowFacilityFanSelectionModal(false);
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

    if (isPrimarySubmitDisabled) {
      return;
    }

    if (shell.requiresRunConfirmation && requiresForceInclusionConfirmation(effectiveValues)) {
      requestForceInclusionConfirmation({}, "run");
      return;
    }

    if (shell.requiresRunConfirmation) {
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

    if (isRunCurtailmentDisabled) {
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
      disabled: isRunCurtailmentDisabled,
      loading: isTestingCurtailment,
    });
  }

  buttons.push({
    text: primaryButtonText,
    variant: variants.primary,
    onClick: handleSubmit,
    disabled: isPrimarySubmitDisabled,
    loading: isSubmitting,
  });

  const confirmForceInclusion = () => {
    const confirmedValues = {
      ...effectiveValues,
      ...pendingForceInclusionValues,
    };
    const nextValues =
      pendingForceInclusionValues && Object.keys(pendingForceInclusionValues).length > 0
        ? resetResponseProfileSelection(confirmedValues)
        : confirmedValues;

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
                    {canReloadSelectedProfile && selectedProfile ? (
                      <div className="flex items-center gap-3 text-300 text-text-critical" role="alert">
                        <span>This profile changed. Reload it and review the updated plan.</span>
                        <Button
                          variant={variants.secondary}
                          text="Reload profile"
                          onClick={() => handleResponseProfileChange(selectedProfile.id)}
                        />
                      </div>
                    ) : null}
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
              subtext={
                facilityFanSelectionDisabledReason ??
                "Choose a site-to-miner path and any infrastructure included in this curtailment."
              }
            >
              <div className="grid">
                <TargetSelectButton
                  label={siteApplyToTarget.label}
                  value={siteApplyToTarget.value}
                  disabled={isLiveCurtailmentEditMode}
                  onClick={openSiteScopeModal}
                />
                {buildingScopeEnabled || targetPath.buildingIds.length > 0 ? (
                  <TargetSelectButton
                    label={buildingApplyToTarget.label}
                    value={buildingApplyToTarget.value}
                    disabled={isLiveCurtailmentEditMode || !buildingScopeEnabled}
                    onClick={() => setShowBuildingSelectionModal(true)}
                  />
                ) : null}
                {rackAndGroupScopeEnabled || targetPath.rackIds.length > 0 ? (
                  <TargetSelectButton
                    label={rackApplyToTarget.label}
                    value={rackApplyToTarget.value}
                    disabled={isLiveCurtailmentEditMode || !rackAndGroupScopeEnabled}
                    onClick={() => setShowRackSelectionModal(true)}
                  />
                ) : null}
                {rackAndGroupScopeEnabled || targetPath.groupIds.length > 0 ? (
                  <TargetSelectButton
                    label={groupApplyToTarget.label}
                    value={groupApplyToTarget.value}
                    disabled={isLiveCurtailmentEditMode || !rackAndGroupScopeEnabled}
                    onClick={() => setShowGroupSelectionModal(true)}
                  />
                ) : null}
                <TargetSelectButton
                  label={minerApplyToTarget.label}
                  value={minerApplyToTarget.value}
                  disabled={isLiveCurtailmentEditMode}
                  onClick={() => setShowMinerSelectionModal(true)}
                />
                <TargetSelectButton
                  label={infrastructureApplyToTarget.label}
                  value={infrastructureApplyToTarget.value}
                  disabled={isInfrastructureApplyToDisabled}
                  onClick={() => setShowFacilityFanSelectionModal(true)}
                />
              </div>
            </Section>

            {/*
              The "Include miners in maintenance" checkbox is intentionally hidden from the UI.
              includeMaintenance defaults to false so non-admin operators with curtailment:manage
              can still start curtailments (force_include_maintenance is admin-gated server-side).
              Only admins can enable "Target all paired miners"; non-admins can turn off an
              inherited value. Enabling it also opts in maintenance-flagged miners via the request builders.
              Re-add the checkbox here if maintenance ever needs to become independently togglable.

              The checkbox is also available for logical topology scopes; explicit miner snapshots
              remain ineligible for the all-paired policy.
            */}
            {(canUseAdminControls || values.forceIncludeAllPairedMiners) &&
            isFullFleetMode &&
            supportsAllPairedTargeting(values) ? (
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
                {hasRestrictedAllPairedTargeting ? (
                  <p className="text-300 text-text-critical" role="alert">
                    Only administrators can target all paired miners. Turn this off to{" "}
                    {isResponseProfileVariant ? "continue." : "run a custom plan."}
                  </p>
                ) : null}
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
          scope={targetPathScope}
          initialFilter={minerInitialFilter}
          filterConfig={rackAndGroupScopeEnabled ? undefined : { showRackFilter: false, showGroupFilter: false }}
          onDismiss={() => setShowMinerSelectionModal(false)}
          onSave={(selection) => {
            handleMinerSelection(selection);
            setShowMinerSelectionModal(false);
          }}
        />
      ) : null}

      {buildingScopeEnabled && showBuildingSelectionModal ? (
        <BuildingSelectionModal
          open={showBuildingSelectionModal}
          selectedBuildingIds={targetPath.buildingIds}
          scope={targetPathScope}
          preserveMissingSelections
          onDismiss={() => setShowBuildingSelectionModal(false)}
          onSave={(buildingIds) => {
            handleBuildingSelection(buildingIds);
            setShowBuildingSelectionModal(false);
          }}
        />
      ) : null}

      {rackAndGroupScopeEnabled && showRackSelectionModal ? (
        <RackSelectionModal
          open={showRackSelectionModal}
          selectedRackIds={targetPath.rackIds}
          scope={targetPathScope}
          buildingIds={targetPathBuildingIds}
          preserveMissingSelections
          onDismiss={() => setShowRackSelectionModal(false)}
          onSave={(rackIds) => {
            handleRackSelection(rackIds);
            setShowRackSelectionModal(false);
          }}
        />
      ) : null}

      {rackAndGroupScopeEnabled && showGroupSelectionModal ? (
        <GroupSelectionModal
          open={showGroupSelectionModal}
          selectedGroupIds={targetPath.groupIds}
          scope={targetPathScope}
          preserveMissingSelections
          onDismiss={() => setShowGroupSelectionModal(false)}
          onSave={(groupIds) => {
            handleGroupSelection(groupIds);
            setShowGroupSelectionModal(false);
          }}
        />
      ) : null}

      {!isInfrastructureApplyToDisabled && showFacilityFanSelectionModal ? (
        <FacilityFanSelectionModal
          devices={infrastructureDevices}
          initialSelectedDeviceIds={values.facilityFanDeviceIds ?? []}
          initialFanOffDelaySec={values.fanOffDelaySec ?? ""}
          initialFanRestoreDelaySec={values.fanRestoreDelaySec ?? ""}
          isLoading={isLoadingInfrastructureDevices}
          loadError={infrastructureDevicesError}
          onRetry={onRetryInfrastructureDevices}
          onDismiss={() => setShowFacilityFanSelectionModal(false)}
          onApply={handleFacilityFanSelection}
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
