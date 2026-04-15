import { create } from "@bufbuild/protobuf";
import {
  bulkRenameModes,
  bulkRenameSeparators,
  getBulkRenamePropertyDefinition,
  getEnabledBulkRenameProperties,
} from "./bulkRenameDefinitions";
import type {
  BulkRenameMode,
  BulkRenamePreferences,
  BulkRenamePreviewMiner,
  BulkRenamePropertyId,
  BulkRenamePropertyPreview,
  BulkRenamePropertyState,
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
  QualifierType,
  StringAndCounterPropertySchema,
  StringPropertySchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

function formatCounter(value: number, scale: number): string {
  return value.toString().padStart(scale, "0");
}

function getMinerDisplayName(snapshot: Pick<MinerStateSnapshot, "name" | "manufacturer" | "model">): string {
  if (snapshot.name.trim() !== "") {
    return snapshot.name;
  }

  return `${snapshot.manufacturer} ${snapshot.model}`.trim();
}

function getFixedValueSection(options: FixedValueOptionsValues): CharacterSection | undefined {
  if (options.characterCount === "all") {
    return undefined;
  }

  return options.stringSection === fixedStringSections.last ? CharacterSection.LAST : CharacterSection.FIRST;
}

function getFixedPreviewValue(type: FixedValueType, miner: BulkRenamePreviewMiner): string {
  switch (type) {
    case FixedValueType.MAC_ADDRESS:
      return miner.macAddress;
    case FixedValueType.SERIAL_NUMBER:
      return miner.serialNumber;
    case FixedValueType.WORKER_NAME:
      return miner.workerName;
    case FixedValueType.MINER_NAME:
      return miner.minerName;
    case FixedValueType.MODEL:
      return miner.model;
    case FixedValueType.MANUFACTURER:
      return miner.manufacturer;
    case FixedValueType.LOCATION:
    case FixedValueType.UNSPECIFIED:
      return "";
  }
}

function getQualifierPreviewValue(type: QualifierType, miner: BulkRenamePreviewMiner): string {
  switch (type) {
    case QualifierType.RACK:
      return miner.rackLabel;
    case QualifierType.RACK_POSITION:
      return miner.rackPosition;
    case QualifierType.BUILDING:
    case QualifierType.UNSPECIFIED:
      return "";
  }
}

function truncateFixedPreviewValue(value: string, characterCount: number, section: CharacterSection): string {
  const runes = Array.from(value);

  if (characterCount >= runes.length) {
    return value;
  }

  if (section === CharacterSection.LAST) {
    return runes.slice(-characterCount).join("");
  }

  return runes.slice(0, characterCount).join("");
}

function buildNameProperty(property: BulkRenamePropertyState): NameProperty | null {
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
          section: getFixedValueSection(options),
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
}

function evaluateNameProperty(property: NameProperty, miner: BulkRenamePreviewMiner, counterIndex: number): string {
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
      const rawValue = getFixedPreviewValue(property.kind.value.type, miner);

      if (rawValue === "") {
        return "";
      }

      if (property.kind.value.characterCount === undefined) {
        return rawValue;
      }

      const characterCount = property.kind.value.characterCount;
      const section =
        property.kind.value.section === CharacterSection.LAST ? CharacterSection.LAST : CharacterSection.FIRST;

      return truncateFixedPreviewValue(rawValue, characterCount, section);
    }
    case "qualifier": {
      const rawValue = getQualifierPreviewValue(property.kind.value.type, miner);

      if (rawValue.trim() === "") {
        return "";
      }

      return `${property.kind.value.prefix}${rawValue}${property.kind.value.suffix}`;
    }
    case undefined:
      return "";
  }
}

function evaluateBulkRenamePropertySegment(
  property: BulkRenamePropertyState,
  miner: BulkRenamePreviewMiner,
  counterIndex: number,
): string {
  const nameProperty = buildNameProperty(property);

  return nameProperty === null ? "" : evaluateNameProperty(nameProperty, miner, counterIndex);
}

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
): string => getMinerDisplayName(snapshot);

type BulkRenamePreviewSnapshot = Pick<
  MinerStateSnapshot,
  | "deviceIdentifier"
  | "name"
  | "manufacturer"
  | "model"
  | "macAddress"
  | "serialNumber"
  | "workerName"
  | "rackLabel"
  | "rackPosition"
>;

export const mapSnapshotToBulkRenamePreviewMiner = (
  snapshot: BulkRenamePreviewSnapshot,
  counterIndex: number,
  mode: BulkRenameMode = bulkRenameModes.rename,
): BulkRenamePreviewMiner => ({
  counterIndex,
  deviceIdentifier: snapshot.deviceIdentifier,
  currentName: mode === bulkRenameModes.worker ? snapshot.workerName : getMinerDisplayName(snapshot),
  storedName: mode === bulkRenameModes.worker ? snapshot.workerName : snapshot.name,
  macAddress: snapshot.macAddress,
  serialNumber: snapshot.serialNumber,
  minerName: getMinerDisplayName(snapshot),
  model: snapshot.model,
  manufacturer: snapshot.manufacturer,
  workerName: snapshot.workerName,
  rackLabel: snapshot.rackLabel,
  rackPosition: snapshot.rackPosition,
});

export const mapSnapshotsToBulkRenamePreviewMiners = (
  snapshots: BulkRenamePreviewSnapshot[],
  mode: BulkRenameMode = bulkRenameModes.rename,
): BulkRenamePreviewMiner[] =>
  snapshots.map((snapshot, counterIndex) => mapSnapshotToBulkRenamePreviewMiner(snapshot, counterIndex, mode));

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
