import { test } from "../fixtures/pageFixtures";
import {
  addSelectableMinersToSlots,
  createZoneName,
  expectGridRackLabels,
  expectListRackLabels,
} from "../helpers/racksHelpers";
import {
  RACK_COLUMNS,
  RACK_ROWS,
  useRacksHooks,
  VALIDATION_RACK_COLUMNS,
  VALIDATION_RACK_ROWS,
} from "../helpers/racksTestSetup";
import { type RackSelectorMiner } from "../pages/racks";

test.describe("Racks - management", () => {
  useRacksHooks();

  test("Multiple racks support zone filtering and miner sorting", async ({ racksPage }) => {
    const zoneA = createZoneName("A");
    const zoneB = createZoneName("B");
    const createdRackLabels: string[] = [];

    await test.step("Create rack A-01 with three miners", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(zoneA);
      await racksPage.inputRackLabel("A-01");
      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(RACK_COLUMNS);
      await racksPage.inputRows(RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();
      await addSelectableMinersToSlots(racksPage, 3, [1, 2, 3]);
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast("A-01");
      await racksPage.clickViewGrid();
      await racksPage.validateRackCardVisible("A-01", zoneA);
      createdRackLabels.push("A-01");
    });

    await test.step("Create rack A-02 with two miners", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(zoneA);
      await racksPage.inputRackLabel("A-02");
      await racksPage.clickContinueFromRackSettings();
      await addSelectableMinersToSlots(racksPage, 2, [1, 2]);
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast("A-02");
      await racksPage.clickViewGrid();
      await racksPage.validateRackCardVisible("A-02", zoneA);
      createdRackLabels.push("A-02");
    });

    await test.step("Create rack B-01 with one miner", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(zoneB);
      await racksPage.inputRackLabel("B-01");
      await racksPage.clickContinueFromRackSettings();
      await addSelectableMinersToSlots(racksPage, 1, [1]);
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast("B-01");
      await racksPage.clickViewGrid();
      await racksPage.validateRackCardVisible("B-01", zoneB);
      createdRackLabels.push("B-01");
    });

    await test.step("Filter racks by zone in grid view", async () => {
      await racksPage.applyZoneFilter([zoneA]);
      await expectGridRackLabels(racksPage, ["A-01", "A-02"]);

      await racksPage.applyZoneFilter([zoneB]);
      await expectGridRackLabels(racksPage, ["B-01"]);

      await racksPage.toggleAllZoneFilters();
      await expectGridRackLabels(racksPage, createdRackLabels);

      await racksPage.toggleAllZoneFilters();
    });

    await test.step("Filter racks by zone in list view", async () => {
      await racksPage.clickViewList();

      await racksPage.applyZoneFilter([zoneA]);
      await expectListRackLabels(racksPage, ["A-01", "A-02"]);

      await racksPage.applyZoneFilter([zoneB]);
      await expectListRackLabels(racksPage, ["B-01"]);

      await racksPage.toggleAllZoneFilters();
      await expectListRackLabels(racksPage, createdRackLabels);

      await racksPage.toggleAllZoneFilters();
      await racksPage.clickViewGrid();
    });

    await test.step("Validate default grid order and miners sort order", async () => {
      await expectGridRackLabels(racksPage, ["A-01", "A-02", "B-01"]);
      await racksPage.selectGridSort("Miners");
      await expectGridRackLabels(racksPage, ["B-01", "A-02", "A-01"]);
    });
  });

  test("Rack settings validation blocks invalid input and miner overflow until corrected", async ({ racksPage }) => {
    const validationZone = createZoneName("A");
    let generatedRackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];

    await test.step("Validate required label and invalid dimensions", async () => {
      // Zone is optional now; the label is required and empty by default, so
      // continuing without typing one surfaces the label error.
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(validationZone);
      generatedRackLabel = "A-01";

      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(0);
      await racksPage.inputRows(13);
      await racksPage.clickContinueFromRackSettings();

      await racksPage.validateRackSettingsFieldError("rack-label", "A label is required");
      await racksPage.validateRackSettingsFieldError("rack-columns", "Columns must be a whole number between 1 and 12");
      await racksPage.validateRackSettingsFieldError("rack-rows", "Rows must be a whole number between 1 and 12");
      await racksPage.validateTitleInModal("Rack settings");
    });

    await test.step("Correct rack settings and continue", async () => {
      await racksPage.inputRackLabel(generatedRackLabel);
      await racksPage.inputColumns(VALIDATION_RACK_COLUMNS);
      await racksPage.inputRows(VALIDATION_RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();

      await racksPage.validateRackConfiguration(VALIDATION_RACK_COLUMNS, VALIDATION_RACK_ROWS, "Bottom left");
      await racksPage.validateAssignedMinersCount(0, 1);
    });

    await test.step("Validate miner overflow error and recover", async () => {
      await racksPage.clickAddMiners();
      await racksPage.waitForMinerSelectorListToLoad();

      const selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(2);
      selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
      await racksPage.selectMinersInSelectorByIndex(selectableMinerIndexes);
      await racksPage.clickContinueInMinerSelector();

      await racksPage.validateMinerSelectorOverflowError(2, 1);
      await racksPage.toggleMinerInSelectorByIpAddress(selectedMiners[1].ipAddress);
      await racksPage.clickContinueInMinerSelector();
    });

    await test.step("Assign remaining miner and save the rack", async () => {
      await racksPage.clickAssignByNetwork();
      await racksPage.validateMinersAssignedByNetwork([selectedMiners[0]]);
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast(generatedRackLabel);
      await racksPage.validateRackCardVisible(generatedRackLabel, validationZone);
      await racksPage.validateRackCardGrid(
        generatedRackLabel,
        validationZone,
        VALIDATION_RACK_COLUMNS,
        VALIDATION_RACK_ROWS,
      );
    });
  });
});
