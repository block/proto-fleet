import { type ReactElement, type ReactNode, useMemo, useState } from "react";

import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import TargetSelectButton, { getTargetButtonLabel } from "@/protoFleet/components/TargetSelectButton";
import {
  getUnsupportedDeviceSetPreviewError,
  useCurtailmentPlanPreview,
} from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import GroupSelectionModal from "@/protoFleet/features/settings/components/Schedules/GroupSelectionModal";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import RackSelectionModal from "@/protoFleet/features/settings/components/Schedules/RackSelectionModal";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select, { type SelectOption, type SelectProps } from "@/shared/components/Select";

export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "deviceSet" | "explicitMiners";
export type ResponseProfileId = "customPlan";
export type CurtailmentMode = "fixedKwReduction";
export type MinerSelectionStrategy = "leastEfficientFirst";
export type CurtailmentStartModalMode = "create" | "edit";

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
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

interface FieldProps {
  id: string;
  label: string;
  value: string;
  units?: string;
  type?: "number" | "text";
  error?: string;
  onChange: (value: string) => void;
}

interface SectionProps {
  title: string;
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

interface TypedSelectOption<Value extends string> extends SelectOption {
  value: Value;
}

interface TypedSelectProps<Value extends string> extends Pick<SelectProps, "className" | "disabled" | "testId"> {
  id: string;
  label: string;
  value: Value;
  options: Array<TypedSelectOption<Value>>;
  error?: string;
  onChange: (value: Value) => void;
}

type DeviceSetScopeId = "racks" | "groups";

const defaultValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
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

const responseProfileOptions: Array<TypedSelectOption<ResponseProfileId>> = [
  { value: "customPlan", label: "Custom plan" },
];

const curtailmentModeOptions: Array<TypedSelectOption<CurtailmentMode>> = [
  { value: "fixedKwReduction", label: "Fixed kW reduction" },
];

const minerSelectionStrategyOptions: Array<TypedSelectOption<MinerSelectionStrategy>> = [
  { value: "leastEfficientFirst", label: "Least efficient first" },
];

const maxCurtailmentDurationSec = 2147483647;

function getInitialValues(initialValues?: Partial<CurtailmentFormValues>): CurtailmentFormValues {
  return {
    ...defaultValues,
    ...initialValues,
  };
}

function getInitialValuesKey(initialValues?: Partial<CurtailmentFormValues>): string {
  return Object.entries(getInitialValues(initialValues))
    .map(([key, value]) => `${key}:${String(value)}`)
    .join("|");
}

function parseDurationField(value: string): { parsed?: number; error?: string } {
  const trimmed = value.trim();
  if (trimmed === "") {
    return {};
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) {
    return { error: "Enter a whole number of seconds." };
  }

  if (parsed < 0) {
    return { error: "Enter 0 or more seconds." };
  }

  if (parsed > maxCurtailmentDurationSec) {
    return { error: `Enter ${maxCurtailmentDurationSec.toLocaleString()} seconds or less.` };
  }

  return { parsed };
}

function validateCurtailmentFormValues(values: CurtailmentFormValues): CurtailmentFormErrors {
  const localErrors: CurtailmentFormErrors = {};
  const minDuration = parseDurationField(values.minDurationSec);
  const maxDuration = parseDurationField(values.maxDurationSec);

  if (minDuration.error) {
    localErrors.minDurationSec = minDuration.error;
  }
  if (maxDuration.error) {
    localErrors.maxDurationSec = maxDuration.error;
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

  return localErrors;
}

function isSelectOptionValue<Value extends string>(
  options: Array<TypedSelectOption<Value>>,
  value: string,
): value is Value {
  return options.some((option) => option.value === value);
}

function Field({ id, label, value, units, type = "number", error, onChange }: FieldProps): ReactElement {
  return <Input id={id} label={label} initValue={value} units={units} type={type} error={error} onChange={onChange} />;
}

function TypedSelect<Value extends string>({
  id,
  label,
  value,
  options,
  error,
  className,
  disabled,
  testId,
  onChange,
}: TypedSelectProps<Value>): ReactElement {
  return (
    <Select
      id={id}
      label={label}
      value={value}
      options={options}
      error={error}
      className={className}
      disabled={disabled}
      testId={testId}
      onChange={(nextValue) => {
        if (isSelectOptionValue(options, nextValue)) {
          onChange(nextValue);
        }
      }}
    />
  );
}

function Section({ title, children }: SectionProps): ReactElement {
  return (
    <section className="grid gap-3">
      <div className="text-emphasis-300 text-text-primary">{title}</div>
      {children}
    </section>
  );
}

function formatKw(value: number): string {
  return `${value.toLocaleString(undefined, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  })} kW`;
}

function clampPercentage(value: number): number {
  return Math.min(Math.max(value, 0), 100);
}

function ReductionProgressBar({ value, max }: ReductionProgressBarProps): ReactElement {
  const reductionPercentage = max > 0 ? clampPercentage((value / max) * 100) : 0;

  return (
    <div className="flex h-3 w-full gap-1 overflow-hidden">
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
      <div className="flex w-full max-w-[520px] flex-col gap-10">
        <div className="text-heading-300 text-text-primary">
          Curtail {preview.selectedMinerCount} miners {preview.scopeLabel} immediately
        </div>

        <div className="grid gap-3">
          <div>
            <div className="text-emphasis-200 text-text-primary-70">Target reduction</div>
            <div className="text-heading-300 text-text-primary">
              {formatKw(preview.estimatedReductionKw)} of {formatKw(preview.targetKw)}
            </div>
          </div>
          <ReductionProgressBar value={preview.estimatedReductionKw} max={preview.targetKw} />
        </div>

        <div className="grid gap-6">
          <div>
            <div className="text-emphasis-200 text-text-primary-70">Curtailment duration</div>
            <div className="text-heading-300 text-text-primary">{preview.curtailEstimate}</div>
          </div>
          <div>
            <div className="text-emphasis-200 text-text-primary-70">Time to restore</div>
            <div className="text-heading-300 text-text-primary">{preview.restoreEstimate}</div>
          </div>
        </div>
      </div>
    </div>
  );
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
  const [values, setValues] = useState<CurtailmentFormValues>(() => getInitialValues(initialValues));
  const [showMaintenanceConfirmation, setShowMaintenanceConfirmation] = useState(false);
  const [maintenanceInclusionConfirmed, setMaintenanceInclusionConfirmed] = useState(false);
  const [submitAfterMaintenanceConfirmation, setSubmitAfterMaintenanceConfirmation] = useState(false);
  const [showRackSelectionModal, setShowRackSelectionModal] = useState(false);
  const [showGroupSelectionModal, setShowGroupSelectionModal] = useState(false);
  const [showMinerSelectionModal, setShowMinerSelectionModal] = useState(false);
  const updateValue = <Key extends keyof CurtailmentFormValues>(key: Key, value: CurtailmentFormValues[Key]) =>
    setValues((current) => ({ ...current, [key]: value }));
  const updateValues = (updater: (current: CurtailmentFormValues) => CurtailmentFormValues) => setValues(updater);
  const localErrors = useMemo(() => validateCurtailmentFormValues(values), [values]);
  const effectiveErrors = { ...errors, ...localErrors };
  const unsupportedDeviceSetPreviewError = getUnsupportedDeviceSetPreviewError(values);
  const hasControlledPreview = preview !== undefined || previewError !== undefined;
  const apiPreview = useCurtailmentPlanPreview({
    open,
    values,
    disabled: hasControlledPreview,
  });
  let previewState: PreviewPaneProps = apiPreview;

  if (hasControlledPreview) {
    previewState = { preview, previewError, isPreviewLoading: false };
  }

  if (unsupportedDeviceSetPreviewError) {
    previewState = { preview: undefined, previewError: unsupportedDeviceSetPreviewError, isPreviewLoading: false };
  }

  const hasBlockingValidationError =
    previewState.previewError !== undefined ||
    Object.keys(localErrors).length > 0 ||
    Object.keys(errors ?? {}).length > 0;
  const isEditMode = mode === "edit";
  const selectedTargets = {
    racks: getSelectedDeviceSetIds(values, "racks"),
    groups: getSelectedDeviceSetIds(values, "groups"),
    miners: getSelectedMinerIds(values),
  };
  const previewPane = <PreviewPane {...previewState} />;

  const handleDeviceSetSelection = (deviceSetIds: string[], scopeId: DeviceSetScopeId) => {
    const hasSelectedDeviceSets = deviceSetIds.length > 0;

    updateValues((current) => ({
      ...current,
      scopeType: hasSelectedDeviceSets ? "deviceSet" : "wholeOrg",
      scopeId: hasSelectedDeviceSets ? scopeId : "whole-org",
      deviceSetIds,
      deviceIdentifiers: [],
    }));
  };

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
    if (hasBlockingValidationError) {
      return;
    }

    if (values.includeMaintenance && !maintenanceInclusionConfirmed) {
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
    disabled: hasBlockingValidationError,
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
        abovePanes={<div className="px-6 pb-6 laptop:hidden">{previewPane}</div>}
        primaryPane={
          <section className="flex flex-col gap-12 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
            <Section title="Response profile">
              <div className="grid gap-3">
                <TypedSelect
                  id="curtailment-response-profile"
                  label="Profile"
                  value={values.responseProfileId}
                  options={responseProfileOptions}
                  error={effectiveErrors.responseProfileId}
                  onChange={(value) => updateValue("responseProfileId", value)}
                />
                <Field
                  id="curtailment-reason"
                  label="Reason"
                  value={values.reason}
                  type="text"
                  error={effectiveErrors.reason}
                  onChange={(value) => updateValue("reason", value)}
                />
              </div>
            </Section>

            <Section title="Curtail behavior">
              <div className="grid gap-3">
                <div className="grid gap-3 tablet:grid-cols-2">
                  <TypedSelect
                    id="curtailment-mode"
                    label="Curtailment mode"
                    value={values.curtailmentMode}
                    options={curtailmentModeOptions}
                    error={effectiveErrors.curtailmentMode}
                    onChange={(value) => updateValue("curtailmentMode", value)}
                  />
                  <Field
                    id="curtailment-target-kw"
                    label="Target reduction"
                    value={values.targetKw}
                    units="kW"
                    error={effectiveErrors.targetKw}
                    onChange={(value) => updateValue("targetKw", value)}
                  />
                </div>
                <TypedSelect
                  id="curtailment-miner-selection-strategy"
                  label="Miner selection strategy"
                  value={values.minerSelectionStrategy}
                  options={minerSelectionStrategyOptions}
                  error={effectiveErrors.minerSelectionStrategy}
                  onChange={(value) => updateValue("minerSelectionStrategy", value)}
                />
                <div className="grid gap-3 tablet:grid-cols-2">
                  <Field
                    id="curtailment-min-duration"
                    label="Min duration (sec)"
                    value={values.minDurationSec}
                    error={effectiveErrors.minDurationSec}
                    onChange={(value) => updateValue("minDurationSec", value)}
                  />
                  <Field
                    id="curtailment-max-duration"
                    label="Max duration (sec)"
                    value={values.maxDurationSec}
                    error={effectiveErrors.maxDurationSec}
                    onChange={(value) => updateValue("maxDurationSec", value)}
                  />
                </div>
              </div>
            </Section>

            <Section title="Restore behavior">
              <div className="grid gap-3 tablet:grid-cols-2">
                <Field
                  id="curtailment-restore-batch-size"
                  label="Batch size (miners)"
                  value={values.restoreBatchSize}
                  error={effectiveErrors.restoreBatchSize}
                  onChange={(value) => updateValue("restoreBatchSize", value)}
                />
                <Field
                  id="curtailment-restore-batch-interval"
                  label="Batch interval (sec)"
                  value={values.restoreIntervalSec}
                  error={effectiveErrors.restoreIntervalSec}
                  onChange={(value) => updateValue("restoreIntervalSec", value)}
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
        secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0 laptop:!rounded-[24px]"
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

function CurtailmentStartModal(props: CurtailmentStartModalProps): ReactElement | null {
  if (!props.open) {
    return null;
  }

  return <CurtailmentStartModalContent key={getInitialValuesKey(props.initialValues)} {...props} />;
}

export default CurtailmentStartModal;
