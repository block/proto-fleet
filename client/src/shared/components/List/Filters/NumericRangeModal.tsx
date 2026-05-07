import { useState } from "react";

import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
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
  // Re-key on open so the draft state hydrates fresh each time the parent
  // opens the modal for a different category, without needing useEffect.
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
      <div className="mt-4 flex flex-col gap-2">
        <div className="flex flex-row items-start gap-3">
          <div className="flex-1">
            <Input
              id={minId}
              label={`Min ${bounds.unit}`}
              type="number"
              units={bounds.unit}
              initValue={minDraft}
              onChange={(value) => setMinDraft(value)}
              error={errors.min ?? false}
              testId={minId}
            />
          </div>
          <div className="flex-1">
            <Input
              id={maxId}
              label={`Max ${bounds.unit}`}
              type="number"
              units={bounds.unit}
              initValue={maxDraft}
              onChange={(value) => setMaxDraft(value)}
              error={errors.max ?? false}
              testId={maxId}
            />
          </div>
        </div>
        {errors.cross ? (
          <div className="text-200 text-intent-critical-fill" data-testid={`numeric-range-${categoryKey}-cross-error`}>
            {errors.cross}
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default NumericRangeModal;
