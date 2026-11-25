import {
  type BulkRenamePreferences,
  type BulkRenamePreviewMiner,
  type BulkRenamePropertyId,
  type BulkRenamePropertyPreview,
  type BulkRenamePropertyState,
  bulkRenameSeparators,
  getBulkRenamePropertyDefinition,
  getEnabledBulkRenameProperties,
} from "./bulkRenameDefinitions";
import {
  type CustomPropertyOptionsValues,
  customPropertyTypes,
  fixedStringSections,
  type FixedValueOptionsValues,
  type QualifierOptionsValues,
} from "./RenameOptionsModals/types";

function formatCounter(value: number, scale: number): string {
  return value.toString().padStart(scale, "0");
}

function evaluateBulkRenamePropertySegment(
  property: BulkRenamePropertyState,
  miner: BulkRenamePreviewMiner,
  counterIndex: number,
): string {
  const definition = getBulkRenamePropertyDefinition(property.id);
  if (definition.fixedField) {
    const value = miner[definition.fixedField];
    const { characterCount, stringSection } = property.options as FixedValueOptionsValues;
    if (characterCount === "all") return value;
    const characters = Array.from(value);
    return (
      stringSection === fixedStringSections.last && characterCount < characters.length
        ? characters.slice(-characterCount)
        : characters.slice(0, characterCount)
    ).join("");
  }

  if (definition.qualifierField) {
    const value = miner[definition.qualifierField];
    const { prefix, suffix } = property.options as QualifierOptionsValues;
    return value.trim() === "" ? "" : `${prefix.trim()}${value}${suffix.trim()}`;
  }

  const options = property.options as CustomPropertyOptionsValues;
  if (options.type === customPropertyTypes.stringOnly) return options.stringValue.trim();
  if (options.counterStart === undefined) return "";
  const counter = formatCounter(options.counterStart + counterIndex, options.counterScale);
  return options.type === customPropertyTypes.counterOnly
    ? counter
    : `${options.prefix.trim()}${counter}${options.suffix.trim()}`;
}

export function evaluateBulkRenamePreviewName(
  preferences: BulkRenamePreferences,
  miner: BulkRenamePreviewMiner,
  counterIndex: number,
): string {
  return getEnabledBulkRenameProperties(preferences)
    .map((property) => evaluateBulkRenamePropertySegment(property, miner, counterIndex))
    .filter((segment) => segment.trim() !== "")
    .join(bulkRenameSeparators[preferences.separator].value)
    .trim();
}

export const buildBulkRenamePropertyPreview = (
  preferences: BulkRenamePreferences,
  propertyId: BulkRenamePropertyId,
  miner: BulkRenamePreviewMiner,
  counterIndex: number,
): BulkRenamePropertyPreview => {
  const separator = bulkRenameSeparators[preferences.separator].value;
  const segments = getEnabledBulkRenameProperties(preferences)
    .map((property) => ({
      propertyId: property.id,
      value: evaluateBulkRenamePropertySegment(property, miner, counterIndex),
    }))
    .filter((segment) => segment.value.trim() !== "");

  let previewName = "";
  let highlightStartIndex: number | undefined;
  let highlightedText: string | undefined;

  for (const segment of segments) {
    if (previewName !== "") {
      previewName += separator;
    }

    const valueStartIndex = previewName.length;
    previewName += segment.value;

    if (segment.propertyId === propertyId) {
      highlightedText = segment.value;
      highlightStartIndex = valueStartIndex;
    }
  }

  return {
    previewName: previewName.trim(),
    highlightedText,
    highlightStartIndex,
  };
};

export const findBulkRenamePropertyPreviewMinerIndex = (
  preferences: BulkRenamePreferences,
  propertyId: BulkRenamePropertyId,
  previewMiners: BulkRenamePreviewMiner[],
): number | null => {
  if (previewMiners.length === 0) {
    return null;
  }

  const property = preferences.properties.find((candidate) => candidate.id === propertyId);
  if (property === undefined) {
    return 0;
  }

  if (getBulkRenamePropertyDefinition(propertyId).kind === "custom") {
    return 0;
  }

  const previewMinerIndex = previewMiners.findIndex(
    (miner) => evaluateBulkRenamePropertySegment(property, miner, miner.counterIndex).trim() !== "",
  );

  return previewMinerIndex === -1 ? null : previewMinerIndex;
};
