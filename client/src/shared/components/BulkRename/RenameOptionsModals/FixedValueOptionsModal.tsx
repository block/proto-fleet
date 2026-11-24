import { useCallback, useEffect, useMemo, useState } from "react";

import { defaultFixedCharacterCount, fixedCharacterCountAll, fixedCharacterCountValues } from "./constants";
import HighlightedNamePreview from "./HighlightedNamePreview";
import InlineRadioGroup, { type InlineRadioOption } from "./InlineRadioGroup";
import RenameOptionsModal, { RenameOptionsModalBody, RenameOptionsModalPreview } from "./RenameOptionsModal";
import { fixedStringSections } from "./types";
import type { FixedCharacterCount, FixedStringSection, FixedValueOptionsValues } from "./types";

const buildDefaultOptions = (initialValues?: Partial<FixedValueOptionsValues>): FixedValueOptionsValues => {
  return {
    characterCount: initialValues?.characterCount ?? defaultFixedCharacterCount,
    stringSection: initialValues?.stringSection ?? fixedStringSections.last,
  };
};

const characterCountOptions: InlineRadioOption<FixedCharacterCount>[] = [
  {
    value: fixedCharacterCountAll,
    label: "All",
    testId: "fixed-value-character-count-option-all",
  },
  ...fixedCharacterCountValues.map((value) => ({
    value,
    label: String(value),
    testId: `fixed-value-character-count-option-${value}`,
  })),
];

interface FixedValueOptionsModalProps {
  open: boolean;
  previewName: string;
  highlightedText?: string;
  highlightStartIndex?: number;
  initialValues?: Partial<FixedValueOptionsValues>;
  onConfirm: (nextValues: FixedValueOptionsValues) => void;
  onDismiss: () => void;
  onChange?: (nextValues: FixedValueOptionsValues) => void;
}

type OpenFixedValueOptionsModalProps = Omit<FixedValueOptionsModalProps, "open">;

const OpenFixedValueOptionsModal = ({
  previewName,
  highlightedText,
  highlightStartIndex,
  initialValues,
  onConfirm,
  onDismiss,
  onChange,
}: OpenFixedValueOptionsModalProps) => {
  const [options, setOptions] = useState<FixedValueOptionsValues>(buildDefaultOptions(initialValues));

  useEffect(() => {
    onChange?.(options);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- onChange is intentionally excluded to prevent infinite loops from unstable callback references
  }, [options]);

  const showStringSectionOptions = options.characterCount !== fixedCharacterCountAll;
  const selectedCount = useMemo(() => {
    if (typeof options.characterCount === "number") {
      return options.characterCount;
    }

    return fixedCharacterCountValues[0];
  }, [options.characterCount]);
  const characterSuffix = selectedCount === 1 ? "character" : "characters";

  const stringSectionOptions: InlineRadioOption<FixedStringSection>[] = [
    {
      value: fixedStringSections.first,
      label: `First ${selectedCount} ${characterSuffix}`,
      testId: "fixed-value-string-section-option-first",
    },
    {
      value: fixedStringSections.last,
      label: `Last ${selectedCount} ${characterSuffix}`,
      testId: "fixed-value-string-section-option-last",
    },
  ];

  const handleConfirm = useCallback(() => {
    onConfirm({
      characterCount: options.characterCount,
      stringSection: showStringSectionOptions ? options.stringSection : undefined,
    });
  }, [onConfirm, options, showStringSectionOptions]);

  return (
    <RenameOptionsModal
      onDismiss={onDismiss}
      onConfirm={handleConfirm}
      desktopSaveTestId="fixed-value-options-save-button"
      mobileSaveTestId="fixed-value-options-save-button-mobile"
    >
      <RenameOptionsModalBody>
        <InlineRadioGroup
          label="Number of characters"
          options={characterCountOptions}
          value={options.characterCount}
          onChange={(nextValue) => {
            setOptions((previousValue) => ({
              ...previousValue,
              characterCount: nextValue,
            }));
          }}
        />

        {showStringSectionOptions ? (
          <InlineRadioGroup
            label="String section"
            options={stringSectionOptions}
            value={options.stringSection ?? fixedStringSections.first}
            onChange={(nextValue) => {
              setOptions((previousValue) => ({
                ...previousValue,
                stringSection: nextValue,
              }));
            }}
          />
        ) : null}

        <RenameOptionsModalPreview>
          <HighlightedNamePreview
            previewName={previewName}
            highlightedText={highlightedText}
            highlightStartIndex={highlightStartIndex}
            testIdPrefix="fixed-value-preview"
          />
        </RenameOptionsModalPreview>
      </RenameOptionsModalBody>
    </RenameOptionsModal>
  );
};

const FixedValueOptionsModal = ({ open, ...props }: FixedValueOptionsModalProps) => {
  if (!open) {
    return null;
  }

  return <OpenFixedValueOptionsModal {...props} />;
};

export default FixedValueOptionsModal;
