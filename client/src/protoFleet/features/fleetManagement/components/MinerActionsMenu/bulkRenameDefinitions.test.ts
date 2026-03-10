import { describe, expect, it } from "vitest";
import {
  type BulkRenamePreviewMiner,
  bulkRenamePropertyIds,
  bulkRenameSeparatorIds,
  createDefaultBulkRenamePreferences,
  getEnabledBulkRenameProperties,
  hasUniquenessGuaranteeingProperty,
  normalizeBulkRenamePreferences,
  reorderBulkRenameProperties,
} from "./bulkRenameDefinitions";
import { customPropertyTypes, fixedStringSections } from "./RenameOptionsModals/types";

const basePreviewMiner: BulkRenamePreviewMiner = {
  counterIndex: 0,
  deviceIdentifier: "device-1",
  currentName: "Proto Rig",
  storedName: "Proto Rig",
  macAddress: "AA:BB:CC:DD:EE:FF",
  serialNumber: "SER123456",
  model: "S21 XP",
  manufacturer: "Bitmain",
  workerName: "worker-01",
};

const legacyHiddenPropertyId = "fixed-location";

describe("bulkRenameDefinitions", () => {
  it("normalizes persisted preferences, drops hidden properties, and appends known ones", () => {
    const normalized = normalizeBulkRenamePreferences({
      separator: bulkRenameSeparatorIds.underscore,
      properties: [
        {
          id: bulkRenamePropertyIds.fixedSerialNumber,
          enabled: true,
          options: {
            characterCount: 4,
            stringSection: fixedStringSections.last,
          },
        },
        {
          id: legacyHiddenPropertyId as never,
          enabled: true,
          options: {
            characterCount: 4,
            stringSection: fixedStringSections.last,
          },
        },
      ],
    });

    expect(normalized.separator).toBe(bulkRenameSeparatorIds.underscore);
    expect(normalized.properties[0].id).toBe(bulkRenamePropertyIds.fixedSerialNumber);
    expect(normalized.properties.find((property) => String(property.id) === legacyHiddenPropertyId)).toBeUndefined();
    expect(normalized.properties).toHaveLength(6);
  });

  it("tracks uniqueness-guaranteeing properties and reorder operations", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.custom) {
        return {
          ...property,
          enabled: true,
          options: {
            ...property.options,
            type: customPropertyTypes.stringOnly,
            stringValue: "Fleet",
          },
        };
      }

      if (property.id === bulkRenamePropertyIds.fixedMacAddress) {
        return { ...property, enabled: true };
      }

      return property;
    });

    expect(getEnabledBulkRenameProperties(preferences)).toHaveLength(2);
    expect(hasUniquenessGuaranteeingProperty(preferences, [basePreviewMiner])).toBe(true);

    const reordered = reorderBulkRenameProperties(
      preferences,
      bulkRenamePropertyIds.custom,
      bulkRenamePropertyIds.fixedMacAddress,
    );

    expect(reordered.properties[0].id).toBe(bulkRenamePropertyIds.custom);
  });

  it("does not treat truncated unique fixed values as uniqueness-guaranteeing", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedMacAddress) {
        return {
          ...property,
          enabled: true,
          options: {
            characterCount: 4,
            stringSection: fixedStringSections.last,
          },
        };
      }

      return property;
    });

    expect(hasUniquenessGuaranteeingProperty(preferences, [basePreviewMiner])).toBe(false);
  });

  it("does not treat full-length unique fixed values as guaranteed when some miners are missing them", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedSerialNumber) {
        return {
          ...property,
          enabled: true,
          options: {
            characterCount: "all",
            stringSection: fixedStringSections.last,
          },
        };
      }

      return property;
    });

    expect(
      hasUniquenessGuaranteeingProperty(preferences, [
        basePreviewMiner,
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-2",
          serialNumber: "",
        },
      ]),
    ).toBe(false);
  });

  it("does not treat counter-based custom properties as unique when counter start is missing", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.custom) {
        return {
          ...property,
          enabled: true,
          options: {
            ...property.options,
            type: customPropertyTypes.counterOnly,
            counterStart: undefined,
          },
        };
      }

      return property;
    });

    expect(hasUniquenessGuaranteeingProperty(preferences, [basePreviewMiner])).toBe(false);
  });
});
