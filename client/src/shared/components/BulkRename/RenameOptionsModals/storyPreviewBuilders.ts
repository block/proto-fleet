import { fixedStringSections, type FixedValueOptionsValues, type QualifierOptionsValues } from "./types";

const sectionSeparator = "-";

export const baseMinerNameSections = {
  location: "TEXAUST",
  building: "BA",
  rack: "R01",
  position: "001",
  suffix: "4D5E",
} as const;

interface PreviewResult {
  previewName: string;
  highlightedText: string;
  highlightStartIndex: number;
}

export const buildFixedPreview = (values: FixedValueOptionsValues): PreviewResult => {
  let selectedLocationSection: string = baseMinerNameSections.location;

  if (typeof values.characterCount === "number") {
    const locationCharacterCount = Math.min(values.characterCount, baseMinerNameSections.location.length);

    if (values.stringSection === fixedStringSections.last) {
      const startIndex = baseMinerNameSections.location.length - locationCharacterCount;
      selectedLocationSection = baseMinerNameSections.location.slice(startIndex);
    } else {
      selectedLocationSection = baseMinerNameSections.location.slice(0, locationCharacterCount);
    }
  }

  const previewName = [
    selectedLocationSection,
    baseMinerNameSections.building,
    baseMinerNameSections.rack,
    baseMinerNameSections.position,
    baseMinerNameSections.suffix,
  ].join(sectionSeparator);

  return { previewName, highlightedText: selectedLocationSection, highlightStartIndex: 0 };
};

export const buildQualifierPreview = (values: QualifierOptionsValues): PreviewResult => {
  const qualifiedBuildingSection = `${values.prefix}${baseMinerNameSections.building}${values.suffix}`;

  const previewName = [
    baseMinerNameSections.location,
    qualifiedBuildingSection,
    baseMinerNameSections.rack,
    baseMinerNameSections.position,
    baseMinerNameSections.suffix,
  ].join(sectionSeparator);

  return {
    previewName,
    highlightedText: qualifiedBuildingSection,
    highlightStartIndex: baseMinerNameSections.location.length + sectionSeparator.length,
  };
};
