import { type ReactNode, useState } from "react";

import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Select from "@/shared/components/Select";

export type CurtailmentPriority = "normal" | "emergency";

export interface CurtailmentFormValues {
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

export interface CurtailmentPlanPreview {
  selectedMinerCount: number;
  targetKw: number;
  estimatedReductionKw: number;
  restoreEstimate: string;
  scopeLabel: string;
}

interface CurtailmentStartModalProps {
  open: boolean;
  onDismiss: () => void;
  initialValues?: Partial<CurtailmentFormValues>;
  preview?: CurtailmentPlanPreview;
  previewError?: string;
}

interface FieldProps {
  id: string;
  label: string;
  value: string;
  units?: string;
  placeholder?: string;
  type?: "number" | "text";
  onChange: (value: string) => void;
}

interface SectionProps {
  title: string;
  children: ReactNode;
}

const defaultValues: CurtailmentFormValues = {
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  reason: "",
  includeMaintenance: false,
};

const inputFrameClassName =
  "flex min-h-14 w-full items-center gap-2 rounded-xl border border-border-5 bg-surface-base px-4 py-1";

function Field({ id, label, value, units, placeholder = label, type = "number", onChange }: FieldProps) {
  const hasValue = value.trim().length > 0;

  return (
    <label htmlFor={id} className="block">
      <span className={inputFrameClassName}>
        <span className="flex min-w-0 flex-1 flex-col justify-center">
          <span className={hasValue ? "text-200 text-text-primary-50" : "sr-only"}>{label}</span>
          <span className="flex min-w-0 items-center">
            <input
              id={id}
              type={type}
              className="min-w-0 bg-transparent text-300 text-text-primary outline-hidden placeholder:text-text-primary-50"
              style={type === "number" && hasValue ? { width: `${Math.max(value.length, 1) + 0.5}ch` } : undefined}
              value={value}
              placeholder={placeholder}
              aria-label={label}
              autoComplete="new-password"
              onChange={(event) => onChange(event.currentTarget.value)}
            />
            {units && hasValue ? <span className="shrink-0 text-300 text-text-primary">{units}</span> : null}
          </span>
        </span>
      </span>
    </label>
  );
}

function Section({ title, children }: SectionProps) {
  return (
    <section className="grid gap-3">
      <div className="text-emphasis-300 text-text-primary">{title}</div>
      {children}
    </section>
  );
}

function TargetButton({ label, value }: { label: string; value: string }) {
  return (
    <button
      type="button"
      className="relative flex h-14 w-full items-center justify-between rounded-lg border border-border-5 bg-surface-base px-4 text-left outline-hidden"
    >
      <div className="flex min-w-0 flex-col pt-[18px]">
        <span className="absolute top-[7px] text-200 text-text-primary-50">{label}</span>
        <div className="truncate text-300 text-text-primary-50">{value}</div>
      </div>
      <ChevronDown width="w-3" className="shrink-0 text-text-primary-70" />
    </button>
  );
}

function formatKw(value: number): string {
  return `${value.toLocaleString(undefined, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  })} kW`;
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

function PreviewPane({ preview, previewError }: { preview?: CurtailmentPlanPreview; previewError?: string }) {
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

        <div>
          <div className="text-emphasis-200 text-text-primary-70">Time to restore</div>
          <div className="text-heading-300 text-text-primary">{preview.restoreEstimate}</div>
        </div>
      </div>
    </div>
  );
}

function CurtailmentStartModal({ open, onDismiss, initialValues, preview, previewError }: CurtailmentStartModalProps) {
  const [values, setValues] = useState<CurtailmentFormValues>({ ...defaultValues, ...initialValues });
  const updateValue = <Key extends keyof CurtailmentFormValues>(key: Key, value: CurtailmentFormValues[Key]) =>
    setValues((current) => ({ ...current, [key]: value }));
  const previewPane = <PreviewPane preview={preview} previewError={previewError} />;

  if (!open) {
    return null;
  }

  return (
    <FullScreenTwoPaneModal
      open={open}
      title="Plan a curtailment"
      closeAriaLabel="Close curtailment planner"
      onDismiss={onDismiss}
      buttons={[{ text: "Start curtailment", variant: variants.primary, onClick: onDismiss }]}
      abovePanes={<div className="px-6 pb-6 laptop:hidden">{previewPane}</div>}
      primaryPane={
        <section className="flex flex-col gap-10 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
          <Section title="Details">
            <div className="grid gap-3">
              <div className="grid gap-3 tablet:grid-cols-2">
                <Field
                  id="curtailment-target-kw"
                  label="Target reduction"
                  value={values.targetKw}
                  units="kW"
                  onChange={(value) => updateValue("targetKw", value)}
                />
                <Field
                  id="curtailment-tolerance-kw"
                  label="Tolerance"
                  value={values.toleranceKw}
                  units="kW"
                  onChange={(value) => updateValue("toleranceKw", value)}
                />
              </div>

              <Select
                id="curtailment-priority"
                label="Priority"
                value={values.priority}
                className="max-w-[274px]"
                options={[
                  { value: "normal", label: "Normal" },
                  { value: "emergency", label: "Emergency" },
                ]}
                onChange={(value) => updateValue("priority", value as CurtailmentPriority)}
              />
            </div>
          </Section>

          <Section title="Safety and restore">
            <div className="grid gap-3">
              <div className="grid gap-3 tablet:grid-cols-2">
                <Field
                  id="curtailment-min-duration"
                  label="Min duration"
                  value={values.minDurationSec}
                  units="sec"
                  onChange={(value) => updateValue("minDurationSec", value)}
                />
                <Field
                  id="curtailment-max-duration"
                  label="Max duration"
                  value={values.maxDurationSec}
                  units="sec"
                  onChange={(value) => updateValue("maxDurationSec", value)}
                />
                <Field
                  id="curtailment-batch-size"
                  label="Restore batch size"
                  value={values.restoreBatchSize}
                  units="miners"
                  onChange={(value) => updateValue("restoreBatchSize", value)}
                />
                <Field
                  id="curtailment-batch-interval"
                  label="Restore interval"
                  value={values.restoreIntervalSec}
                  units="sec"
                  onChange={(value) => updateValue("restoreIntervalSec", value)}
                />
              </div>

              <Field
                id="curtailment-reason"
                label="Reason"
                value={values.reason}
                placeholder="Label"
                type="text"
                onChange={(value) => updateValue("reason", value)}
              />
            </div>
          </Section>

          <Section title="Apply to">
            <div className="grid gap-4 tablet:grid-cols-3">
              <TargetButton label="Racks" value="Select" />
              <TargetButton label="Groups" value="Select" />
              <TargetButton label="Miners" value="Select" />
            </div>
          </Section>

          <label className="flex cursor-pointer items-start gap-3 text-left">
            <Checkbox
              checked={values.includeMaintenance}
              onChange={(event) => updateValue("includeMaintenance", event.currentTarget.checked)}
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
  );
}

export default CurtailmentStartModal;
