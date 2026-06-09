import { type ReactElement, type ReactNode, useMemo, useState } from "react";

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
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";

export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "site" | "deviceSet" | "explicitMiners";
export type ResponseProfileId = "customPlan";
export type CurtailmentMode = "fixedKwReduction" | "fullFleet";
export type MinerSelectionStrategy = "leastEfficientFirst";
export type CurtailmentStartModalMode = "create" | "edit";

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
  restoreBatchSize: string;
  restoreIntervalSec: string;
  reason: string;
  includeMaintenance: boolean;
}

export type CurtailmentSubmitValues = CurtailmentFormValues;

export interface CurtailmentPlanPreview {
  selectedMinerCount: number;
  targetKw: number;
  estimatedReductionKw: number;
  curtailEstimate: string;
  restoreEstimate: string;
  scopeLabel: string;
}

export type CurtailmentFormErrors = Partial<Record<keyof CurtailmentFormValues, string>>;

interface CurtailmentStartModalProps {
  open: boolean;
  onDismiss: () => void;
  onSubmit: (values: CurtailmentSubmitValues) => void;
  /**
   * Called from edit mode when the operator requests a curtailment stop. The
   * parent owns confirmation and the stop-curtailment RPC.
   */
  onStopCurtailment?: () => void;
  mode?: CurtailmentStartModalMode;
  initialValues?: Partial<CurtailmentFormValues>;
  errors?: CurtailmentFormErrors;
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  isSubmitting?: boolean;
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
type EditableCurtailmentField = "reason" | "maxDurationSec" | "restoreIntervalSec";

const defaultValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  siteId: "",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: "customPlan",
  curtailmentMode: "fixedKwReduction",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  reason: "",
  includeMaintenance: true,
};
const editableCurtailmentFields: EditableCurtailmentField[] = ["reason", "maxDurationSec", "restoreIntervalSec"];
const curtailmentModeOptions = [
  { value: "fixedKwReduction", label: "Fixed kW reduction" },
  { value: "fullFleet", label: "Full shutdown" },
];

function isCurtailmentMode(value: string): value is CurtailmentMode {
  return value === "fixedKwReduction" || value === "fullFleet";
}

function getInitialValues(initialValues?: Partial<CurtailmentFormValues>): CurtailmentFormValues {
  return {
    ...defaultValues,
    ...initialValues,
  };
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

    if (field === "maxDurationSec") {
      return (
        parseComparableUint32Field(values.maxDurationSec, curtailmentNumericFieldLimits.maxDurationSec) !==
        parseComparableUint32Field(initialValues.maxDurationSec, curtailmentNumericFieldLimits.maxDurationSec)
      );
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
): CurtailmentFormErrors {
  const localErrors: CurtailmentFormErrors = {};
  const isEditMode = mode === "edit";
  const maxDuration = parseOptionalUint32Field(values.maxDurationSec, {
    label: "max duration",
    max: curtailmentNumericFieldLimits.maxDurationSec,
  });
  const minDuration = parseOptionalUint32Field(isEditMode ? initialValues.minDurationSec : values.minDurationSec, {
    label: "min duration",
    max: curtailmentNumericFieldLimits.minDurationSec,
  });
  const restoreInterval = parseOptionalUint32Field(values.restoreIntervalSec, {
    label: "batch interval",
    max: curtailmentNumericFieldLimits.restoreIntervalSec,
  });

  if (values.reason.trim() === "") {
    localErrors.reason = "Enter a reason.";
  }
  if (maxDuration.error) {
    localErrors.maxDurationSec = maxDuration.error;
  }
  if (isEditMode && maxDuration.error === undefined && maxDuration.parsed === 0) {
    localErrors.maxDurationSec = "Enter max duration greater than 0.";
  }
  if (
    isEditMode &&
    maxDuration.error === undefined &&
    values.maxDurationSec.trim() === "" &&
    initialValues.maxDurationSec.trim() !== ""
  ) {
    localErrors.maxDurationSec = "Max duration cannot be cleared.";
  }
  if (restoreInterval.error) {
    localErrors.restoreIntervalSec = restoreInterval.error;
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
  if (
    minDuration.error === undefined &&
    maxDuration.error === undefined &&
    minDuration.parsed !== undefined &&
    maxDuration.parsed !== undefined &&
    minDuration.parsed > maxDuration.parsed
  ) {
    localErrors.maxDurationSec = "Max duration must be greater than or equal to min duration.";
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
  if (minDuration.error) {
    localErrors.minDurationSec = minDuration.error;
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
            {preview.curtailEstimate} duration, {preview.restoreEstimate} to restore
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
  mode = "create",
  initialValues,
  errors,
  preview,
  previewError,
  isSubmitting = false,
}: CurtailmentStartModalProps): ReactElement {
  const [initialFormValues] = useState<CurtailmentFormValues>(() => getInitialValues(initialValues));
  const [values, setValues] = useState<CurtailmentFormValues>(() => initialFormValues);
  const [showMaintenanceConfirmation, setShowMaintenanceConfirmation] = useState(false);
  const [maintenanceInclusionConfirmed, setMaintenanceInclusionConfirmed] = useState(false);
  const [submitAfterMaintenanceConfirmation, setSubmitAfterMaintenanceConfirmation] = useState(false);
  const [showMinerSelectionModal, setShowMinerSelectionModal] = useState(false);
  const [editedFields, setEditedFields] = useState<ReadonlySet<keyof CurtailmentFormValues>>(() => new Set());
  const updateValue = <Key extends keyof CurtailmentFormValues>(key: Key, value: CurtailmentFormValues[Key]) => {
    setEditedFields((current) => (current.has(key) ? current : new Set(current).add(key)));
    setValues((current) => ({ ...current, [key]: value }));
  };
  const updateValues = (updater: (current: CurtailmentFormValues) => CurtailmentFormValues) => setValues(updater);
  const isEditMode = mode === "edit";
  const localErrors = useMemo(
    () => validateCurtailmentFormValues(values, mode, initialFormValues),
    [initialFormValues, mode, values],
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
    disabled: isEditMode || controlledPreview !== undefined,
  });
  const previewState = getPreviewState({
    apiPreview,
    controlledPreview,
    isEditMode,
    unsupportedDeviceSetPreviewError,
  });

  const hasBlockingValidationError =
    previewState.previewError !== undefined ||
    previewState.isPreviewLoading ||
    Object.keys(localErrors).length > 0 ||
    Object.keys(errors ?? {}).length > 0;
  const hasEditableChanges = !isEditMode || hasEditableCurtailmentChanges(values, initialFormValues);
  const isSubmitDisabled = hasBlockingValidationError || !hasEditableChanges;
  const selectedMinerIds = getSelectedMinerIds(values);
  const applyToTarget = getApplyToTarget(values, isEditMode, previewState.preview?.selectedMinerCount);
  const isFullFleetMode = values.curtailmentMode === "fullFleet";
  const curtailmentBehaviorSubtext = isEditMode
    ? undefined
    : "Fleet will automatically curtail the least efficient miners first.";
  const curtailmentTargetGridClassName = isFullFleetMode ? "grid gap-3" : "grid gap-3 tablet:grid-cols-2";
  const shouldShowPreviewPane =
    !isEditMode || previewState.preview !== undefined || previewState.previewError !== undefined;
  const previewPane = shouldShowPreviewPane ? <PreviewPane {...previewState} /> : null;
  const useSinglePaneLayout = isEditMode && previewPane === null;
  const paneContainerClassName = useSinglePaneLayout
    ? "flex min-h-[calc(100dvh-200px)] w-full flex-1 flex-col laptop:px-10"
    : undefined;
  const primaryPaneClassName = useSinglePaneLayout ? "mx-auto w-full max-w-[720px] laptop:pl-0" : undefined;
  const secondaryPaneClassName = useSinglePaneLayout
    ? "!hidden"
    : "!hidden !bg-transparent laptop:!flex laptop:!pl-0 laptop:!rounded-[24px]";

  const handleMinerSelection = (deviceIdentifiers: string[]) => {
    const hasSelectedMiners = deviceIdentifiers.length > 0;

    updateValues((current) => ({
      ...current,
      scopeType: hasSelectedMiners ? "explicitMiners" : "wholeOrg",
      scopeId: hasSelectedMiners ? undefined : "whole-org",
      deviceSetIds: [],
      deviceIdentifiers,
    }));
  };

  const closeMaintenanceConfirmation = () => {
    setSubmitAfterMaintenanceConfirmation(false);
    setShowMaintenanceConfirmation(false);
  };

  const handleSubmit = () => {
    if (isSubmitDisabled) {
      return;
    }

    if (!isEditMode && values.includeMaintenance && !maintenanceInclusionConfirmed) {
      setSubmitAfterMaintenanceConfirmation(true);
      setShowMaintenanceConfirmation(true);
      return;
    }

    onSubmit(values);
  };

  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [];

  if (isEditMode && onStopCurtailment) {
    buttons.push({
      text: "Stop curtailment",
      variant: variants.secondaryDanger,
      onClick: onStopCurtailment,
      disabled: isSubmitting,
    });
  }

  buttons.push({
    text: isEditMode ? "Save" : "Start curtailment",
    variant: variants.primary,
    onClick: handleSubmit,
    disabled: isSubmitDisabled,
    loading: isSubmitting,
  });

  const confirmMaintenanceInclusion = () => {
    const nextValues = { ...values, includeMaintenance: true };

    setMaintenanceInclusionConfirmed(true);
    setValues(nextValues);
    setShowMaintenanceConfirmation(false);

    if (submitAfterMaintenanceConfirmation) {
      setSubmitAfterMaintenanceConfirmation(false);
      onSubmit(nextValues);
    }
  };

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title={isEditMode ? "Manage curtailment" : "Plan a curtailment"}
        closeAriaLabel={isEditMode ? "Close curtailment editor" : "Close curtailment planner"}
        onDismiss={onDismiss}
        isBusy={isSubmitting}
        buttons={buttons}
        abovePanes={previewPane ? <div className="px-6 pb-6 laptop:hidden">{previewPane}</div> : null}
        primaryPane={
          <section className="flex flex-col gap-12 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
            <Input
              id="curtailment-reason"
              label="Reason"
              initValue={values.reason}
              type="text"
              error={effectiveErrors.reason}
              onChange={(value) => updateValue("reason", value)}
            />

            <Section title="Curtail behavior" subtext={curtailmentBehaviorSubtext}>
              <div className="grid gap-3">
                <div className={curtailmentTargetGridClassName}>
                  <Select
                    id="curtailment-mode"
                    label="Curtailment mode"
                    value={values.curtailmentMode}
                    options={curtailmentModeOptions}
                    disabled={isEditMode}
                    forceBelow
                    showSelectedIndicator={false}
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
                      disabled={isEditMode}
                      inputMode="decimal"
                      error={effectiveErrors.targetKw}
                      onChange={(value) => updateValue("targetKw", value)}
                    />
                  ) : null}
                </div>
                <div className="grid gap-3 tablet:grid-cols-2">
                  <Input
                    id="curtailment-min-duration"
                    label="Min duration (sec)"
                    initValue={values.minDurationSec}
                    disabled={isEditMode}
                    inputMode="numeric"
                    error={effectiveErrors.minDurationSec}
                    onChange={(value) => updateValue("minDurationSec", value)}
                  />
                  <Input
                    id="curtailment-max-duration"
                    label="Max duration (sec)"
                    initValue={values.maxDurationSec}
                    inputMode="numeric"
                    error={effectiveErrors.maxDurationSec}
                    onChange={(value) => updateValue("maxDurationSec", value)}
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
                  disabled={isEditMode}
                  inputMode="numeric"
                  error={effectiveErrors.restoreBatchSize}
                  onChange={(value) => updateValue("restoreBatchSize", value)}
                />
                <Input
                  id="curtailment-restore-batch-interval"
                  label="Batch interval (sec)"
                  initValue={values.restoreIntervalSec}
                  inputMode="numeric"
                  error={effectiveErrors.restoreIntervalSec}
                  onChange={(value) => updateValue("restoreIntervalSec", value)}
                />
              </div>
            </Section>

            <Section title="Apply to">
              <div className="grid">
                <TargetSelectButton
                  label={applyToTarget.label}
                  value={applyToTarget.value}
                  disabled={isEditMode}
                  onClick={() => setShowMinerSelectionModal(true)}
                />
              </div>
            </Section>

            <label
              className={`flex items-start gap-3 text-left ${isEditMode ? "cursor-not-allowed" : "cursor-pointer"}`}
            >
              <Checkbox
                checked={values.includeMaintenance}
                disabled={isEditMode}
                onChange={(event) => {
                  if (event.currentTarget.checked) {
                    setSubmitAfterMaintenanceConfirmation(false);
                    setShowMaintenanceConfirmation(true);
                    return;
                  }

                  setMaintenanceInclusionConfirmed(false);
                  updateValue("includeMaintenance", false);
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
