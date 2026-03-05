import { useCallback, useEffect, useState } from "react";

import { renameOptionInputMaxLength } from "./constants";
import HighlightedNamePreview from "./HighlightedNamePreview";
import { type QualifierOptionsValues } from "./types";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";

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
          testId: "qualifier-options-save-button",
        },
      ]}
    >
      <div className="mt-10 flex flex-col gap-6">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
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

        <div className="max-w-[592px]">
          <HighlightedNamePreview
            previewName={previewName}
            highlightedText={highlightedText}
            highlightStartIndex={highlightStartIndex}
            testIdPrefix="qualifier-preview"
          />
        </div>
      </div>
    </Modal>
  );
};

const QualifierOptionsModal = ({ open, ...props }: QualifierOptionsModalProps) => {
  if (!open) {
    return null;
  }

  return <OpenQualifierOptionsModal {...props} />;
};

export default QualifierOptionsModal;
