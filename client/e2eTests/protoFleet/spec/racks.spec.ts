import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { PROTO_RIG_MODEL } from "../helpers/minerModels";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { type RackSelectorMiner, RacksPage } from "../pages/racks";

const AUTOMATION_ZONE = "AutomationZone";
const RACK_COLUMNS = 2;
const RACK_ROWS = 2;
const VALIDATION_RACK_COLUMNS = 1;
const VALIDATION_RACK_ROWS = 1;
const NETWORK_RACK_COLUMNS = 9;
const NETWORK_RACK_ROWS = 9;
const LARGE_RACK_COLUMNS = 3;
const LARGE_RACK_ROWS = 3;
const OVERVIEW_RACK_COLUMNS = 8;
const OVERVIEW_RACK_ROWS = 2;
const ORDER_INDEX_SCENARIOS = [
  { label: "Bottom left", expectedNumbers: [3, 4, 1, 2] },
  { label: "Top left", expectedNumbers: [1, 2, 3, 4] },
  { label: "Bottom right", expectedNumbers: [4, 3, 2, 1] },
  { label: "Top right", expectedNumbers: [2, 1, 4, 3] },
] as const;

test.describe("Racks", () => {
  test.beforeEach(async ({ page, commonSteps, racksPage }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
    await racksPage.navigateToRacksPage();
  });

  test.afterEach("CLEANUP: Delete all racks", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({
      baseURL: testConfig.baseUrl,
      viewport: testInfo.project.use?.viewport,
    });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const racksPage = new RacksPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await racksPage.navigateToRacksPage();
      await cleanupAllRacks(racksPage);
    } finally {
      await context.close();
    }
  });

  async function cleanupAllRacks(racksPage: RacksPage) {
    await racksPage.navigateToRacksPage();
    await racksPage.tryAction(() => racksPage.clickViewList());
    await racksPage.waitForRackListToLoad();

    let rackNames = await racksPage.listRackNames();

    while (rackNames.length > 0) {
      await racksPage.openRackFromList(rackNames[0]);
      await racksPage.clickEditRack();
      await racksPage.clickDeleteRack();
      await racksPage.clickDeleteConfirm();
      await racksPage.tryAction(() => racksPage.validateRackDeletedToast());

      await racksPage.navigateToRacksPage();
      await racksPage.tryAction(() => racksPage.clickViewList());
      await racksPage.waitForRackListToLoad();
      rackNames = await racksPage.listRackNames();
    }
  }

  function createZoneName(prefix: "A" | "B") {
    const suffix = Math.random()
      .toString(36)
      .replace(/[^a-z]+/g, "")
      .slice(0, 6);
    return `${prefix}-${suffix || "zone"}`;
  }

  async function addSelectableMinersToSlots(
    racksPage: RacksPage,
    minerCount: number,
    slotNumbers: readonly number[],
  ): Promise<RackSelectorMiner[]> {
    test.expect(slotNumbers).toHaveLength(minerCount);

    await racksPage.clickAddMiners();
    await racksPage.waitForMinerSelectorListToLoad();

    const selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(minerCount);
    const selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
    await racksPage.selectMinersInSelectorByIndex(selectableMinerIndexes);
    await racksPage.clickContinueInMinerSelector();

    for (let i = 0; i < selectedMiners.length; i++) {
      await racksPage.selectRackMiner(selectedMiners[i].ipAddress);
      await racksPage.clickRackSlot(slotNumbers[i]);
    }

    return selectedMiners;
  }

  async function addSelectableRigMinersToSlots(
    racksPage: RacksPage,
    minerCount: number,
    slotNumbers: readonly number[],
  ): Promise<RackSelectorMiner[]> {
    test.expect(slotNumbers).toHaveLength(minerCount);

    await racksPage.clickAddMiners();
    await racksPage.waitForMinerSelectorListToLoad();
    await racksPage.filterModalType(PROTO_RIG_MODEL);
    await racksPage.waitForMinerSelectorListToLoad();

    const selectableMinerIndexes = await racksPage.getSelectableMinerIndexes(minerCount);
    const selectedMiners = await racksPage.getMinersFromSelector(selectableMinerIndexes);
    await racksPage.selectMinersInSelectorByIndex(selectableMinerIndexes);
    await racksPage.clickContinueInMinerSelector();

    for (let i = 0; i < selectedMiners.length; i++) {
      await racksPage.selectRackMiner(selectedMiners[i].ipAddress);
      await racksPage.clickRackSlot(slotNumbers[i]);
    }

    return selectedMiners;
  }

  async function expectGridRackLabels(racksPage: RacksPage, expectedLabels: string[]) {
    await test.expect.poll(async () => await racksPage.getGridRackLabels()).toEqual(expectedLabels);
  }

  async function expectListRackLabels(racksPage: RacksPage, expectedLabels: string[]) {
    await test.expect.poll(async () => await racksPage.listRackNames()).toEqual(expectedLabels);
  }

  test("Create rack with miners assigned by name", async ({ racksPage }) => {
    let rackLabel = "";
    let orderIndexValue = "";
    let selectedMiners: RackSelectorMiner[] = [];

    await test.step("Create a new 2x2 rack", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = await racksPage.getGeneratedRackLabel();
      test.expect(rackLabel).toBeTruthy();

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

  test("Manual rack assignment supports search, selection replacement, and saved slot state", async ({ racksPage }) => {
    let rackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];
    let selectableMinerIndexes: number[] = [];

    await test.step("Create a new 3x3 rack", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = await racksPage.getGeneratedRackLabel();
      test.expect(rackLabel).toBeTruthy();

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

      rackLabel = await racksPage.getGeneratedRackLabel();
      test.expect(rackLabel).toBeTruthy();

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

  test("Rack overview actions menu manages power for assigned rig miners", async ({ racksPage, minersPage, page }) => {
    let rackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];

    await test.step("Create and save a new rack with two rig miners", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = await racksPage.getGeneratedRackLabel();
      test.expect(rackLabel).toBeTruthy();

      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(RACK_COLUMNS);
      await racksPage.inputRows(RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();

      selectedMiners = await addSelectableRigMinersToSlots(racksPage, 2, [1, 2]);
      test.expect(selectedMiners).toHaveLength(2);

      await racksPage.clickSaveRack();
      await racksPage.validateRackToast(rackLabel);
    });

    await test.step("Open the rack overview and validate assigned slots", async () => {
      await racksPage.clickViewGrid();
      await racksPage.openRackCard(rackLabel, AUTOMATION_ZONE);
      await racksPage.validateRackOverviewAssignedSlots([1, 2]);
    });

    const requestPromise = page.waitForRequest(/SetPowerTarget/);
    const responsePromise = page.waitForResponse(/SetPowerTarget/);

    await test.step("Use the overview actions menu to reduce power", async () => {
      await racksPage.openRackOverviewActionsMenu();
      await racksPage.clickRackOverviewManagePower();
      await minersPage.clickReducePowerOption();
      await minersPage.clickManagePowerConfirm();
    });

    await test.step("Validate manage power toasts", async () => {
      await minersPage.validateTextInToastGroup("Updating power settings");
      await minersPage.validateTextInToastGroup("Updated power settings");
    });

    await test.step("Validate the SetPowerTarget request targets the rack miners", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();

      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_EFFICIENCY");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(2);
      test.expect(response.status()).toBe(200);
    });
  });

  test("Multiple racks support zone filtering and miner sorting", async ({ racksPage }) => {
    const zoneA = createZoneName("A");
    const zoneB = createZoneName("B");
    const createdRackLabels: string[] = [];

    await test.step("Create rack A-01 with three miners", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(zoneA);
      test.expect(await racksPage.getGeneratedRackLabel()).toBe("A-01");
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
      test.expect(await racksPage.getGeneratedRackLabel()).toBe("A-02");
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
      test.expect(await racksPage.getGeneratedRackLabel()).toBe("B-01");
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

  test("Assign by network orders all miners by IP address on a 9x9 rack", async ({ racksPage }) => {
    let allVisibleMiners: RackSelectorMiner[] = [];

    await test.step("Create a new 9x9 rack and add all visible miners", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);
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

  test("Rack settings validation blocks invalid input and miner overflow until corrected", async ({ racksPage }) => {
    const validationZone = createZoneName("A");
    let generatedRackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];

    await test.step("Validate required zone before continuing", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.clickContinueFromRackSettings();
      await racksPage.validateRackSettingsFieldError("rack-zone", "A zone is required");
      await racksPage.validateTitleInModal("Rack settings");
    });

    await test.step("Validate required label and invalid dimensions", async () => {
      await racksPage.inputZone(validationZone);
      generatedRackLabel = await racksPage.getGeneratedRackLabel();
      test.expect(generatedRackLabel).toBe("A-01");

      await racksPage.inputRackLabel("");
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
