import { create } from "@bufbuild/protobuf";
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
import {
  bulkRenameModes,
  bulkRenameSeparators,
  getBulkRenamePropertyDefinition,
  getEnabledBulkRenameProperties,
} from "@/shared/components/BulkRename/bulkRenameDefinitions";
import type {
  BulkRenameMode,
  BulkRenamePreferences,
  BulkRenamePreviewMiner,
  BulkRenamePropertyState,
} from "@/shared/components/BulkRename/bulkRenameDefinitions";
import { evaluateBulkRenamePreviewName } from "@/shared/components/BulkRename/bulkRenamePreview";
import {
  type CustomPropertyOptionsValues,
  customPropertyTypes,
  fixedStringSections,
  type FixedValueOptionsValues,
  type QualifierOptionsValues,
} from "@/shared/components/BulkRename/RenameOptionsModals/types";

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

const fixedValueTypes = {
  macAddress: FixedValueType.MAC_ADDRESS,
  serialNumber: FixedValueType.SERIAL_NUMBER,
  workerName: FixedValueType.WORKER_NAME,
  minerName: FixedValueType.MINER_NAME,
  model: FixedValueType.MODEL,
  manufacturer: FixedValueType.MANUFACTURER,
};

const qualifierTypes = {
  rackLabel: QualifierType.RACK,
  rackPosition: QualifierType.RACK_POSITION,
};

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

  if (definition.fixedField) {
    const options = property.options as FixedValueOptionsValues;

    return create(NamePropertySchema, {
      kind: {
        case: "fixedValue",
        value: create(FixedValuePropertySchema, {
          type: fixedValueTypes[definition.fixedField],
          characterCount: options.characterCount === "all" ? undefined : options.characterCount,
          section: getFixedValueSection(options),
        }),
      },
    });
  }

  if (!definition.qualifierField) return null;
  const options = property.options as QualifierOptionsValues;

  return create(NamePropertySchema, {
    kind: {
      case: "qualifier",
      value: create(QualifierPropertySchema, {
        type: qualifierTypes[definition.qualifierField],
        prefix: options.prefix.trim(),
        suffix: options.suffix.trim(),
      }),
    },
  });
}

export const buildBulkRenameConfig = (preferences: BulkRenamePreferences): MinerNameConfig =>
  create(MinerNameConfigSchema, {
    separator: bulkRenameSeparators[preferences.separator].value,
    properties: getEnabledBulkRenameProperties(preferences)
      .map(buildNameProperty)
      .filter((property): property is NameProperty => property !== null),
  });

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

  const previewNames = previewMiners.map((miner) =>
    evaluateBulkRenamePreviewName(preferences, miner, miner.counterIndex),
  );

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
  | "rackPosition"
  | "placement"
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
  rackLabel: snapshot.placement?.rack?.label ?? "",
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
