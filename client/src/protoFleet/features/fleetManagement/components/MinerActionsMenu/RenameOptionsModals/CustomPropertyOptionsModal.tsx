import { useCallback, useEffect, useState } from "react";
import {
  counterScaleMaximum,
  counterScaleMinimum,
  counterScaleValues,
  counterStartInputMaxLength,
  defaultCounterScale,
  renameOptionInputMaxLength,
} from "./constants";
import CustomPropertyTypeDropdown from "./CustomPropertyTypeDropdown";
import InlineRadioGroup from "./InlineRadioGroup";
import { type CustomPropertyOptionsValues, customPropertyTypes } from "./types";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import NamePreview, { PreviewContainer } from "@/shared/components/NamePreview";
import { clamp } from "@/shared/utils/math";

const buildDefaultOptions = (initialValues?: Partial<CustomPropertyOptionsValues>): CustomPropertyOptionsValues => ({
  type: initialValues?.type ?? customPropertyTypes.stringAndCounter,
  prefix: initialValues?.prefix ?? "",
  suffix: initialValues?.suffix ?? "",
  counterStart: initialValues?.counterStart,
  counterScale: initialValues?.counterScale ?? defaultCounterScale,
  stringValue: initialValues?.stringValue ?? "",
});

const parseCounterStart = (inputValue: string): number | undefined => {
  const parsed = Number.parseInt(inputValue.trim(), 10);
  return Number.isNaN(parsed) ? undefined : parsed;
};

const counterScaleOptions = counterScaleValues.map((counterScale) => ({
  value: counterScale,
  label: String(counterScale),
  testId: `custom-property-counter-scale-option-${counterScale}`,
}));

const previewPlaceholderLabels = {
  [customPropertyTypes.stringOnly]: "Enter string to preview",
  [customPropertyTypes.counterOnly]: "Enter counter to preview",
  [customPropertyTypes.stringAndCounter]: "Enter prefix, suffix, or counter to preview",
} as const;

interface CustomPropertyOptionsModalProps {
  open: boolean;
  previewName: string;
  initialValues?: Partial<CustomPropertyOptionsValues>;
  onConfirm: (nextValues: CustomPropertyOptionsValues) => void;
  onDismiss: () => void;
  onChange?: (nextValues: CustomPropertyOptionsValues) => void;
}

type OpenCustomPropertyOptionsModalProps = Omit<CustomPropertyOptionsModalProps, "open">;

const OpenCustomPropertyOptionsModal = ({
  previewName,
  initialValues,
  onConfirm,
  onDismiss,
  onChange,
}: OpenCustomPropertyOptionsModalProps) => {
  const [options, setOptions] = useState<CustomPropertyOptionsValues>(buildDefaultOptions(initialValues));
  const [counterStartInput, setCounterStartInput] = useState(
    initialValues?.counterStart === undefined ? "" : String(initialValues.counterStart),
  );

  useEffect(() => {
    onChange?.(options);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- onChange is intentionally excluded to prevent infinite loops from unstable callback references
  }, [options]);

  const updateOption = useCallback(
    <K extends keyof CustomPropertyOptionsValues>(key: K, value: CustomPropertyOptionsValues[K]) => {
      setOptions((prev) => ({ ...prev, [key]: value }));
    },
    [],
  );

  const isStringAndCounter = options.type === customPropertyTypes.stringAndCounter;
  const isCounterOnly = options.type === customPropertyTypes.counterOnly;
  const isStringOnly = options.type === customPropertyTypes.stringOnly;
  const includesCounter = isStringAndCounter || isCounterOnly;
  const missingCounter = counterStartInput.trim() === "";

  const saveDisabled = (includesCounter && missingCounter) || (isStringOnly && options.stringValue.trim() === "");

  const showPreviewPlaceholder =
    (isStringOnly && options.stringValue.trim() === "") ||
    (isCounterOnly && missingCounter) ||
    (isStringAndCounter && missingCounter && options.prefix.trim() === "" && options.suffix.trim() === "");

  const previewNameValue = isStringAndCounter && missingCounter ? `${options.prefix}${options.suffix}` : previewName;

  const handleConfirm = useCallback(() => {
    if (saveDisabled) return;
    onConfirm({
      ...options,
      prefix: options.prefix.trim(),
      suffix: options.suffix.trim(),
      stringValue: options.stringValue.trim(),
    });
  }, [onConfirm, options, saveDisabled]);

  return (
    <Modal
      open={true}
      contentHeader="Options"
      contentHeaderClassName="text-heading-300"
      onDismiss={onDismiss}
      divider={false}
      headerSpacingClassName="mt-4"
      size="large"
      buttonSize="base"
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleConfirm,
          disabled: saveDisabled,
          testId: "custom-property-options-save-button",
        },
      ]}
    >
      <div className="mt-10 flex flex-col gap-6">
        <CustomPropertyTypeDropdown
          selectedType={options.type}
          onChange={(nextType) => updateOption("type", nextType)}
        />

        {isStringAndCounter ? (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input
              id="custom-property-prefix"
              label="Prefix (optional)"
              initValue={options.prefix}
              maxLength={renameOptionInputMaxLength}
              onChange={(v) => updateOption("prefix", v)}
              testId="custom-property-prefix-input"
            />
            <Input
              id="custom-property-suffix"
              label="Suffix (optional)"
              initValue={options.suffix}
              maxLength={renameOptionInputMaxLength}
              onChange={(v) => updateOption("suffix", v)}
              testId="custom-property-suffix-input"
            />
          </div>
        ) : null}

        {includesCounter ? (
          <>
            <Input
              id="custom-property-counter-start"
              label="Counter start number"
              initValue={counterStartInput}
              maxLength={counterStartInputMaxLength}
              onChange={(nextValue) => {
                const limited = nextValue.replace(/\D/g, "").slice(0, counterStartInputMaxLength);
                setCounterStartInput(limited);
                updateOption("counterStart", parseCounterStart(limited));
              }}
              testId="custom-property-counter-start-input"
            />
            <InlineRadioGroup
              label="Counter scale"
              value={options.counterScale}
              options={counterScaleOptions}
              onChange={(v) => updateOption("counterScale", clamp(v, counterScaleMinimum, counterScaleMaximum))}
            />
          </>
        ) : null}

        {isStringOnly ? (
          <Input
            id="custom-property-string"
            label="String"
            initValue={options.stringValue}
            maxLength={renameOptionInputMaxLength}
            onChange={(v) => updateOption("stringValue", v)}
            testId="custom-property-string-input"
          />
        ) : null}

        <div className="max-w-[592px]">
          {showPreviewPlaceholder ? (
            <PreviewContainer>
              <span className="text-300 text-text-primary-50">{previewPlaceholderLabels[options.type]}</span>
            </PreviewContainer>
          ) : (
            <NamePreview mode="new-name-only" newName={previewNameValue} />
          )}
        </div>
      </div>
    </Modal>
  );
};

const CustomPropertyOptionsModal = ({ open, ...props }: CustomPropertyOptionsModalProps) => {
  if (!open) {
    return null;
  }

  return <OpenCustomPropertyOptionsModal {...props} />;
};

export default CustomPropertyOptionsModal;
