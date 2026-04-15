import { fixedCharacterCountAll } from "./RenameOptionsModals/constants";
import {
  type CustomPropertyOptionsValues,
  customPropertyTypes,
  fixedStringSections,
  type FixedValueOptionsValues,
  type QualifierOptionsValues,
} from "./RenameOptionsModals/types";
import { FixedValueType, QualifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

export const bulkRenameSeparatorIds = {
  dash: "dash",
  underscore: "underscore",
  period: "period",
  none: "none",
} as const;

export type BulkRenameSeparatorId = (typeof bulkRenameSeparatorIds)[keyof typeof bulkRenameSeparatorIds];

export const bulkRenameModes = {
  rename: "rename",
  worker: "worker",
} as const;

export type BulkRenameMode = (typeof bulkRenameModes)[keyof typeof bulkRenameModes];

export const bulkRenameSeparators: Record<
  BulkRenameSeparatorId,
  {
    label: string;
    value: string;
  }
> = {
  [bulkRenameSeparatorIds.dash]: { label: "Dash ( - )", value: "-" },
  [bulkRenameSeparatorIds.underscore]: { label: "Underscore ( _ )", value: "_" },
  [bulkRenameSeparatorIds.period]: { label: "Period ( . )", value: "." },
  [bulkRenameSeparatorIds.none]: { label: "None", value: "" },
};

type PropertyKind = "custom" | "fixed" | "qualifier";
type QualifierPropertySpec = readonly [string, string, QualifierType];
type FixedPropertySpec = readonly [string, string, FixedValueType, boolean];

type KebabCase<Value extends string> = Value extends `${infer First}${infer Rest}`
  ? Rest extends Uncapitalize<Rest>
    ? `${Lowercase<First>}${KebabCase<Rest>}`
    : `${Lowercase<First>}-${KebabCase<Rest>}`
  : Value;

export type BulkRenamePropertyOptions = CustomPropertyOptionsValues | FixedValueOptionsValues | QualifierOptionsValues;

export interface BulkRenamePropertyState {
  id: BulkRenamePropertyId;
  enabled: boolean;
  options: BulkRenamePropertyOptions;
}

export interface BulkRenamePreferences {
  separator: BulkRenameSeparatorId;
  properties: BulkRenamePropertyState[];
}

export interface BulkRenamePropertyDefinition {
  id: BulkRenamePropertyId;
  label: string;
  kind: PropertyKind;
  defaultOptions: BulkRenamePropertyOptions;
  guaranteesUniqueness?: boolean;
  fixedValueType?: FixedValueType;
  qualifierType?: QualifierType;
}

export interface BulkRenamePreviewMiner {
  counterIndex: number;
  deviceIdentifier: string;
  currentName: string;
  storedName: string;
  macAddress: string;
  serialNumber: string;
  minerName: string;
  model: string;
  manufacturer: string;
  workerName: string;
  rackLabel: string;
  rackPosition: string;
}

const hasNonEmptyUniquenessValue = (property: BulkRenamePropertyState, miner: BulkRenamePreviewMiner): boolean => {
  switch (property.id) {
    case bulkRenamePropertyIds.fixedMacAddress:
      return miner.macAddress.trim() !== "";
    case bulkRenamePropertyIds.fixedSerialNumber:
      return miner.serialNumber.trim() !== "";
    default:
      return false;
  }
};

export interface BulkRenamePropertyPreview {
  previewName: string;
  highlightedText?: string;
  highlightStartIndex?: number;
}

const defaultFixedValueOptions = {
  characterCount: fixedCharacterCountAll,
  stringSection: fixedStringSections.last,
} satisfies FixedValueOptionsValues;

const defaultCustomOptions = {
  type: customPropertyTypes.stringAndCounter,
  prefix: "",
  suffix: "",
  counterStart: 1,
  counterScale: 1,
  stringValue: "",
} satisfies CustomPropertyOptionsValues;

// [code key, UI label, backend FixedValueType, guaranteesUniqueness]
const sharedFixedPropertySpecs = [
  ["fixedMacAddress", "MAC address", FixedValueType.MAC_ADDRESS, true],
  ["fixedSerialNumber", "Serial number", FixedValueType.SERIAL_NUMBER, true],
  ["fixedModel", "Model", FixedValueType.MODEL, false],
  ["fixedManufacturer", "Manufacturer", FixedValueType.MANUFACTURER, false],
] as const;

const renameOnlyFixedPropertySpecs = [["fixedWorkerName", "Worker name", FixedValueType.WORKER_NAME, false]] as const;

const workerOnlyFixedPropertySpecs = [["fixedMinerName", "Miner name", FixedValueType.MINER_NAME, false]] as const;

const fixedPropertySpecs = [
  ...sharedFixedPropertySpecs,
  ...renameOnlyFixedPropertySpecs,
  ...workerOnlyFixedPropertySpecs,
] as const satisfies readonly FixedPropertySpec[];

// [code key, UI label, backend QualifierType]
const workerQualifierPropertySpecs = [
  ["qualifierRack", "Rack", QualifierType.RACK],
  ["qualifierRackPosition", "Rack position", QualifierType.RACK_POSITION],
] as const satisfies readonly QualifierPropertySpec[];

const customPropertySpec = ["custom", "Custom"] as const;

const propertySpecs = [...fixedPropertySpecs, ...workerQualifierPropertySpecs, customPropertySpec] as const;

type BulkRenamePropertyKey = (typeof propertySpecs)[number][0];

export const bulkRenamePropertyIds = Object.fromEntries(
  propertySpecs.map(([key]) => [key, key.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`)]),
) as {
  [Spec in (typeof propertySpecs)[number] as Spec[0]]: KebabCase<Spec[0]>;
};

export type BulkRenamePropertyId = (typeof bulkRenamePropertyIds)[keyof typeof bulkRenamePropertyIds];

const createQualifierPropertyDefinition = (
  key: string,
  label: string,
  qualifierType: QualifierType,
): BulkRenamePropertyDefinition => ({
  id: bulkRenamePropertyIds[key as keyof typeof bulkRenamePropertyIds],
  label,
  kind: "qualifier",
  qualifierType,
  defaultOptions: { prefix: "", suffix: "" } satisfies QualifierOptionsValues,
});

const BULK_RENAME_PROPERTY_DEFINITIONS: BulkRenamePropertyDefinition[] = [
  ...fixedPropertySpecs.map(([key, label, fixedValueType, guaranteesUniqueness]) => ({
    id: bulkRenamePropertyIds[key],
    label,
    kind: "fixed" as const,
    fixedValueType,
    guaranteesUniqueness,
    defaultOptions: defaultFixedValueOptions,
  })),
  ...workerQualifierPropertySpecs.map((spec) => createQualifierPropertyDefinition(spec[0], spec[1], spec[2])),
  {
    id: bulkRenamePropertyIds[customPropertySpec[0]],
    label: customPropertySpec[1],
    kind: "custom",
    defaultOptions: defaultCustomOptions,
  },
];

const BULK_RENAME_MODE_PROPERTY_KEYS: Record<BulkRenameMode, readonly BulkRenamePropertyKey[]> = {
  [bulkRenameModes.rename]: [
    "fixedMacAddress",
    "fixedSerialNumber",
    "fixedWorkerName",
    "fixedModel",
    "fixedManufacturer",
    "qualifierRack",
    "qualifierRackPosition",
    "custom",
  ],
  [bulkRenameModes.worker]: [
    "fixedMacAddress",
    "fixedSerialNumber",
    "fixedMinerName",
    "fixedModel",
    "fixedManufacturer",
    "qualifierRack",
    "qualifierRackPosition",
    "custom",
  ],
};

const getBulkRenameModeDefinitions = (mode: BulkRenameMode): BulkRenamePropertyDefinition[] =>
  BULK_RENAME_MODE_PROPERTY_KEYS[mode].map((key) => {
    const definition = propertyDefinitionsById.get(bulkRenamePropertyIds[key]);

    if (definition === undefined) {
      throw new Error(`Unknown bulk rename property key: ${key}`);
    }

    return definition;
  });

const propertyDefinitionsById = new Map(
  BULK_RENAME_PROPERTY_DEFINITIONS.map((definition) => [definition.id, definition]),
);

const cloneOptions = (options: BulkRenamePropertyOptions): BulkRenamePropertyOptions => {
  return JSON.parse(JSON.stringify(options)) as BulkRenamePropertyOptions;
};

const mergeBulkRenamePropertyOptions = (
  definition: BulkRenamePropertyDefinition,
  options?: BulkRenamePropertyOptions,
): BulkRenamePropertyOptions => ({
  ...cloneOptions(definition.defaultOptions),
  ...(typeof options === "object" && options !== null ? options : {}),
});

const createBulkRenamePropertyState = (
  definition: BulkRenamePropertyDefinition,
  persistedState?: Partial<BulkRenamePropertyState>,
): BulkRenamePropertyState => ({
  id: definition.id,
  enabled: persistedState?.enabled ?? false,
  options: mergeBulkRenamePropertyOptions(definition, persistedState?.options),
});

export const getBulkRenamePropertyDefinition = (id: BulkRenamePropertyId): BulkRenamePropertyDefinition => {
  const definition = propertyDefinitionsById.get(id);

  if (definition === undefined) {
    throw new Error(`Unknown bulk rename property id: ${id}`);
  }

  return definition;
};

export const createDefaultBulkRenamePreferences = (
  mode: BulkRenameMode = bulkRenameModes.rename,
): BulkRenamePreferences => ({
  separator: bulkRenameSeparatorIds.dash,
  properties: getBulkRenameModeDefinitions(mode).map((definition) => createBulkRenamePropertyState(definition)),
});

export const normalizeBulkRenamePreferences = (
  preferences?: Partial<BulkRenamePreferences> | null,
  mode: BulkRenameMode = bulkRenameModes.rename,
): BulkRenamePreferences => {
  const defaults = createDefaultBulkRenamePreferences(mode);
  const separator =
    preferences?.separator !== undefined && preferences.separator in bulkRenameSeparators
      ? (preferences.separator as BulkRenameSeparatorId)
      : defaults.separator;

  const availableDefinitions = new Map(
    defaults.properties.map((property) => [property.id, getBulkRenamePropertyDefinition(property.id)]),
  );
  const persistedStates = preferences?.properties ?? [];
  const seen = new Set<BulkRenamePropertyId>();
  const properties: BulkRenamePropertyState[] = [];

  for (const state of persistedStates) {
    const definition = availableDefinitions.get(state.id);
    if (definition === undefined || seen.has(state.id)) {
      continue;
    }

    seen.add(state.id);
    properties.push(createBulkRenamePropertyState(definition, state));
  }

  for (const state of defaults.properties) {
    if (seen.has(state.id)) {
      continue;
    }

    properties.push(state);
  }

  return {
    separator,
    properties,
  };
};

export const updateBulkRenameProperty = (
  preferences: BulkRenamePreferences,
  propertyId: BulkRenamePropertyId,
  updater: (property: BulkRenamePropertyState) => BulkRenamePropertyState,
): BulkRenamePreferences => ({
  ...preferences,
  properties: preferences.properties.map((property) => (property.id === propertyId ? updater(property) : property)),
});

export const reorderBulkRenameProperties = (
  preferences: BulkRenamePreferences,
  activeId: BulkRenamePropertyId,
  overId: BulkRenamePropertyId,
): BulkRenamePreferences => {
  const oldIndex = preferences.properties.findIndex((property) => property.id === activeId);
  const newIndex = preferences.properties.findIndex((property) => property.id === overId);

  if (oldIndex === -1 || newIndex === -1 || oldIndex === newIndex) {
    return preferences;
  }

  const properties = [...preferences.properties];
  const [movedProperty] = properties.splice(oldIndex, 1);
  properties.splice(newIndex, 0, movedProperty);

  return {
    ...preferences,
    properties,
  };
};

export const getEnabledBulkRenameProperties = (preferences: BulkRenamePreferences): BulkRenamePropertyState[] =>
  preferences.properties.filter((property) => property.enabled);

export const isBulkRenamePropertyUniquenessGuaranteeing = (
  property: BulkRenamePropertyState,
  previewMiners: BulkRenamePreviewMiner[] | null = null,
): boolean => {
  const definition = getBulkRenamePropertyDefinition(property.id);

  if (definition.guaranteesUniqueness) {
    const options = property.options as FixedValueOptionsValues;
    return (
      options.characterCount === fixedCharacterCountAll &&
      previewMiners !== null &&
      previewMiners.every((miner) => hasNonEmptyUniquenessValue(property, miner))
    );
  }

  if (definition.kind !== "custom") {
    return false;
  }

  const options = property.options as CustomPropertyOptionsValues;
  return (
    options.counterStart !== undefined &&
    (options.type === customPropertyTypes.counterOnly || options.type === customPropertyTypes.stringAndCounter)
  );
};

export const hasUniquenessGuaranteeingProperty = (
  preferences: BulkRenamePreferences,
  previewMiners: BulkRenamePreviewMiner[] | null = null,
): boolean =>
  getEnabledBulkRenameProperties(preferences).some((property) =>
    isBulkRenamePropertyUniquenessGuaranteeing(property, previewMiners),
  );

export const shouldWarnAboutBulkRenameDuplicates = (
  selectionCount: number,
  preferences: BulkRenamePreferences,
  previewMiners: BulkRenamePreviewMiner[] | null = null,
): boolean => selectionCount > 1 && !hasUniquenessGuaranteeingProperty(preferences, previewMiners);
