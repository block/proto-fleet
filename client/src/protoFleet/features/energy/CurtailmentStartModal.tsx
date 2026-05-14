import { type ReactNode, useEffect, useState } from "react";
import clsx from "clsx";

import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import GroupSelectionModal from "@/protoFleet/features/settings/components/Schedules/GroupSelectionModal";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import RackSelectionModal from "@/protoFleet/features/settings/components/Schedules/RackSelectionModal";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export type CurtailmentMode = "fixedKw";
export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "deviceSet" | "explicitMiners";

export interface CurtailmentCandidate {
  deviceIdentifier: string;
  currentPowerW: number;
  efficiencyJth: number;
  reasonSelected: string;
}

export interface CurtailmentSkippedCandidate {
  deviceIdentifier: string;
  reason: string;
  currentPowerW?: number;
}

export interface CurtailmentPlanPreview {
  mode: CurtailmentMode;
  targetKw: number;
  toleranceKw?: number;
  estimatedReductionKw: number;
  estimatedRemainingPowerKw: number;
  preEventPowerKw: number;
  selectedCandidateCount: number;
  eligibleCandidateCount: number;
  selectedCandidates: CurtailmentCandidate[];
  skippedCandidates: CurtailmentSkippedCandidate[];
}

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
  deviceSetIds: string[];
  deviceIdentifiers: string[];
  targetKw: string;
  toleranceKw: string;
  priority: CurtailmentPriority;
  minCurtailedDurationSec: string;
  maxDurationSec: string;
  restoreBatchSize: string;
  restoreBatchIntervalSec: string;
  includeMaintenance: boolean;
  forceIncludeMaintenance: boolean;
  reason: string;
}

interface CurtailmentStartModalProps {
  open: boolean;
  onDismiss: () => void;
  onPreviewCurtailmentPlan: (values: CurtailmentFormValues) => Promise<CurtailmentPlanPreview>;
  onStartCurtailment: (values: CurtailmentFormValues) => Promise<unknown>;
  onStarted?: () => void;
  initialValues?: CurtailmentFormValues;
}

type TouchedFields = Partial<Record<keyof CurtailmentFormValues, true>>;
type DeviceSetScopeId = "racks" | "groups";

interface SectionProps {
  title: string;
  children: ReactNode;
}

interface NumberFieldProps {
  id: string;
  label: string;
  value: string;
  units?: string;
  error?: string;
  onChange: (value: string) => void;
}

interface TextFieldProps {
  id: string;
  label: string;
  value: string;
  error?: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

interface SelectFieldProps {
  id: string;
  label: string;
  value: CurtailmentPriority;
  onChange: (value: CurtailmentPriority) => void;
}

interface PreviewPaneMessageProps {
  children: ReactNode;
  icon?: ReactNode;
  emptyState?: boolean;
}

interface CurtailmentPreviewPaneProps {
  preview?: CurtailmentPlanPreview;
  scopeSummary: string;
  restoreEstimate?: RestoreEstimate;
  isPreviewLoading: boolean;
  displayPreviewError?: string;
  isPreviewBlocked: boolean;
}

interface SubmitCurtailmentOptions {
  onSubmit: () => Promise<unknown>;
  successMessage: string;
  errorMessage: string;
}

interface RestoreEstimate {
  batchCount: number;
  totalSeconds: number;
}

interface TargetSelectButtonProps {
  label: string;
  value: string;
  onClick: () => void;
}

const inputFrameClassName =
  "flex min-h-14 w-full items-center gap-2 rounded-xl border border-border-5 bg-surface-base px-4 py-1";
const sectionTitleClassName = "text-emphasis-300 text-text-primary";
const sectionBodyClassName = "grid gap-3";
const priorityLabels: Record<CurtailmentPriority, string> = {
  normal: "Normal",
  emergency: "Emergency",
};
const priorityOptions = [
  { value: "normal", label: priorityLabels.normal },
  { value: "emergency", label: priorityLabels.emergency },
];
const targetSelectPlaceholderLabel = "Select";
const defaultCurtailmentFormValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  minCurtailedDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "",
  restoreBatchIntervalSec: "",
  includeMaintenance: false,
  forceIncludeMaintenance: false,
  reason: "",
};

type OptionalWholeNumberField =
  | "minCurtailedDurationSec"
  | "maxDurationSec"
  | "restoreBatchSize"
  | "restoreBatchIntervalSec";

const optionalWholeNumberFields: Array<{ valueKey: OptionalWholeNumberField; message: string }> = [
  {
    valueKey: "minCurtailedDurationSec",
    message: "Minimum curtailed duration must be a whole number 0 or greater.",
  },
  {
    valueKey: "maxDurationSec",
    message: "Maximum duration must be a whole number 0 or greater.",
  },
  {
    valueKey: "restoreBatchSize",
    message: "Restore batch size must be a whole number 0 or greater.",
  },
  {
    valueKey: "restoreBatchIntervalSec",
    message: "Restore interval must be a whole number 0 or greater.",
  },
];

function Section({ title, children }: SectionProps) {
  return (
    <section className={sectionBodyClassName}>
      <div className={sectionTitleClassName}>{title}</div>
      <div>{children}</div>
    </section>
  );
}

function FieldError({ id, error }: { id: string; error?: string }) {
  if (!error) {
    return null;
  }

  return (
    <span id={`${id}-error`} className="mt-2 block text-200 text-intent-critical-fill">
      {error}
    </span>
  );
}

function NumberField({ id, label, value, units, error, onChange }: NumberFieldProps) {
  const hasValue = value.trim().length > 0;

  return (
    <label htmlFor={id} className="block">
      <span className={inputFrameClassName}>
        <span className="flex min-w-0 flex-1 flex-col justify-center">
          <span className={hasValue ? "text-200 text-text-primary-50" : "sr-only"}>{label}</span>
          <span className="flex min-w-0 items-center">
            <input
              id={id}
              type="number"
              className="no-spinner min-w-0 bg-transparent text-300 text-text-primary outline-hidden placeholder:text-text-primary-50"
              style={hasValue ? { width: `${Math.max(value.length, 1) + 0.5}ch` } : undefined}
              value={value}
              placeholder={label}
              onChange={(event) => onChange(event.currentTarget.value)}
              aria-label={label}
              aria-invalid={!!error || undefined}
              aria-describedby={error ? `${id}-error` : undefined}
              autoComplete="new-password"
            />
            {units && hasValue ? <span className="shrink-0 text-300 text-text-primary">{units}</span> : null}
          </span>
        </span>
      </span>
      <FieldError id={id} error={error} />
    </label>
  );
}

function TextField({ id, label, value, error, onChange, placeholder }: TextFieldProps) {
  const hasValue = value.trim().length > 0;

  return (
    <label htmlFor={id} className="block">
      <span className={inputFrameClassName}>
        <span className="flex min-w-0 flex-1 flex-col justify-center">
          <span className={hasValue ? "text-200 text-text-primary-50" : "sr-only"}>{label}</span>
          <input
            id={id}
            type="text"
            className="min-w-0 bg-transparent text-300 text-text-primary outline-hidden placeholder:text-text-primary-50"
            value={value}
            placeholder={placeholder ?? label}
            onChange={(event) => onChange(event.currentTarget.value)}
            aria-label={label}
            aria-invalid={!!error || undefined}
            aria-describedby={error ? `${id}-error` : undefined}
            autoComplete="new-password"
          />
        </span>
      </span>
      <FieldError id={id} error={error} />
    </label>
  );
}

function SelectField({ id, label, value, onChange }: SelectFieldProps) {
  return (
    <Select
      id={id}
      label={label}
      value={value}
      className="max-w-[274px]"
      options={priorityOptions}
      onChange={(nextValue) => onChange(nextValue as CurtailmentPriority)}
    />
  );
}

function TargetSelectButton({ label, value, onClick }: TargetSelectButtonProps) {
  const isPlaceholder = value === targetSelectPlaceholderLabel;

  return (
    <button
      type="button"
      onClick={onClick}
      className="relative flex h-14 w-full items-center justify-between rounded-lg border border-border-5 bg-surface-base pr-4 pl-4 text-left outline-hidden"
    >
      <div className="flex min-w-0 flex-col pt-[18px]">
        <span className="absolute top-[7px] text-200 text-text-primary-50">{label}</span>
        <div className={clsx("truncate text-300", isPlaceholder ? "text-text-primary-50" : "text-text-primary")}>
          {value}
        </div>
      </div>
      <ChevronDown width="w-3" className="shrink-0 text-text-primary-70" />
    </button>
  );
}

function parseOptionalNumber(value: string): number | undefined {
  if (!value.trim()) {
    return undefined;
  }

  return Number(value);
}

function isBlankOrNonNegativeInteger(value: string): boolean {
  if (!value.trim()) {
    return true;
  }

  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= 0;
}

function validateCurtailmentForm(
  values: CurtailmentFormValues,
  { requireReason }: { requireReason: boolean },
): string | undefined {
  const targetKw = Number(values.targetKw);
  const toleranceKw = parseOptionalNumber(values.toleranceKw);

  if (!Number.isFinite(targetKw) || targetKw <= 0) {
    return "Target reduction must be greater than 0.";
  }

  if (toleranceKw !== undefined && (!Number.isFinite(toleranceKw) || toleranceKw < 0)) {
    return "Tolerance must be 0 or greater.";
  }

  if (values.includeMaintenance !== values.forceIncludeMaintenance) {
    return "Maintenance override requires confirmation.";
  }

  for (const { valueKey, message } of optionalWholeNumberFields) {
    if (!isBlankOrNonNegativeInteger(values[valueKey])) {
      return message;
    }
  }

  if (requireReason && !values.reason.trim()) {
    return "Reason is required.";
  }

  return undefined;
}

function parsePositiveInteger(value: string): number | undefined {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function getRestoreEstimate({
  selectedCandidateCount,
  restoreBatchSize,
  restoreBatchIntervalSec,
}: {
  selectedCandidateCount: number;
  restoreBatchSize: string;
  restoreBatchIntervalSec: string;
}): RestoreEstimate | undefined {
  const batchSize = parsePositiveInteger(restoreBatchSize);
  const intervalSec = parsePositiveInteger(restoreBatchIntervalSec);

  if (batchSize === undefined || intervalSec === undefined || selectedCandidateCount <= 0) {
    return undefined;
  }

  const batchCount = Math.ceil(selectedCandidateCount / batchSize);

  return {
    batchCount,
    totalSeconds: Math.max(batchCount - 1, 0) * intervalSec,
  };
}

function formatKwValue(value: number): string {
  return value.toLocaleString(undefined, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  });
}

function formatPreviewRestoreEstimate(estimate?: RestoreEstimate): string {
  if (!estimate) {
    return "Unavailable";
  }

  if (estimate.totalSeconds === 0) {
    return "Immediate";
  }

  const minutes = Math.max(Math.round(estimate.totalSeconds / 60), 1);

  return `~${minutes} ${minutes === 1 ? "minute" : "minutes"}`;
}

function formatKw(value: number, fractionDigits = 1): string {
  return `${value.toLocaleString(undefined, {
    maximumFractionDigits: fractionDigits,
    minimumFractionDigits: fractionDigits,
  })} kW`;
}

function getTargetButtonLabel(count: number, singular: string): string {
  if (count === 0) {
    return targetSelectPlaceholderLabel;
  }

  const noun = count === 1 ? singular : `${singular}s`;
  return `${count} ${noun}`;
}

function ReductionProgressBar({ value, max }: { value: number; max: number }) {
  const width = max > 0 ? Math.min(Math.max((value / max) * 100, 0), 100) : 0;

  return (
    <div className="flex h-3 w-full gap-1 overflow-hidden">
      <div className="rounded-full bg-core-accent-fill" style={{ width: `${width}%` }} />
      <div className="min-w-0 flex-1 rounded-full bg-core-primary-20" />
    </div>
  );
}

function PreviewPaneMessage({ children, icon, emptyState = false }: PreviewPaneMessageProps) {
  function renderContent(className: string): ReactNode {
    return (
      <div className={className}>
        {icon}
        <div>{children}</div>
      </div>
    );
  }

  if (emptyState) {
    return (
      <div className="flex min-h-40 flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-6 py-10 text-center text-300 text-text-primary-70 laptop:px-16">
        {children}
      </div>
    );
  }

  const mobileClassName = icon
    ? "flex max-w-[360px] gap-2 text-300 text-text-primary-70"
    : "text-300 text-text-primary-50";
  const desktopClassName = icon
    ? "flex max-w-[420px] gap-2 text-300 text-text-primary-70"
    : "max-w-[420px] text-300 text-text-primary-70";

  return (
    <>
      <div className="flex min-h-16 items-center justify-center px-6 py-4 laptop:hidden">
        {renderContent(mobileClassName)}
      </div>

      <div className="hidden flex-col justify-center px-16 pt-6 pb-4 laptop:flex laptop:flex-1">
        {renderContent(desktopClassName)}
      </div>
    </>
  );
}

function CurtailmentPreviewPane({
  preview,
  scopeSummary,
  restoreEstimate,
  isPreviewLoading,
  displayPreviewError,
  isPreviewBlocked,
}: CurtailmentPreviewPaneProps) {
  if (displayPreviewError) {
    return (
      <PreviewPaneMessage icon={<Alert className="mt-0.5 shrink-0 text-text-primary-50" width="w-4" />}>
        {displayPreviewError}
      </PreviewPaneMessage>
    );
  }

  if (!preview || isPreviewBlocked) {
    const emptyStateText = isPreviewLoading
      ? "Loading curtailment preview."
      : "Configure your curtailment to see a preview.";

    return <PreviewPaneMessage emptyState>{emptyStateText}</PreviewPaneMessage>;
  }

  const restoreEstimateLabel = formatPreviewRestoreEstimate(restoreEstimate);

  return (
    <div className="flex min-h-[360px] flex-1 items-center justify-center rounded-[24px] bg-surface-overlay px-8 py-12 laptop:min-h-0 laptop:px-16 laptop:py-6">
      <div className="flex w-full max-w-[520px] flex-col gap-10">
        <div className="text-heading-300 text-text-primary">
          Curtail {preview.selectedCandidateCount} miners {scopeSummary} immediately
        </div>

        <div className="grid gap-3">
          <div>
            <div className="text-emphasis-200 text-text-primary-70">Target reduction</div>
            <div className="text-heading-300 text-text-primary">
              {formatKwValue(preview.estimatedReductionKw)} of {formatKw(preview.targetKw)}
            </div>
          </div>
          <ReductionProgressBar value={preview.estimatedReductionKw} max={preview.targetKw} />
        </div>

        <div>
          <div className="text-emphasis-200 text-text-primary-70">Time to restore</div>
          <div className="text-heading-300 text-text-primary">{restoreEstimateLabel}</div>
        </div>
      </div>
    </div>
  );
}

function pluralize(count: number, singular: string): string {
  return `${count} ${singular}${count === 1 ? "" : "s"}`;
}

function getDeviceSetScopeSummary(values: CurtailmentFormValues): string {
  const deviceSetCount = values.deviceSetIds.length;

  if (values.scopeId === "racks") {
    return deviceSetCount > 0 ? `in ${pluralize(deviceSetCount, "rack")}` : "in racks";
  }

  if (values.scopeId === "groups") {
    return deviceSetCount > 0 ? `in ${pluralize(deviceSetCount, "group")}` : "in groups";
  }

  return deviceSetCount > 0 ? `in ${pluralize(deviceSetCount, "device set")}` : "in device sets";
}

function getPreviewScopeSummary(values: CurtailmentFormValues): string {
  switch (values.scopeType) {
    case "wholeOrg":
      return "across the fleet";
    case "explicitMiners":
      return values.deviceIdentifiers.length > 0
        ? `from ${pluralize(values.deviceIdentifiers.length, "selected miner")}`
        : "from selected miners";
    case "deviceSet":
      return getDeviceSetScopeSummary(values);
  }
}

function getSelectedDeviceSetIds(values: CurtailmentFormValues, scopeId: DeviceSetScopeId): string[] {
  if (values.scopeType !== "deviceSet" || values.scopeId !== scopeId) {
    return [];
  }

  return values.deviceSetIds;
}

function getSelectedMinerIds(values: CurtailmentFormValues): string[] {
  if (values.scopeType !== "explicitMiners") {
    return [];
  }

  return values.deviceIdentifiers;
}

function CurtailmentStartModal({
  open,
  onDismiss,
  onPreviewCurtailmentPlan,
  onStartCurtailment,
  onStarted,
  initialValues,
}: CurtailmentStartModalProps) {
  const [values, setValues] = useState<CurtailmentFormValues>(() => initialValues ?? defaultCurtailmentFormValues);
  const [preview, setPreview] = useState<CurtailmentPlanPreview>();
  const [previewError, setPreviewError] = useState<string>();
  const [isPreviewLoading, setIsPreviewLoading] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showMaintenanceConfirmation, setShowMaintenanceConfirmation] = useState(false);
  const [showRackSelectionModal, setShowRackSelectionModal] = useState(false);
  const [showGroupSelectionModal, setShowGroupSelectionModal] = useState(false);
  const [showMinerSelectionModal, setShowMinerSelectionModal] = useState(false);
  const [touchedFields, setTouchedFields] = useState<TouchedFields>({});
  const [showAllErrors, setShowAllErrors] = useState(false);
  const previewScopeSummary = getPreviewScopeSummary(values);
  const previewValidationError = validateCurtailmentForm(values, { requireReason: false });
  const selectedTargets = {
    racks: getSelectedDeviceSetIds(values, "racks"),
    groups: getSelectedDeviceSetIds(values, "groups"),
    miners: getSelectedMinerIds(values),
  };

  useEffect(() => {
    if (!open || previewValidationError) {
      return;
    }

    let isSubscribed = true;

    const refreshPreview = async () => {
      if (isSubscribed) {
        setIsPreviewLoading(true);
        setPreviewError(undefined);
      }

      try {
        const nextPreview = await onPreviewCurtailmentPlan(values);

        if (isSubscribed) {
          setPreview(nextPreview);
        }
      } catch (error) {
        if (isSubscribed) {
          setPreview(undefined);
          setPreviewError(error instanceof Error ? error.message : "Failed to preview curtailment.");
        }
      } finally {
        if (isSubscribed) {
          setIsPreviewLoading(false);
        }
      }
    };

    void refreshPreview();

    return () => {
      isSubscribed = false;
    };
  }, [open, onPreviewCurtailmentPlan, previewValidationError, values]);

  if (!open) {
    return null;
  }

  const restoreEstimate = preview
    ? getRestoreEstimate({
        selectedCandidateCount: preview.selectedCandidateCount,
        restoreBatchSize: values.restoreBatchSize,
        restoreBatchIntervalSec: values.restoreBatchIntervalSec,
      })
    : undefined;

  const setFormValue = <Key extends keyof CurtailmentFormValues>(key: Key, value: CurtailmentFormValues[Key]) => {
    setTouchedFields((current) => ({ ...current, [key]: true }));
    setValues((current) => ({ ...current, [key]: value }));
  };

  const shouldShowError = (field: keyof CurtailmentFormValues): boolean =>
    Boolean(showAllErrors || touchedFields[field]);
  const targetKw = Number(values.targetKw);
  const targetError = shouldShowError("targetKw") && (!targetKw || targetKw <= 0) ? "Required" : undefined;
  const reasonError = shouldShowError("reason") && !values.reason.trim() ? "Reason is required" : undefined;

  const handleDeviceSetSelection = (deviceSetIds: string[], scopeId: DeviceSetScopeId) => {
    const hasSelectedDeviceSets = deviceSetIds.length > 0;

    setValues((current) => ({
      ...current,
      scopeType: hasSelectedDeviceSets ? "deviceSet" : "wholeOrg",
      scopeId: hasSelectedDeviceSets ? scopeId : "whole-org",
      deviceSetIds,
      deviceIdentifiers: [],
    }));
  };

  const handleMinerSelection = (deviceIdentifiers: string[]) => {
    const hasSelectedMiners = deviceIdentifiers.length > 0;

    setValues((current) => ({
      ...current,
      scopeType: hasSelectedMiners ? "explicitMiners" : "wholeOrg",
      scopeId: hasSelectedMiners ? undefined : "whole-org",
      deviceSetIds: [],
      deviceIdentifiers,
    }));
  };

  const submitCurtailment = async ({ onSubmit, successMessage, errorMessage }: SubmitCurtailmentOptions) => {
    if (isSubmitting) {
      return;
    }

    const validationError = validateCurtailmentForm(values, { requireReason: true });
    setShowAllErrors(true);

    if (validationError) {
      setPreviewError(validationError);
      return;
    }

    setIsSubmitting(true);

    try {
      await onSubmit();
      pushToast({
        message: successMessage,
        status: STATUSES.success,
      });
      onDismiss();
    } catch (error) {
      pushToast({
        message: error instanceof Error ? error.message : errorMessage,
        status: STATUSES.error,
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleStart = async () => {
    await submitCurtailment({
      onSubmit: async () => {
        await onStartCurtailment(values);
        onStarted?.();
      },
      successMessage: "Curtailment started.",
      errorMessage: "Failed to start curtailment.",
    });
  };

  const handlePrimaryAction = () => {
    void handleStart();
  };

  const previewPane = (
    <CurtailmentPreviewPane
      preview={preview}
      scopeSummary={previewScopeSummary}
      restoreEstimate={restoreEstimate}
      isPreviewLoading={isPreviewLoading}
      displayPreviewError={previewError}
      isPreviewBlocked={Boolean(previewValidationError)}
    />
  );

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title="Plan a curtailment"
        closeAriaLabel="Close curtailment planner"
        onDismiss={onDismiss}
        buttons={[
          {
            text: "Start curtailment",
            variant: variants.primary,
            onClick: handlePrimaryAction,
            loading: isSubmitting,
          },
        ]}
        abovePanes={<div className="px-6 pb-6 laptop:hidden">{previewPane}</div>}
        primaryPane={
          <section className="flex flex-col gap-10 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
            <Section title="Details">
              <div className="grid gap-3">
                <div className="grid gap-3 tablet:grid-cols-2">
                  <NumberField
                    id="curtailment-target-kw"
                    label="Target reduction"
                    value={values.targetKw}
                    units="kW"
                    error={targetError}
                    onChange={(value) => setFormValue("targetKw", value)}
                  />
                  <NumberField
                    id="curtailment-tolerance-kw"
                    label="Tolerance"
                    value={values.toleranceKw}
                    units="kW"
                    onChange={(value) => setFormValue("toleranceKw", value)}
                  />
                </div>

                <SelectField
                  id="curtailment-priority"
                  label="Priority"
                  value={values.priority}
                  onChange={(value) => setFormValue("priority", value)}
                />
              </div>
            </Section>

            <Section title="Safety and restore">
              <div className="grid gap-3">
                <div className="grid gap-3 tablet:grid-cols-2">
                  <NumberField
                    id="curtailment-min-duration"
                    label="Min duration"
                    value={values.minCurtailedDurationSec}
                    units="sec"
                    onChange={(value) => setFormValue("minCurtailedDurationSec", value)}
                  />
                  <NumberField
                    id="curtailment-max-duration"
                    label="Max duration"
                    value={values.maxDurationSec}
                    units="sec"
                    onChange={(value) => setFormValue("maxDurationSec", value)}
                  />
                  <NumberField
                    id="curtailment-batch-size"
                    label="Restore batch size"
                    value={values.restoreBatchSize}
                    units="miners"
                    onChange={(value) => setFormValue("restoreBatchSize", value)}
                  />
                  <NumberField
                    id="curtailment-batch-interval"
                    label="Restore interval"
                    value={values.restoreBatchIntervalSec}
                    units="sec"
                    onChange={(value) => setFormValue("restoreBatchIntervalSec", value)}
                  />
                </div>

                {restoreEstimate ? (
                  <div className="text-200 text-text-primary-50">
                    Estimated time to restore {formatPreviewRestoreEstimate(restoreEstimate)}
                  </div>
                ) : null}

                <TextField
                  id="curtailment-reason"
                  label="Reason"
                  value={values.reason}
                  placeholder="Label"
                  onChange={(value) => setFormValue("reason", value)}
                  error={reasonError}
                />
              </div>
            </Section>

            <Section title="Apply to">
              <div className="grid gap-4 tablet:grid-cols-3">
                <TargetSelectButton
                  label="Racks"
                  value={getTargetButtonLabel(selectedTargets.racks.length, "rack")}
                  onClick={() => setShowRackSelectionModal(true)}
                />
                <TargetSelectButton
                  label="Groups"
                  value={getTargetButtonLabel(selectedTargets.groups.length, "group")}
                  onClick={() => setShowGroupSelectionModal(true)}
                />
                <TargetSelectButton
                  label="Miners"
                  value={getTargetButtonLabel(selectedTargets.miners.length, "miner")}
                  onClick={() => setShowMinerSelectionModal(true)}
                />
              </div>
            </Section>

            <label className="flex cursor-pointer items-start gap-3 text-left">
              <Checkbox
                checked={values.includeMaintenance}
                onChange={(event) => {
                  if (event.currentTarget.checked) {
                    setShowMaintenanceConfirmation(true);
                    return;
                  }

                  setValues((current) => ({
                    ...current,
                    includeMaintenance: false,
                    forceIncludeMaintenance: false,
                  }));
                }}
              />
              <span>
                <span className="block text-300 text-text-primary">Include miners in maintenance</span>
                <span className="block text-200 text-text-primary-70">Requires explicit force acknowledgement</span>
              </span>
            </label>
          </section>
        }
        secondaryPane={previewPane}
        secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0 laptop:!rounded-[24px]"
      />
      <Dialog
        open={showMaintenanceConfirmation}
        title="Force include maintenance miners?"
        testId="curtailment-maintenance-confirmation"
        onDismiss={() => setShowMaintenanceConfirmation(false)}
        icon={
          <DialogIcon intent="warning">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            onClick: () => setShowMaintenanceConfirmation(false),
            variant: variants.secondary,
          },
          {
            text: "Force include",
            onClick: () => {
              setValues((current) => ({
                ...current,
                includeMaintenance: true,
                forceIncludeMaintenance: true,
              }));
              setShowMaintenanceConfirmation(false);
            },
            variant: variants.danger,
          },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          This will run Curtail on miners that are currently flagged for maintenance work.
        </div>
      </Dialog>

      {showRackSelectionModal ? (
        <RackSelectionModal
          open={showRackSelectionModal}
          selectedRackIds={selectedTargets.racks}
          onDismiss={() => setShowRackSelectionModal(false)}
          onSave={(rackIds) => {
            handleDeviceSetSelection(rackIds, "racks");
            setShowRackSelectionModal(false);
          }}
        />
      ) : null}
      {showGroupSelectionModal ? (
        <GroupSelectionModal
          open={showGroupSelectionModal}
          selectedGroupIds={selectedTargets.groups}
          onDismiss={() => setShowGroupSelectionModal(false)}
          onSave={(groupIds) => {
            handleDeviceSetSelection(groupIds, "groups");
            setShowGroupSelectionModal(false);
          }}
        />
      ) : null}
      {showMinerSelectionModal ? (
        <MinerSelectionModal
          open={showMinerSelectionModal}
          selectedMinerIds={selectedTargets.miners}
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

export default CurtailmentStartModal;
