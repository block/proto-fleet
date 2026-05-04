import { useState } from "react";
import clsx from "clsx";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import { type NumericRangeBounds, type NumericRangeValue, validateNumericRange } from "@/shared/utils/filterValidation";

type NumericRangeModalProps = {
  open: boolean;
  categoryKey: string;
  label: string;
  bounds: NumericRangeBounds;
  initialValue: NumericRangeValue;
  onApply: (value: NumericRangeValue) => void;
  onClose: () => void;
};

const toBoundValue = (raw: string): number | undefined => {
  if (raw.trim() === "") return undefined;
  return Number(raw);
};

const NumericRangeModal = ({
  open,
  categoryKey,
  label,
  bounds,
  initialValue,
  onApply,
  onClose,
}: NumericRangeModalProps) => {
  // Re-keying on initialValue ensures the modal hydrates fresh every time the
  // parent opens it for a different category, without needing useEffect.
  return open ? (
    <NumericRangeModalContent
      key={`${categoryKey}-${initialValue.min ?? ""}-${initialValue.max ?? ""}`}
      categoryKey={categoryKey}
      label={label}
      bounds={bounds}
      initialValue={initialValue}
      onApply={onApply}
      onClose={onClose}
    />
  ) : null;
};

const NumericRangeModalContent = ({
  categoryKey,
  label,
  bounds,
  initialValue,
  onApply,
  onClose,
}: Omit<NumericRangeModalProps, "open">) => {
  const [minDraft, setMinDraft] = useState(initialValue.min !== undefined ? String(initialValue.min) : "");
  const [maxDraft, setMaxDraft] = useState(initialValue.max !== undefined ? String(initialValue.max) : "");

  const draft: NumericRangeValue = {
    min: toBoundValue(minDraft),
    max: toBoundValue(maxDraft),
  };
  const errors = validateNumericRange(draft, bounds);
  const isValid = Object.keys(errors).length === 0;

  const handleApply = () => {
    const cleaned: NumericRangeValue = {};
    if (draft.min !== undefined) cleaned.min = draft.min;
    if (draft.max !== undefined) cleaned.max = draft.max;
    onApply(cleaned);
    onClose();
  };

  const minId = `numeric-range-${categoryKey}-min`;
  const maxId = `numeric-range-${categoryKey}-max`;

  return (
    <Modal
      open
      title={label}
      onDismiss={onClose}
      size="standard"
      testId={`numeric-range-modal-${categoryKey}`}
      buttons={[
        {
          text: "Apply",
          onClick: handleApply,
          variant: variants.primary,
          disabled: !isValid,
        },
      ]}
    >
      <div className="mt-4 flex flex-col gap-4">
        <RangeInput
          id={minId}
          label="Min"
          unit={bounds.unit}
          value={minDraft}
          onChange={setMinDraft}
          error={errors.min}
        />
        <RangeInput
          id={maxId}
          label="Max"
          unit={bounds.unit}
          value={maxDraft}
          onChange={setMaxDraft}
          error={errors.max}
        />
        {errors.cross ? (
          <div className="text-200 text-intent-critical-fill" data-testid={`numeric-range-${categoryKey}-cross-error`}>
            {errors.cross}
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

type RangeInputProps = {
  id: string;
  label: string;
  unit: string;
  value: string;
  onChange: (value: string) => void;
  error?: string;
};

const RangeInput = ({ id, label, unit, value, onChange, error }: RangeInputProps) => (
  <div className="space-y-1">
    <label htmlFor={id} className="text-200 text-text-primary-70">
      {label}
    </label>
    <div
      className={clsx(
        "flex items-center gap-2 rounded-xl border bg-surface-elevated-base px-3 py-2",
        error ? "border-intent-critical-fill" : "border-border-primary",
      )}
    >
      <input
        id={id}
        type="number"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="grow bg-transparent text-emphasis-300 text-text-primary outline-none placeholder:text-text-primary-50"
        placeholder="—"
      />
      <span className="text-200 text-text-primary-70">{unit}</span>
    </div>
    {error ? (
      <div className="text-200 text-intent-critical-fill" data-testid={`${id}-error`}>
        {error}
      </div>
    ) : null}
  </div>
);

export default NumericRangeModal;
