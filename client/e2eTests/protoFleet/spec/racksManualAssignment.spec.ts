import { test } from "../fixtures/pageFixtures";
import {
  AUTOMATION_ZONE,
  LARGE_RACK_COLUMNS,
  LARGE_RACK_ROWS,
  OVERVIEW_RACK_COLUMNS,
  OVERVIEW_RACK_ROWS,
  RACK_LABEL,
  useRacksHooks,
} from "../helpers/racksTestSetup";
import { type RackSelectorMiner } from "../pages/racks";

test.describe("Racks - manual assignment", () => {
  useRacksHooks();

  test("Manual rack assignment supports search, selection replacement, and saved slot state", async ({ racksPage }) => {
    let rackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];
    let selectableMinerIndexes: number[] = [];

    await test.step("Create a new 3x3 rack", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = RACK_LABEL;
      await racksPage.inputRackLabel(rackLabel);

      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(LARGE_RACK_COLUMNS);
      await racksPage.inputRows(LARGE_RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();
    });

    await test.step("Manage miners and add the first miner to the rack list", async () => {
      await racksPage.clickManageMiners();
      await racksPage.waitForMinerSelectorListToLoad();

      selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(2);
      selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
      test.expect(selectedMiners).toHaveLength(2);
      await racksPage.selectMinersInSelectorByIndex([selectableMinerIndexes[0]]);
      await racksPage.clickContinueInMinerSelector();
    });

    await test.step("Search and assign the second miner to slot 04", async () => {
      await racksPage.clickRackSlot(4);
      await racksPage.clickRackSlotMenuItem("Search miners");
      await racksPage.assignSearchMinerByIpAddress(selectedMiners[1].ipAddress);

      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[1].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[1].ipAddress, 4);
      await racksPage.validateRackSlotsHighlighted([4]);
    });

    await test.step("Open the assigned slot while the first miner is selected", async () => {
      await racksPage.selectRackMiner(selectedMiners[0].ipAddress);
      await racksPage.clickRackSlot(4);

      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[1].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[1].ipAddress, 4);
      await racksPage.validateMinerRowUnassigned(selectedMiners[0].ipAddress);
    });

    await test.step("Replace slot 04 assignment from the list", async () => {
      await racksPage.clickRackSlotMenuItem("Select from list");
      await racksPage.selectRackMiner(selectedMiners[0].ipAddress);

      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[0].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[0].ipAddress, 4);
      await racksPage.validateMinerRowUnassigned(selectedMiners[1].ipAddress);
      await racksPage.validateRackSlotsHighlighted([4]);
    });

    await test.step("Assign the second miner to slot 06", async () => {
      await racksPage.selectRackMiner(selectedMiners[1].ipAddress);
      await racksPage.clickRackSlot(6);

      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[0].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[0].ipAddress, 4);
      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[1].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[1].ipAddress, 6);
      await racksPage.validateRackSlotsHighlighted([4, 6]);
    });

    await test.step("Clear assignments and validate empty state", async () => {
      await racksPage.clickClearAssignments();

      await racksPage.validateMinerRowUnassigned(selectedMiners[0].ipAddress);
      await racksPage.validateMinerRowUnassigned(selectedMiners[1].ipAddress);
      await racksPage.validateRackSlotsNotHighlighted([4, 6]);
    });

    await test.step("Assign miners to slots 01 and 09 and save", async () => {
      await racksPage.selectRackMiner(selectedMiners[0].ipAddress);
      await racksPage.clickRackSlot(1);
      await racksPage.selectRackMiner(selectedMiners[1].ipAddress);
      await racksPage.clickRackSlot(9);

      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[0].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[0].ipAddress, 1);
      await racksPage.validateMinerRowHasGreenCheck(selectedMiners[1].ipAddress);
      await racksPage.validateMinerRowPosition(selectedMiners[1].ipAddress, 9);
      await racksPage.validateRackSlotsHighlighted([1, 9]);

      await racksPage.clickSaveRack();
      await racksPage.validateRackToast(rackLabel);
    });

    await test.step("Open the created rack and validate saved slots", async () => {
      await racksPage.clickViewGrid();
      await racksPage.openRackCard(rackLabel, AUTOMATION_ZONE);
      await racksPage.validateRackOverviewAssignedSlots([1, 9]);
      await racksPage.validateRackOverviewEmptySlots([2, 3, 4, 5, 6, 7, 8]);
    });
  });

  test("Rack overview search assignment updates slots and miners filter state", async ({ racksPage, minersPage }) => {
    let rackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];
    let selectableMinerIndexes: number[] = [];
    let expectedVisibleMinerCount = 0;

    await test.step("Create and save a new 8x2 rack", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = RACK_LABEL;
      await racksPage.inputRackLabel(rackLabel);

      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(OVERVIEW_RACK_COLUMNS);
      await racksPage.inputRows(OVERVIEW_RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast(rackLabel);
    });

    await test.step("Open the created rack and assign the first miner to slot 02", async () => {
      await racksPage.clickViewGrid();
      await racksPage.openRackCard(rackLabel, AUTOMATION_ZONE);
      await racksPage.clickRackOverviewEmptySlot(2);
      await racksPage.waitForMinerSelectorListToLoad();
      expectedVisibleMinerCount = (await racksPage.getAllVisibleMinersFromSelector()).length;

      selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(2);
      selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
      test.expect(selectedMiners).toHaveLength(2);

      await racksPage.assignSearchMinerByIpAddress(selectedMiners[0].ipAddress);
      await racksPage.validateRackOverviewAssignedSlots([2]);
    });

    await test.step("Reassign the same first miner from slot 02 to slot 15", async () => {
      await racksPage.clickRackOverviewEmptySlot(15);
      await racksPage.assignSearchMinerByIpAddress(selectedMiners[0].ipAddress);

      await racksPage.validateRackOverviewAssignedSlots([15]);
      await racksPage.validateRackOverviewEmptySlots([2]);
    });

    await test.step("Assign the second miner to slot 02", async () => {
      await racksPage.clickRackOverviewEmptySlot(2);
      await racksPage.assignSearchMinerByIpAddress(selectedMiners[1].ipAddress);

      await racksPage.validateRackOverviewAssignedSlots([2, 15]);
    });

    await test.step("Validate miners page is filtered to the rack and contains only the assigned miners", async () => {
      await racksPage.clickViewMiners();
      await minersPage.validateActiveFilter(rackLabel);
      await minersPage.validateAmountOfMiners(2);
      await minersPage.validateMinerInList(selectedMiners[0].ipAddress);
      await minersPage.validateMinerInList(selectedMiners[1].ipAddress);
    });

    await test.step("Remove all rack miners from edit rack manage miners flow", async () => {
      await racksPage.navigateToRacksPage();
      await racksPage.clickViewGrid();
      await racksPage.openRackCard(rackLabel, AUTOMATION_ZONE);
      await racksPage.clickEditRack();
      await racksPage.clickManageMiners();
      await racksPage.waitForMinerSelectorListToLoad();
      await racksPage.toggleMinerInSelectorByIpAddress(selectedMiners[0].ipAddress);
      await racksPage.toggleMinerInSelectorByIpAddress(selectedMiners[1].ipAddress);
      await racksPage.clickContinueInMinerSelector();
      await racksPage.validateTextIsVisible("No miners added to this rack yet.");
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast(rackLabel, "updated");
    });

    await test.step("Validate rack overview is empty after saving", async () => {
      await racksPage.validateRackOverviewEmptySlots([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]);
    });

    await test.step("Validate miners view empty state and clear the rack filter", async () => {
      await racksPage.clickViewMiners();
      await minersPage.validateNoResultsEmptyState();
      await minersPage.clickClearAllFilters();
      await minersPage.validateActiveFilterNotVisible(rackLabel);
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinersAdded(expectedVisibleMinerCount);
    });
  });
});
