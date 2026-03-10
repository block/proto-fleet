import { describe, expect, it } from "vitest";
import {
  type BulkRenamePreviewMiner,
  bulkRenamePropertyIds,
  bulkRenameSeparatorIds,
  createDefaultBulkRenamePreferences,
} from "./bulkRenameDefinitions";
import {
  buildBulkRenameConfig,
  evaluateBulkRenamePreviewName,
  findBulkRenamePropertyPreviewMinerIndex,
  hasEmptyBulkRenameConfig,
  hasNoBulkRenameChanges,
  mapSnapshotsToBulkRenamePreviewMiners,
  shouldShowBulkRenameNoChangesWarning,
  takePreviewMiners,
} from "./bulkRenamePreview";
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

describe("bulkRenamePreview", () => {
  it("builds a config from enabled properties in persisted order", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.separator = bulkRenameSeparatorIds.period;

    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedManufacturer) {
        return { ...property, enabled: true };
      }

      if (property.id === bulkRenamePropertyIds.custom) {
        return {
          ...property,
          enabled: true,
          options: {
            ...property.options,
            type: customPropertyTypes.counterOnly,
            counterStart: 7,
            counterScale: 3,
          },
        };
      }

      return property;
    });

    const config = buildBulkRenameConfig(preferences);

    expect(config.separator).toBe(".");
    expect(config.properties).toHaveLength(2);
    expect(config.properties[0].kind.case).toBe("fixedValue");
    expect(config.properties[1].kind.case).toBe("counter");
  });

  it("evaluates preview names with fixed values, counters, and omitted blank worker names", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedManufacturer) {
        return { ...property, enabled: true };
      }

      if (property.id === bulkRenamePropertyIds.fixedWorkerName) {
        return { ...property, enabled: true };
      }

      if (property.id === bulkRenamePropertyIds.custom) {
        return {
          ...property,
          enabled: true,
          options: {
            ...property.options,
            type: customPropertyTypes.stringAndCounter,
            prefix: "M",
            suffix: "",
            counterStart: 1,
            counterScale: 2,
            stringValue: "",
          },
        };
      }

      return property;
    });

    const config = buildBulkRenameConfig(preferences);
    expect(evaluateBulkRenamePreviewName(config, basePreviewMiner, 0)).toBe("worker-01-Bitmain-M01");
    expect(
      evaluateBulkRenamePreviewName(
        config,
        {
          ...basePreviewMiner,
          workerName: "",
        },
        1,
      ),
    ).toBe("Bitmain-M02");
  });

  it("treats empty or unchanged bulk rename results as no-op changes", () => {
    const defaults = createDefaultBulkRenamePreferences();

    expect(hasNoBulkRenameChanges(defaults, [basePreviewMiner])).toBe(true);

    const unchangedPreferences = createDefaultBulkRenamePreferences();
    unchangedPreferences.properties = unchangedPreferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedMacAddress) {
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
      hasNoBulkRenameChanges(unchangedPreferences, [
        {
          ...basePreviewMiner,
          currentName: "AA:BB:CC:DD:EE:FF",
          storedName: "AA:BB:CC:DD:EE:FF",
        },
      ]),
    ).toBe(true);
  });

  it("compares no-change checks against stored miner names, not display-name fallbacks", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.custom) {
        return {
          ...property,
          enabled: true,
          options: {
            ...property.options,
            type: customPropertyTypes.stringOnly,
            stringValue: "Bitmain S21 XP",
          },
        };
      }

      return property;
    });

    expect(
      hasNoBulkRenameChanges(preferences, [
        {
          ...basePreviewMiner,
          currentName: "Bitmain S21 XP",
          storedName: "",
        },
      ]),
    ).toBe(false);
  });

  it("uses each preview miner's real counter index for no-op detection", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.custom) {
        return {
          ...property,
          enabled: true,
          options: {
            ...property.options,
            type: customPropertyTypes.counterOnly,
            counterStart: 1,
            counterScale: 3,
          },
        };
      }

      return property;
    });

    expect(
      hasNoBulkRenameChanges(preferences, [
        {
          ...basePreviewMiner,
          counterIndex: 69,
          currentName: "070",
          storedName: "070",
        },
      ]),
    ).toBe(true);
  });

  it("preserves the provided table order when assigning preview counter indices", () => {
    const previewMiners = mapSnapshotsToBulkRenamePreviewMiners([
      {
        deviceIdentifier: "device-2",
        name: "Alpha",
        manufacturer: "Bitmain",
        model: "S21",
        macAddress: "AA:AA:AA:AA:AA:02",
        serialNumber: "SER-2",
      },
      {
        deviceIdentifier: "device-3",
        name: "Zulu",
        manufacturer: "Avalon",
        model: "A1",
        macAddress: "AA:AA:AA:AA:AA:03",
        serialNumber: "SER-3",
      },
      {
        deviceIdentifier: "device-1",
        name: "Beta",
        manufacturer: "Bitmain",
        model: "S19",
        macAddress: "AA:AA:AA:AA:AA:01",
        serialNumber: "SER-1",
      },
    ]);

    expect(previewMiners.map((miner) => [miner.deviceIdentifier, miner.counterIndex])).toEqual([
      ["device-2", 0],
      ["device-3", 1],
      ["device-1", 2],
    ]);
  });

  it("does not reorder rows when manufacturer or model values are blank", () => {
    const previewMiners = mapSnapshotsToBulkRenamePreviewMiners([
      {
        deviceIdentifier: "device-1",
        name: "One",
        manufacturer: "A",
        model: "",
        macAddress: "AA:AA:AA:AA:AA:01",
        serialNumber: "SER-1",
      },
      {
        deviceIdentifier: "device-2",
        name: "Two",
        manufacturer: "",
        model: "A",
        macAddress: "AA:AA:AA:AA:AA:02",
        serialNumber: "SER-2",
      },
    ]);

    expect(previewMiners.map((miner) => miner.deviceIdentifier)).toEqual(["device-1", "device-2"]);
  });

  it("preserves worker names when building preview miners from snapshots", () => {
    const previewMiners = mapSnapshotsToBulkRenamePreviewMiners([
      {
        deviceIdentifier: "device-1",
        name: "One",
        manufacturer: "Bitmain",
        model: "S21",
        macAddress: "AA:AA:AA:AA:AA:01",
        serialNumber: "SER-1",
        workerName: "worker-a",
      },
      {
        deviceIdentifier: "device-2",
        name: "Two",
        manufacturer: "Bitmain",
        model: "S21",
        macAddress: "AA:AA:AA:AA:AA:02",
        serialNumber: "SER-2",
      },
    ]);

    expect(previewMiners.map((miner) => miner.workerName)).toEqual(["worker-a", ""]);
  });

  it("does not duplicate rows when preview miners are already a partial sample", () => {
    const previewMiners = [
      { deviceIdentifier: "device-1" },
      { deviceIdentifier: "device-2" },
      { deviceIdentifier: "device-3" },
      { deviceIdentifier: "device-4" },
    ];

    expect(takePreviewMiners(previewMiners, 10)).toEqual({
      miners: previewMiners,
      showEllipsis: true,
    });
  });

  it("does not treat an empty preview set as unchanged when a real name config exists", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedMacAddress) {
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

    expect(hasNoBulkRenameChanges(preferences, [])).toBe(false);
  });

  it("treats an empty rename config as a no-change warning even without validation miners", () => {
    const preferences = createDefaultBulkRenamePreferences();

    expect(hasEmptyBulkRenameConfig(preferences)).toBe(true);
    expect(shouldShowBulkRenameNoChangesWarning(preferences, null)).toBe(true);
  });

  it("does not show a no-change warning without validation miners when the config has real properties", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedMacAddress) {
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

    expect(hasEmptyBulkRenameConfig(preferences)).toBe(false);
    expect(shouldShowBulkRenameNoChangesWarning(preferences, null)).toBe(false);
  });

  it("prefers a preview miner that has a value for non-custom property previews", () => {
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
      findBulkRenamePropertyPreviewMinerIndex(preferences, bulkRenamePropertyIds.fixedSerialNumber, [
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-1",
          serialNumber: "",
        },
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-2",
          serialNumber: "SER987654",
        },
      ]),
    ).toBe(1);
  });

  it("returns no preview miner when a non-custom property is blank for every previewed miner", () => {
    const preferences = createDefaultBulkRenamePreferences();
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedWorkerName) {
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
      findBulkRenamePropertyPreviewMinerIndex(preferences, bulkRenamePropertyIds.fixedWorkerName, [
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-1",
          workerName: "",
        },
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-2",
          workerName: "",
        },
      ]),
    ).toBeNull();
  });

  it("keeps custom property previews on the first preview miner", () => {
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

      return property;
    });

    expect(
      findBulkRenamePropertyPreviewMinerIndex(preferences, bulkRenamePropertyIds.custom, [
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-1",
        },
        {
          ...basePreviewMiner,
          deviceIdentifier: "device-2",
        },
      ]),
    ).toBe(0);
  });
});
