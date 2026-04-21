import { type ReactNode, useState } from "react";
import { action } from "storybook/actions";

import { buildFixedPreview, buildQualifierPreview } from "./storyPreviewBuilders";
import {
  CustomPropertyOptionsModal,
  type CustomPropertyOptionsValues,
  customPropertyTypes,
  FixedValueOptionsModal,
  type FixedValueOptionsValues,
  QualifierOptionsModal,
  type QualifierOptionsValues,
} from "./index";
import { padLeft } from "@/shared/utils/stringUtils";

export default {
  title: "Proto Fleet/Fleet Management/Bulk Rename/Options Modals",
};

const StoryContainer = ({ children }: { children: ReactNode }) => {
  return <div className="min-h-screen bg-surface-base p-6">{children}</div>;
};

const buildCustomPreview = (values: CustomPropertyOptionsValues) => {
  if (values.type === customPropertyTypes.stringOnly) {
    return values.stringValue;
  }

  if (values.counterStart === undefined) {
    return `${values.prefix}${values.suffix}`;
  }

  const counterValue = padLeft(values.counterStart, values.counterScale);

  if (values.type === customPropertyTypes.counterOnly) {
    return counterValue;
  }

  return `${values.prefix}${counterValue}${values.suffix}`;
};

const customOptionsInitialValues: CustomPropertyOptionsValues = {
  type: customPropertyTypes.stringAndCounter,
  prefix: "Building-A-",
  suffix: "-R01",
  counterStart: 7,
  counterScale: 3,
  stringValue: "Rack-A",
};

export const CustomOptions = () => {
  const [open, setOpen] = useState(true);
  const [previewName, setPreviewName] = useState(buildCustomPreview(customOptionsInitialValues));

  return (
    <StoryContainer>
      <CustomPropertyOptionsModal
        open={open}
        previewName={previewName}
        initialValues={customOptionsInitialValues}
        onChange={(nextValues) => setPreviewName(buildCustomPreview(nextValues))}
        onConfirm={(values) => {
          action("customOptionsOnConfirm")(values);
          setOpen(false);
        }}
        onDismiss={() => {
          action("customOptionsOnDismiss")();
          setOpen(false);
        }}
      />
    </StoryContainer>
  );
};

export const FixedValueOptions = () => {
  const [open, setOpen] = useState(true);
  const initialValues: FixedValueOptionsValues = {
    characterCount: 4,
    stringSection: "first",
  };
  const [preview, setPreview] = useState(buildFixedPreview(initialValues));

  return (
    <StoryContainer>
      <FixedValueOptionsModal
        open={open}
        previewName={preview.previewName}
        highlightedText={preview.highlightedText}
        highlightStartIndex={preview.highlightStartIndex}
        initialValues={initialValues}
        onChange={(nextValues) => setPreview(buildFixedPreview(nextValues))}
        onConfirm={(values) => {
          action("fixedValueOptionsOnConfirm")(values);
          setOpen(false);
        }}
        onDismiss={() => {
          action("fixedValueOptionsOnDismiss")();
          setOpen(false);
        }}
      />
    </StoryContainer>
  );
};

export const QualifierOptions = () => {
  const [open, setOpen] = useState(true);
  const initialValues: QualifierOptionsValues = {
    prefix: "",
    suffix: "",
  };
  const [preview, setPreview] = useState(buildQualifierPreview(initialValues));

  return (
    <StoryContainer>
      <QualifierOptionsModal
        open={open}
        previewName={preview.previewName}
        highlightedText={preview.highlightedText}
        highlightStartIndex={preview.highlightStartIndex}
        initialValues={initialValues}
        onChange={(nextValues) => setPreview(buildQualifierPreview(nextValues))}
        onConfirm={(values) => {
          action("qualifierOptionsOnConfirm")(values);
          setOpen(false);
        }}
        onDismiss={() => {
          action("qualifierOptionsOnDismiss")();
          setOpen(false);
        }}
      />
    </StoryContainer>
  );
};
