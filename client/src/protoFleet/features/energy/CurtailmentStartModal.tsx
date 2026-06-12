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
  createCurtailmentPlanPreview,
  getUnsupportedDeviceSetPreviewError,
  useCurtailmentPlanPreview,
} from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import { Alert, LightningAlt, Question } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";
import { positions } from "@/shared/constants";

export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "site" | "deviceSet" | "explicitMiners";
export type ResponseProfileId = string;
export type CurtailmentMode = "fixedKwReduction" | "fullFleet";
export type MinerSelectionStrategy = "leastEfficientFirst";
export type CurtailmentStartModalMode = "create" | "edit";
export type CurtailmentStartModalVariant = "curtailment" | "responseProfile";
export type ResponseProfileModalMode = "create" | "edit";

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
  siteId?: string;
  deviceSetIds: string[];
  deviceIdentifiers: string[];
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
}

export type CurtailmentSubmitValues = CurtailmentFormValues;

export interface CurtailmentResponseProfileOption {
  id: ResponseProfileId;
  label: string;
  values: Partial<Omit<CurtailmentFormValues, "responseProfileId">>;
}

export interface CurtailmentPlanPreview {
  selectedMinerCount: number;
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

export const customResponseProfileId = "customPlan";
const responseProfileDescription = "Saved configurations that define how much power to shed and how to restore it.";
const fieldHelp = {
  curtailmentMode: "How power reduction is measured: fixed kW target or full shutdown.",
  fixedTargetReduction: "The amount to reduce based on the selected mode.",
  curtailBatchSize: "Number of miners to shut down in each wave.",
  curtailBatchInterval: "Seconds to wait between each curtailment wave.",
  restoreBatchSize: "Number of miners to bring back online in each wave.",
  restoreBatchInterval: "Seconds to wait between each restore wave.",
} as const;
const defaultValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  siteId: "",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: customResponseProfileId,
  curtailmentMode: "fixedKwReduction",
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
  includeMaintenance: true,
};
const editableCurtailmentFields: EditableCurtailmentField[] = ["reason", "restoreIntervalSec"];
const curtailmentModeOptions = [
  { value: "fixedKwReduction", label: "Fixed kW reduction" },
  { value: "fullFleet", label: "Full shutdown" },
];

function withWholeFleetScope(values: CurtailmentFormValues): CurtailmentFormValues {
  return {
    ...values,
    scopeType: "wholeOrg",
    scopeId: "whole-org",
    siteId: "",
    deviceSetIds: [],
    deviceIdentifiers: [],
  };
}

function withResponseProfileScope(
  values: CurtailmentFormValues,
  responseProfileMode: ResponseProfileModalMode,
): CurtailmentFormValues {
  if (responseProfileMode === "edit" && values.scopeType === "site" && values.siteId) {
    return {
      ...values,
      scopeType: "site",
      scopeId: values.scopeId ?? `Site ${values.siteId}`,
      deviceSetIds: [],
      deviceIdentifiers: [],
    };
  }

  return withWholeFleetScope(values);
}

function removeScopeValues(
  values: CurtailmentResponseProfileOption["values"],
): CurtailmentResponseProfileOption["values"] {
  const behaviorValues = { ...values };

  delete behaviorValues.scopeType;
  delete behaviorValues.scopeId;
  delete behaviorValues.siteId;
  delete behaviorValues.deviceSetIds;
  delete behaviorValues.deviceIdentifiers;

  return behaviorValues;
}

function isCurtailmentMode(value: string): value is CurtailmentMode {
  return value === "fixedKwReduction" || value === "fullFleet";
}

function getInitialValues(
  initialValues?: Partial<CurtailmentFormValues>,
  variant: CurtailmentStartModalVariant = "curtailment",
  responseProfileMode: ResponseProfileModalMode = "create",
): CurtailmentFormValues {
  const values = {
    ...defaultValues,
    ...initialValues,
  };

  return variant === "responseProfile" ? withResponseProfileScope(values, responseProfileMode) : values;
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
  const shouldValidateCurtailBatchFields = isResponseProfileVariant;
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
  if (isEditMode && restoreInterval.error === undefined && restoreInterval.parsed === 0) {
    localErrors.restoreIntervalSec = "Enter batch interval greater than 0.";
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
  if (isResponseProfileVariant && restoreBatchSize.error === undefined && restoreBatchSize.parsed === 0) {
    localErrors.restoreBatchSize = "Enter batch size greater than 0.";
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

function PreviewPane({ preview, previewError, isPreviewLoading = false }: PreviewPaneProps): ReactElement {
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
          <div className="text-heading-100 text-text-primary">
            Curtail {preview.selectedMinerCount} miners {preview.scopeLabel} immediately
          </div>
          <div className="text-heading-100 text-text-primary-50">
            {preview.curtailEstimate} to curtail, {preview.restoreEstimate} to restore
          </div>
        </div>
      </div>
    </div>
  );
}

function getSelectedMinerIds(values: CurtailmentFormValues): string[] {
  if (values.scopeType !== "explicitMiners") {
    return [];
  }

  return values.deviceIdentifiers;
}

function formatCountLabel(count: number, singular: string): string {
  return getTargetButtonLabel(count, singular);
}

function formatScopeLabelForSentence(scopeLabel: string): string {
  return scopeLabel === "All sites" ? "all sites" : scopeLabel;
}

function formatCurtailmentConfirmationTarget(values: CurtailmentFormValues, selectedMinerCount?: number): string {
  if (values.scopeType === "explicitMiners") {
    return formatCountLabel(values.deviceIdentifiers.length, "miner").toLowerCase();
  }

  if (values.scopeType === "deviceSet") {
    return `miners in ${formatCountLabel(values.deviceSetIds.length, "device set").toLowerCase()}`;
  }

  if (values.scopeType === "site") {
    return `miners in ${values.scopeId ? formatScopeLabelForSentence(values.scopeId) : "the selected site"}`;
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

function getApplyToTarget(
  values: CurtailmentFormValues,
  isEditMode: boolean,
  selectedMinerCount?: number,
): ApplyToTarget {
  if (!isEditMode) {
    return {
      label: "Miners",
      value: getTargetButtonLabel(getSelectedMinerIds(values).length, "miner"),
    };
  }

  if (values.scopeType === "site") {
    return {
      label: "Site",
      value: values.siteId ? `Site ${values.siteId}` : "Site",
    };
  }

  if (selectedMinerCount !== undefined) {
    return {
      label: "Miners",
      value: formatCountLabel(selectedMinerCount, "miner"),
    };
  }

  if (values.scopeType === "deviceSet") {
    return {
      label: "Device sets",
      value: formatCountLabel(values.deviceSetIds.length, "device set"),
    };
  }

  if (values.scopeType === "wholeOrg") {
    return {
      label: "Miners",
      value: "Whole fleet",
    };
  }

  return {
    label: "Miners",
    value: formatCountLabel(values.deviceIdentifiers.length, "miner"),
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
  errors,
  preview,
  previewError,
  actionError,
  isSubmitting = false,
  isTestingCurtailment = false,
  isDeleting = false,
}: CurtailmentStartModalProps): ReactElement {
  const [initialFormValues] = useState<CurtailmentFormValues>(() =>
    getInitialValues(initialValues, variant, responseProfileMode),
  );
  const [values, setValues] = useState<CurtailmentFormValues>(() => initialFormValues);
  const [showMaintenanceConfirmation, setShowMaintenanceConfirmation] = useState(false);
  const [maintenanceInclusionConfirmed, setMaintenanceInclusionConfirmed] = useState(false);
  const [submitAfterMaintenanceConfirmation, setSubmitAfterMaintenanceConfirmation] = useState<
    PendingCurtailmentConfirmation["action"] | null
  >(null);
  const [pendingCurtailmentConfirmation, setPendingCurtailmentConfirmation] =
    useState<PendingCurtailmentConfirmation | null>(null);
  const [showMinerSelectionModal, setShowMinerSelectionModal] = useState(false);
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

    return { ...nextValues, responseProfileId: customResponseProfileId };
  };
  const updateValue = <Key extends keyof CurtailmentFormValues>(key: Key, value: CurtailmentFormValues[Key]) => {
    setEditedFields((current) => (current.has(key) ? current : new Set(current).add(key)));
    setValues((current) => {
      const nextValues = { ...current, [key]: value };

      return key === "reason" ? nextValues : resetResponseProfileSelection(nextValues);
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
  const unsupportedDeviceSetPreviewError = getUnsupportedDeviceSetPreviewError(values);
  const controlledPreviewValue = preview
    ? createCurtailmentPlanPreview(values, {
        selectedMinerCount: preview.selectedMinerCount,
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
    values,
    disabled: isLiveCurtailmentEditMode || controlledPreview !== undefined,
  });
  const previewState = getPreviewState({
    apiPreview,
    controlledPreview,
    isEditMode: isLiveCurtailmentEditMode,
    unsupportedDeviceSetPreviewError,
  });

  const hasBlockingFormError = Object.keys(localErrors).length > 0 || Object.keys(errors ?? {}).length > 0;
  const hasBlockingPreviewState =
    previewState.previewError !== undefined || (!isResponseProfileVariant && previewState.isPreviewLoading);
  const hasBlockingValidationError = hasBlockingPreviewState || hasBlockingFormError;
  const hasEditableChanges = !isLiveCurtailmentEditMode || hasEditableCurtailmentChanges(values, initialFormValues);
  const isSubmitDisabled = isBusy || hasBlockingValidationError || !hasEditableChanges;
  const selectedMinerIds = getSelectedMinerIds(values);
  const applyToTarget = getApplyToTarget(values, isLiveCurtailmentEditMode, previewState.preview?.selectedMinerCount);
  const isFullFleetMode = values.curtailmentMode === "fullFleet";
  const curtailmentBehaviorSubtext = isLiveCurtailmentEditMode
    ? undefined
    : "Fleet will automatically curtail the least efficient miners first.";
  const curtailmentTargetGridClassName = isFullFleetMode ? "grid gap-3" : "grid gap-3 tablet:grid-cols-2";
  const shouldShowCurtailBatchFields = isResponseProfileVariant;
  const curtailBatchSizeTestId = isResponseProfileVariant
    ? "response-profile-curtail-batch-size"
    : "curtailment-curtail-batch-size";
  const curtailBatchIntervalTestId = isResponseProfileVariant
    ? "response-profile-curtail-batch-interval"
    : "curtailment-curtail-batch-interval";
  const shouldShowPreviewPane =
    !isLiveCurtailmentEditMode || previewState.preview !== undefined || previewState.previewError !== undefined;
  const previewPane = shouldShowPreviewPane ? <PreviewPane {...previewState} /> : null;
  const curtailmentConfirmationCopy = getCurtailmentConfirmationCopy(
    pendingCurtailmentConfirmation,
    previewState.preview?.selectedMinerCount,
  );
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
      setValues((current) => ({ ...current, responseProfileId: customResponseProfileId }));
      return;
    }

    const responseProfile = responseProfiles.find((profile) => profile.id === responseProfileId);
    if (!responseProfile) {
      return;
    }

    setEditedFields(new Set());
    setMaintenanceInclusionConfirmed(false);
    setValues((current) => ({
      ...current,
      ...removeScopeValues(responseProfile.values),
      responseProfileId: responseProfile.id,
    }));
  };

  const handleMinerSelection = (deviceIdentifiers: string[]) => {
    const hasSelectedMiners = deviceIdentifiers.length > 0;

    updateValues(
      (current) => ({
        ...current,
        scopeType: hasSelectedMiners ? "explicitMiners" : "wholeOrg",
        scopeId: hasSelectedMiners ? undefined : "whole-org",
        deviceSetIds: [],
        deviceIdentifiers,
      }),
      { resetResponseProfileSelection: true },
    );
  };

  const closeMaintenanceConfirmation = () => {
    setSubmitAfterMaintenanceConfirmation(null);
    setShowMaintenanceConfirmation(false);
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
    if (isSubmitDisabled) {
      return;
    }

    if (!isResponseProfileVariant && !isEditMode && values.includeMaintenance && !maintenanceInclusionConfirmed) {
      setSubmitAfterMaintenanceConfirmation("run");
      setShowMaintenanceConfirmation(true);
      return;
    }

    if (!isResponseProfileVariant && !isEditMode) {
      requestCurtailmentConfirmation("run", values);
      return;
    }

    onSubmit(values);
  };

  const requestResponseProfileCurtailment = () => {
    if (values.includeMaintenance && !maintenanceInclusionConfirmed) {
      setSubmitAfterMaintenanceConfirmation("test");
      setShowMaintenanceConfirmation(true);
      return;
    }

    requestCurtailmentConfirmation("test", values);
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
      disabled: isBusy || hasBlockingValidationError,
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

  const confirmMaintenanceInclusion = () => {
    const nextValues = resetResponseProfileSelection({ ...values, includeMaintenance: true });

    setMaintenanceInclusionConfirmed(true);
    setValues(nextValues);
    setShowMaintenanceConfirmation(false);

    if (submitAfterMaintenanceConfirmation) {
      const pendingAction = submitAfterMaintenanceConfirmation;
      setSubmitAfterMaintenanceConfirmation(null);
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
                        updateValue("curtailmentMode", value);
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
                {shouldShowCurtailBatchFields ? (
                  <div className="grid gap-3 tablet:grid-cols-2">
                    <Input
                      id="curtailment-batch-size"
                      label="Batch size (miners)"
                      initValue={values.curtailBatchSize}
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
                ) : null}
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
                  testId={isResponseProfileVariant ? "response-profile-restore-batch-size" : undefined}
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
                  testId={isResponseProfileVariant ? "response-profile-restore-batch-interval" : undefined}
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

            {!isResponseProfileVariant ? (
              <Section
                title="Apply to"
                subtext="Applies to all miners by default. Use the options below to narrow the scope."
              >
                <div className="grid">
                  <TargetSelectButton
                    label={applyToTarget.label}
                    value={applyToTarget.value}
                    disabled={isLiveCurtailmentEditMode}
                    onClick={() => setShowMinerSelectionModal(true)}
                  />
                </div>
              </Section>
            ) : null}

            <label
              className={`flex items-start gap-3 text-left ${
                isLiveCurtailmentEditMode ? "cursor-not-allowed" : "cursor-pointer"
              }`}
            >
              <Checkbox
                checked={values.includeMaintenance}
                disabled={isLiveCurtailmentEditMode}
                onChange={(event) => {
                  if (!isResponseProfileVariant && event.currentTarget.checked) {
                    setSubmitAfterMaintenanceConfirmation(null);
                    setShowMaintenanceConfirmation(true);
                    return;
                  }

                  setMaintenanceInclusionConfirmed(event.currentTarget.checked);
                  updateValue("includeMaintenance", event.currentTarget.checked);
                }}
              />
              <span>
                <span className="block text-300 text-text-primary">Include miners in maintenance</span>
              </span>
            </label>
          </section>
        }
        secondaryPane={previewPane}
        paneContainerClassName={paneContainerClassName}
        primaryPaneClassName={primaryPaneClassName}
        secondaryPaneClassName={secondaryPaneClassName}
      />
      <Dialog
        open={showMaintenanceConfirmation}
        title="Force include maintenance miners?"
        testId="curtailment-maintenance-confirmation"
        onDismiss={closeMaintenanceConfirmation}
        icon={
          <DialogIcon intent="warning">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            onClick: closeMaintenanceConfirmation,
            variant: variants.secondary,
          },
          {
            text: "Force include",
            onClick: confirmMaintenanceInclusion,
            variant: variants.danger,
          },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          This will run Curtail on miners that are currently flagged for maintenance work.
        </div>
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
          selectedMinerIds={selectedMinerIds}
          onDismiss={() => setShowMinerSelectionModal(false)}
          onSave={(minerIds) => {
            handleMinerSelection(minerIds);
            setShowMinerSelectionModal(false);
          }}
        />
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
