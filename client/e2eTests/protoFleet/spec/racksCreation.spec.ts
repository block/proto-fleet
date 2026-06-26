import { test } from "../fixtures/pageFixtures";
import {
  AUTOMATION_ZONE,
  NETWORK_RACK_COLUMNS,
  NETWORK_RACK_ROWS,
  ORDER_INDEX_SCENARIOS,
  RACK_COLUMNS,
  RACK_LABEL,
  RACK_ROWS,
  useRacksHooks,
} from "../helpers/racksTestSetup";
import { type RackSelectorMiner } from "../pages/racks";

test.describe("Racks - creation", () => {
  useRacksHooks();

  test("Create rack with miners assigned by name", async ({ racksPage }) => {
    let rackLabel = "";
    let orderIndexValue = "";
    let selectedMiners: RackSelectorMiner[] = [];

    await test.step("Create a new 2x2 rack", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = RACK_LABEL;
      await racksPage.inputRackLabel(rackLabel);

      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(RACK_COLUMNS);
      await racksPage.inputRows(RACK_ROWS);

      orderIndexValue = await racksPage.getOrderIndexValue();
      await racksPage.clickContinueFromRackSettings();
    });

    await test.step("Validate empty rack assignment state", async () => {
      await racksPage.validateRackConfiguration(RACK_COLUMNS, RACK_ROWS, orderIndexValue);
      await racksPage.validateAssignedMinersCount(0, 4);
    });

    await test.step("Add the first two miners", async () => {
      await racksPage.clickAddMiners();
      await racksPage.waitForMinerSelectorListToLoad();

      selectedMiners = await racksPage.getMinersFromSelector([0, 1]);
      test.expect(selectedMiners).toHaveLength(2);
      await racksPage.selectMinersInSelectorByIndex([0, 1]);
      await racksPage.clickContinueInMinerSelector();
    });

    await test.step("Assign miners by name and validate positions", async () => {
      await racksPage.clickAssignByName();
      await racksPage.validateMinersAssignedByName(selectedMiners);
    });

    await test.step("Save rack and validate rack grid card", async () => {
      await racksPage.clickSaveRack();
      await racksPage.validateRackToast(rackLabel);
      await racksPage.clickViewGrid();
      await racksPage.validateRackCardVisible(rackLabel, AUTOMATION_ZONE);
      await racksPage.validateRackCardGrid(rackLabel, AUTOMATION_ZONE, RACK_COLUMNS, RACK_ROWS);
    });

    await test.step("Validate rack in list view", async () => {
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.validateRackRow(rackLabel, AUTOMATION_ZONE, 2);
    });
  });

  test("Rack numbering updates when order index changes", async ({ racksPage }) => {
    let selectedMiners: RackSelectorMiner[] = [];

    await test.step("Create a new 2x2 rack", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);
      await racksPage.inputRackLabel(RACK_LABEL);
      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(RACK_COLUMNS);
      await racksPage.inputRows(RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();
    });

    await test.step("Add four miners", async () => {
      await racksPage.clickAddMiners();
      await racksPage.waitForMinerSelectorListToLoad();

      selectedMiners = await racksPage.getMinersFromSelector([0, 1, 2, 3]);
      test.expect(selectedMiners).toHaveLength(4);
      await racksPage.selectMinersInSelectorByIndex([0, 1, 2, 3]);
      await racksPage.clickContinueInMinerSelector();
    });

    await test.step("Assign miners manually in DOM order and validate default numbering", async () => {
      await racksPage.clickAssignManually();
      await racksPage.assignMinersToSlotsInDomOrder(selectedMiners);

      await racksPage.validateRackSlotNumbersInDomOrder(ORDER_INDEX_SCENARIOS[0].expectedNumbers);
      await racksPage.validateMinerPositions(selectedMiners, ORDER_INDEX_SCENARIOS[0].expectedNumbers);
    });

    for (const scenario of ORDER_INDEX_SCENARIOS.slice(1)) {
      await test.step(`Change order index to ${scenario.label}`, async () => {
        await racksPage.clickEditRackSettings();
        await racksPage.changeOrderIndexAndContinue(scenario.label);
        await racksPage.validateRackConfiguration(RACK_COLUMNS, RACK_ROWS, scenario.label);
        await racksPage.validateRackSlotNumbersInDomOrder(scenario.expectedNumbers);
        await racksPage.validateMinerPositions(selectedMiners, scenario.expectedNumbers);
      });
    }
  });

  test("Assign by network orders all miners by IP address on a 9x9 rack", async ({ racksPage }) => {
    let allVisibleMiners: RackSelectorMiner[] = [];

    await test.step("Create a new 9x9 rack and add all visible miners", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);
      await racksPage.inputRackLabel(RACK_LABEL);
      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(NETWORK_RACK_COLUMNS);
      await racksPage.inputRows(NETWORK_RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();

      await racksPage.clickAddMiners();
      await racksPage.waitForMinerSelectorListToLoad();
      allVisibleMiners = await racksPage.getAllVisibleMinersFromSelector();
      test.expect(allVisibleMiners.length).toBeGreaterThan(0);
      test.expect(allVisibleMiners.length).toBeLessThanOrEqual(NETWORK_RACK_COLUMNS * NETWORK_RACK_ROWS);
      await racksPage.clickSelectAllMinersInSelector();
      await racksPage.clickContinueInMinerSelector();
    });

    await test.step("Assign all miners by network and validate positions by IP and name", async () => {
      await racksPage.clickAssignByNetwork();
      await racksPage.validateMinersAssignedByNetwork(allVisibleMiners);
    });
  });
});
