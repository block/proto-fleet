import { useCallback, useEffect, useState } from "react";

import { renameOptionInputMaxLength } from "./constants";
import HighlightedNamePreview from "./HighlightedNamePreview";
import RenameOptionsModal, { RenameOptionsModalBody, RenameOptionsModalPreview } from "./RenameOptionsModal";
import { type QualifierOptionsValues } from "./types";
import Input from "@/shared/components/Input";

const buildDefaultOptions = (initialValues?: Partial<QualifierOptionsValues>): QualifierOptionsValues => {
  return {
    prefix: initialValues?.prefix ?? "",
    suffix: initialValues?.suffix ?? "",
  };
};

interface QualifierOptionsModalProps {
  open: boolean;
  previewName: string;
  highlightedText?: string;
  highlightStartIndex?: number;
  initialValues?: Partial<QualifierOptionsValues>;
  onConfirm: (nextValues: QualifierOptionsValues) => void;
  onDismiss: () => void;
  onChange?: (nextValues: QualifierOptionsValues) => void;
}

type OpenQualifierOptionsModalProps = Omit<QualifierOptionsModalProps, "open">;

const OpenQualifierOptionsModal = ({
  previewName,
  highlightedText,
  highlightStartIndex,
  initialValues,
  onConfirm,
  onDismiss,
  onChange,
}: OpenQualifierOptionsModalProps) => {
  const [options, setOptions] = useState<QualifierOptionsValues>(buildDefaultOptions(initialValues));

  useEffect(() => {
    onChange?.(options);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- onChange is intentionally excluded to prevent infinite loops from unstable callback references
  }, [options]);

  const handleConfirm = useCallback(() => {
    onConfirm({
      prefix: options.prefix.trim(),
      suffix: options.suffix.trim(),
    });
  }, [onConfirm, options.prefix, options.suffix]);

  return (
    <RenameOptionsModal
      onDismiss={onDismiss}
      onConfirm={handleConfirm}
      desktopSaveTestId="qualifier-options-save-button"
      mobileSaveTestId="qualifier-options-save-button-mobile"
    >
      <RenameOptionsModalBody>
        <div className="grid grid-cols-1 gap-4 tablet:grid-cols-2">
          <Input
            id="qualifier-property-prefix"
            label="Prefix (optional)"
            initValue={options.prefix}
            maxLength={renameOptionInputMaxLength}
            onChange={(nextValue) => {
              setOptions((previousValue) => ({
                ...previousValue,
                prefix: nextValue,
              }));
            }}
            testId="qualifier-property-prefix-input"
          />
          <Input
            id="qualifier-property-suffix"
            label="Suffix (optional)"
            initValue={options.suffix}
            maxLength={renameOptionInputMaxLength}
            onChange={(nextValue) => {
              setOptions((previousValue) => ({
                ...previousValue,
                suffix: nextValue,
              }));
            }}
            testId="qualifier-property-suffix-input"
          />
        </div>

        <RenameOptionsModalPreview>
          <HighlightedNamePreview
            previewName={previewName}
            highlightedText={highlightedText}
            highlightStartIndex={highlightStartIndex}
            testIdPrefix="qualifier-preview"
          />
        </RenameOptionsModalPreview>
      </RenameOptionsModalBody>
    </RenameOptionsModal>
  );
};

const QualifierOptionsModal = ({ open, ...props }: QualifierOptionsModalProps) => {
  if (!open) {
    return null;
  }

  return <OpenQualifierOptionsModal {...props} />;
};

export default QualifierOptionsModal;
