import { create } from "@bufbuild/protobuf";
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
import {
  CharacterSection,
  CounterPropertySchema,
  FixedValuePropertySchema,
  FixedValueType,
  type MinerNameConfig,
  MinerNameConfigSchema,
  type NameProperty,
  NamePropertySchema,
  QualifierPropertySchema,
  StringAndCounterPropertySchema,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerStateSnapshot } from "@/protoFleet/store";

const formatCounter = (value: number, scale: number): string => value.toString().padStart(scale, "0");

const normalizeCurrentName = (snapshot: Pick<MinerStateSnapshot, "name" | "manufacturer" | "model">): string => {
  if (snapshot.name.trim() !== "") {
    return snapshot.name;
  }

  return `${snapshot.manufacturer} ${snapshot.model}`.trim();
};

const buildNameProperty = (property: BulkRenamePropertyState): NameProperty | null => {
  const definition = getBulkRenamePropertyDefinition(property.id);

  if (definition.kind === "custom") {
    const options = property.options as CustomPropertyOptionsValues;

    if (options.type === customPropertyTypes.stringOnly) {
      const stringValue = options.stringValue.trim();
      if (stringValue === "") {
        return null;
      }

      return create(NamePropertySchema, {
        kind: {
          case: "stringValue",
          value: create(StringPropertySchema, { value: stringValue }),
        },
      });
    }

    if (options.counterStart === undefined) {
      return null;
    }

    if (options.type === customPropertyTypes.counterOnly) {
      return create(NamePropertySchema, {
        kind: {
          case: "counter",
          value: create(CounterPropertySchema, {
            counterStart: options.counterStart,
            counterScale: options.counterScale,
          }),
        },
      });
    }

    return create(NamePropertySchema, {
      kind: {
        case: "stringAndCounter",
        value: create(StringAndCounterPropertySchema, {
          prefix: options.prefix.trim(),
          suffix: options.suffix.trim(),
          counterStart: options.counterStart,
          counterScale: options.counterScale,
        }),
      },
    });
  }

  if (definition.kind === "fixed") {
    const options = property.options as FixedValueOptionsValues;

    return create(NamePropertySchema, {
      kind: {
        case: "fixedValue",
        value: create(FixedValuePropertySchema, {
          type: definition.fixedValueType,
          characterCount: options.characterCount === "all" ? undefined : options.characterCount,
          section:
            options.characterCount === "all"
              ? undefined
              : options.stringSection === fixedStringSections.last
                ? CharacterSection.LAST
                : CharacterSection.FIRST,
        }),
      },
    });
  }

  const options = property.options as QualifierOptionsValues;

  return create(NamePropertySchema, {
    kind: {
      case: "qualifier",
      value: create(QualifierPropertySchema, {
        type: definition.qualifierType,
        prefix: options.prefix.trim(),
        suffix: options.suffix.trim(),
      }),
    },
  });
};

const evaluateNameProperty = (property: NameProperty, miner: BulkRenamePreviewMiner, counterIndex: number): string => {
  switch (property.kind.case) {
    case "stringAndCounter":
      return `${property.kind.value.prefix}${formatCounter(
        property.kind.value.counterStart + counterIndex,
        property.kind.value.counterScale,
      )}${property.kind.value.suffix}`;
    case "counter":
      return formatCounter(property.kind.value.counterStart + counterIndex, property.kind.value.counterScale);
    case "stringValue":
      return property.kind.value.value;
    case "fixedValue": {
      let rawValue = "";

      switch (property.kind.value.type) {
        case FixedValueType.MAC_ADDRESS:
          rawValue = miner.macAddress;
          break;
        case FixedValueType.SERIAL_NUMBER:
          rawValue = miner.serialNumber;
          break;
        case FixedValueType.WORKER_NAME:
          rawValue = miner.workerName;
          break;
        case FixedValueType.MODEL:
          rawValue = miner.model;
          break;
        case FixedValueType.MANUFACTURER:
          rawValue = miner.manufacturer;
          break;
        case FixedValueType.LOCATION:
        case FixedValueType.UNSPECIFIED:
          return "";
      }

      if (rawValue === "") {
        return "";
      }

      if (property.kind.value.characterCount === undefined) {
        return rawValue;
      }

      const runes = Array.from(rawValue);
      const characterCount = property.kind.value.characterCount;
      if (characterCount >= runes.length) {
        return rawValue;
      }

      return property.kind.value.section === CharacterSection.LAST
        ? runes.slice(-characterCount).join("")
        : runes.slice(0, characterCount).join("");
    }
    case "qualifier":
    case undefined:
      return "";
  }
};

const evaluateBulkRenamePropertySegment = (
  property: BulkRenamePropertyState,
  miner: BulkRenamePreviewMiner,
  counterIndex: number,
): string => {
  const nameProperty = buildNameProperty(property);

  return nameProperty === null ? "" : evaluateNameProperty(nameProperty, miner, counterIndex);
};

export const buildBulkRenameConfig = (preferences: BulkRenamePreferences): MinerNameConfig =>
  create(MinerNameConfigSchema, {
    separator: bulkRenameSeparators[preferences.separator].value,
    properties: getEnabledBulkRenameProperties(preferences)
      .map(buildNameProperty)
      .filter((property): property is NameProperty => property !== null),
  });

export const evaluateBulkRenamePreviewName = (
  config: MinerNameConfig,
  miner: BulkRenamePreviewMiner,
  counterIndex: number,
): string => {
  const segments = config.properties
    .map((property) => evaluateNameProperty(property, miner, counterIndex))
    .filter((segment) => segment.trim() !== "");

  return segments.join(config.separator).trim();
};

export const hasEmptyBulkRenameConfig = (preferences: BulkRenamePreferences): boolean =>
  buildBulkRenameConfig(preferences).properties.length === 0;

export const hasNoBulkRenameChanges = (
  preferences: BulkRenamePreferences,
  previewMiners: BulkRenamePreviewMiner[],
): boolean => {
  if (getEnabledBulkRenameProperties(preferences).length === 0 || hasEmptyBulkRenameConfig(preferences)) {
    return true;
  }

  if (previewMiners.length === 0) {
    return false;
  }

  const config = buildBulkRenameConfig(preferences);
  const previewNames = previewMiners.map((miner) => evaluateBulkRenamePreviewName(config, miner, miner.counterIndex));

  if (previewNames.every((name) => name.trim() === "")) {
    return true;
  }

  return previewNames.every((name, index) => name.trim() === previewMiners[index]?.storedName.trim());
};

export const shouldShowBulkRenameNoChangesWarning = (
  preferences: BulkRenamePreferences,
  previewMiners: BulkRenamePreviewMiner[] | null,
): boolean =>
  hasEmptyBulkRenameConfig(preferences) ||
  (previewMiners !== null && hasNoBulkRenameChanges(preferences, previewMiners));

export const getMinerPreviewName = (
  snapshot: Pick<MinerStateSnapshot, "deviceIdentifier" | "name" | "manufacturer" | "model">,
): string => normalizeCurrentName(snapshot);

type BulkRenamePreviewSnapshot = Pick<
  MinerStateSnapshot,
  "deviceIdentifier" | "name" | "manufacturer" | "model" | "macAddress" | "serialNumber"
> & {
  workerName?: string;
};

export const mapSnapshotToBulkRenamePreviewMiner = (
  snapshot: BulkRenamePreviewSnapshot,
  counterIndex: number,
): BulkRenamePreviewMiner => ({
  counterIndex,
  deviceIdentifier: snapshot.deviceIdentifier,
  currentName: normalizeCurrentName(snapshot),
  storedName: snapshot.name,
  macAddress: snapshot.macAddress,
  serialNumber: snapshot.serialNumber,
  model: snapshot.model,
  manufacturer: snapshot.manufacturer,
  workerName: snapshot.workerName ?? "",
});

export const mapSnapshotsToBulkRenamePreviewMiners = (
  snapshots: BulkRenamePreviewSnapshot[],
): BulkRenamePreviewMiner[] =>
  snapshots.map((snapshot, counterIndex) => mapSnapshotToBulkRenamePreviewMiner(snapshot, counterIndex));

export const takePreviewMiners = <T>(
  miners: T[],
  totalCount: number,
  maxVisibleMiners: number = 6,
): { miners: T[]; showEllipsis: boolean } => {
  if (maxVisibleMiners <= 0 || totalCount <= 0 || miners.length === 0) {
    return {
      miners: [],
      showEllipsis: false,
    };
  }

  if (maxVisibleMiners === 1) {
    return {
      miners: miners.slice(0, 1),
      showEllipsis: false,
    };
  }

  if (totalCount <= maxVisibleMiners || miners.length <= maxVisibleMiners) {
    return {
      miners,
      showEllipsis: totalCount > miners.length,
    };
  }

  const headCount = Math.floor(maxVisibleMiners / 2);
  const tailCount = maxVisibleMiners - headCount;

  return {
    miners: [...miners.slice(0, headCount), ...miners.slice(-tailCount)],
    showEllipsis: true,
  };
};

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
