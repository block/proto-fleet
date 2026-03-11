import {
  type BulkRenamePropertyId,
  type BulkRenamePropertyOptions,
  getBulkRenamePropertyDefinition,
} from "./bulkRenameDefinitions";
import CustomPropertyOptionsModal from "./RenameOptionsModals/CustomPropertyOptionsModal";
import FixedValueOptionsModal from "./RenameOptionsModals/FixedValueOptionsModal";
import QualifierOptionsModal from "./RenameOptionsModals/QualifierOptionsModal";
import {
  type CustomPropertyOptionsValues,
  type FixedValueOptionsValues,
  type QualifierOptionsValues,
} from "./RenameOptionsModals/types";

interface BulkRenameOptionModalProps {
  activeOptionsPropertyId: BulkRenamePropertyId | null;
  activeOptionsPropertyOptions: BulkRenamePropertyOptions | null;
  previewName: string;
  highlightedText?: string;
  highlightStartIndex?: number;
  onDismiss: () => void;
  onChange: (options: BulkRenamePropertyOptions | null) => void;
  onConfirm: (
    propertyId: BulkRenamePropertyId,
    options: CustomPropertyOptionsValues | FixedValueOptionsValues | QualifierOptionsValues,
  ) => void;
}

const BulkRenameOptionModals = ({
  activeOptionsPropertyId,
  activeOptionsPropertyOptions,
  previewName,
  highlightedText,
  highlightStartIndex,
  onDismiss,
  onChange,
  onConfirm,
}: BulkRenameOptionModalProps) => {
  if (activeOptionsPropertyId === null || activeOptionsPropertyOptions === null) {
    return null;
  }

  const sharedProps = {
    open: true,
    previewName,
    highlightedText,
    highlightStartIndex,
    onDismiss,
    onChange,
  };

  const activeOptionsKind = getBulkRenamePropertyDefinition(activeOptionsPropertyId).kind;

  if (activeOptionsKind === "custom") {
    return (
      <CustomPropertyOptionsModal
        {...sharedProps}
        initialValues={activeOptionsPropertyOptions as CustomPropertyOptionsValues}
        onConfirm={(options) => onConfirm(activeOptionsPropertyId, options)}
      />
    );
  }

  if (activeOptionsKind === "fixed") {
    return (
      <FixedValueOptionsModal
        {...sharedProps}
        initialValues={activeOptionsPropertyOptions as FixedValueOptionsValues}
        onConfirm={(options) => onConfirm(activeOptionsPropertyId, options)}
      />
    );
  }

  return (
    <QualifierOptionsModal
      {...sharedProps}
      initialValues={activeOptionsPropertyOptions as QualifierOptionsValues}
      onConfirm={(options) => onConfirm(activeOptionsPropertyId, options)}
    />
  );
};

export default BulkRenameOptionModals;
