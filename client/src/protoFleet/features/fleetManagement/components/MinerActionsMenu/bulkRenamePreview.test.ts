import { describe, expect, it } from "vitest";
import {
  bulkRenameModes,
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
  minerName: "Proto Rig",
  model: "S21 XP",
  manufacturer: "Bitmain",
  workerName: "worker-01",
  rackLabel: "Rack-A1",
  rackPosition: "12",
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

  it("evaluates preview names with fixed values and counters", () => {
    const preferences = createDefaultBulkRenamePreferences();
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
    expect(evaluateBulkRenamePreviewName(config, basePreviewMiner, 0)).toBe("Bitmain-M01");
    expect(evaluateBulkRenamePreviewName(config, basePreviewMiner, 1)).toBe("Bitmain-M02");
  });

  it("evaluates worker-name previews with miner name and rack qualifiers", () => {
    const preferences = createDefaultBulkRenamePreferences(bulkRenameModes.worker);
    preferences.properties = preferences.properties.map((property) => {
      if (property.id === bulkRenamePropertyIds.fixedMinerName) {
        return { ...property, enabled: true };
      }

      if (property.id === bulkRenamePropertyIds.qualifierRack) {
        return {
          ...property,
          enabled: true,
          options: {
            prefix: "",
            suffix: "",
          },
        };
      }

      if (property.id === bulkRenamePropertyIds.qualifierRackPosition) {
        return {
          ...property,
          enabled: true,
          options: {
            prefix: "",
            suffix: "",
          },
        };
      }

      return property;
    });

    const config = buildBulkRenameConfig(preferences);

    expect(evaluateBulkRenamePreviewName(config, basePreviewMiner, 0)).toBe("Proto Rig-Rack-A1-12");
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
        workerName: "worker-02",
        rackLabel: "",
        rackPosition: "",
      },
      {
        deviceIdentifier: "device-3",
        name: "Zulu",
        manufacturer: "Avalon",
        model: "A1",
        macAddress: "AA:AA:AA:AA:AA:03",
        serialNumber: "SER-3",
        workerName: "worker-03",
        rackLabel: "",
        rackPosition: "",
      },
      {
        deviceIdentifier: "device-1",
        name: "Beta",
        manufacturer: "Bitmain",
        model: "S19",
        macAddress: "AA:AA:AA:AA:AA:01",
        serialNumber: "SER-1",
        workerName: "worker-01",
        rackLabel: "",
        rackPosition: "",
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
        workerName: "worker-01",
        rackLabel: "",
        rackPosition: "",
      },
      {
        deviceIdentifier: "device-2",
        name: "Two",
        manufacturer: "",
        model: "A",
        macAddress: "AA:AA:AA:AA:AA:02",
        serialNumber: "SER-2",
        workerName: "worker-02",
        rackLabel: "",
        rackPosition: "",
      },
    ]);

    expect(previewMiners.map((miner) => miner.deviceIdentifier)).toEqual(["device-1", "device-2"]);
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

  it("limits compact previews to a single row without showing a desktop ellipsis marker", () => {
    const previewMiners = [{ deviceIdentifier: "device-1" }, { deviceIdentifier: "device-2" }];

    expect(takePreviewMiners(previewMiners, 2, 1)).toEqual({
      miners: [previewMiners[0]],
      showEllipsis: false,
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

  it("maps worker-mode previews from stored worker names instead of fleet display names", () => {
    const [previewMiner] = mapSnapshotsToBulkRenamePreviewMiners(
      [
        {
          deviceIdentifier: "device-1",
          name: "",
          manufacturer: "Bitmain",
          model: "S21 XP",
          macAddress: "AA:BB:CC:DD:EE:FF",
          serialNumber: "SER123456",
          workerName: "worker-99",
          rackLabel: "Rack-A1",
          rackPosition: "12",
        },
      ],
      bulkRenameModes.worker,
    );

    expect(previewMiner.currentName).toBe("worker-99");
    expect(previewMiner.storedName).toBe("worker-99");
    expect(previewMiner.minerName).toBe("Bitmain S21 XP");
  });
});
