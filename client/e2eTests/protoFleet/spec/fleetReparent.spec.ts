import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

const RACK_ZONE = "Automation Zone";

test.describe("Fleet reparent flows", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test("Move a rack to a building from the Racks tab", async ({
    fleetLocationsPage,
    page,
    parentPickerModal,
    racksPage,
  }) => {
    const siteName = generateRandomText("reparent_rack_site");
    const sourceBuildingName = generateRandomText("reparent_source_building");
    const targetBuildingName = generateRandomText("reparent_target_building");
    const rackName = generateRandomText("reparent_rack");
    let sourceBuildingId = "";
    let targetBuildingId = "";

    await test.step("Create a site, two buildings, and an empty rack", async () => {
      await fleetLocationsPage.navigateToSitesPage();
      await fleetLocationsPage.createSite(siteName);
      await fleetLocationsPage.openSiteSettings(siteName);
      sourceBuildingId = await fleetLocationsPage.createBuildingInSelectedSite(sourceBuildingName);
      targetBuildingId = await fleetLocationsPage.createBuildingInSelectedSite(targetBuildingName);

      await racksPage.navigateToRacksPage();
      await racksPage.createEmptyRack(rackName, RACK_ZONE);
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.validateRackRow(rackName, RACK_ZONE, 0);
    });

    await test.step("Assign the rack to the source building", async () => {
      await racksPage.clickRackRowAction(rackName, "Add to building");
      await parentPickerModal.validateTitleMatches(/Add .* to a building/);
      await parentPickerModal.selectOption(sourceBuildingName);
      await parentPickerModal.clickSave();
      await racksPage.validateTextInToast(`Moved "${rackName}" to selected building.`);

      await page.goto(`/racks?building=${sourceBuildingId}`);
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.validateRackRow(rackName, RACK_ZONE, 0);
    });

    await test.step("Move the rack from the source building to the target building", async () => {
      await racksPage.clickRackRowAction(rackName, "Add to building");
      await parentPickerModal.validateTitleMatches(/Add .* to a building/);
      await parentPickerModal.selectOption(targetBuildingName);
      await parentPickerModal.clickSave();
    });

    await test.step("Validate the rack appears under the target building and not under the source building", async () => {
      await racksPage.validateTextInToast(`Moved "${rackName}" to selected building.`);

      await page.goto(`/racks?building=${targetBuildingId}`);
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      test.expect(await racksPage.listRackNames()).toContain(rackName);

      await page.goto(`/racks?building=${sourceBuildingId}`);
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad();
      test.expect(await racksPage.listRackNames()).not.toContain(rackName);
    });
  });

  test("Assign a single miner to a site from the Miners tab", async ({
    fleetLocationsPage,
    minersPage,
    parentPickerModal,
  }) => {
    const siteName = generateRandomText("reparent_miner_site");
    let minerName = "";

    await test.step("Create a target site and capture a miner to move", async () => {
      await fleetLocationsPage.navigateToSitesPage();
      await fleetLocationsPage.createSite(siteName);

      await minersPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    const minerIp = await minersPage.getMinerIpAddressByIndex(0);
    minerName = (await minersPage.getMinerNameByIndex(0)).trim();

    await test.step("Move one miner into the site", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickMinerActionMenuItem("Add to site");
      await parentPickerModal.validateTitleMatches(/Add .* to a site/);
      await parentPickerModal.selectOption(siteName);
      await parentPickerModal.clickSave();
      await parentPickerModal.continueSiteMoveIfVisible();
    });

    await test.step("Validate the operator sees a successful move confirmation", async () => {
      await minersPage.validateTextInToast(`Moved "${minerName}" to selected site.`);
    });
  });

  test("Assign a single miner to a rack from the Miners tab", async ({ minersPage, parentPickerModal, racksPage }) => {
    const rackName = generateRandomText("reparent_target_rack");

    await test.step("Create an empty rack and capture a miner to move", async () => {
      await racksPage.navigateToRacksPage();
      await racksPage.createEmptyRack(rackName, RACK_ZONE);
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.validateRackRow(rackName, RACK_ZONE, 0);

      await minersPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    const minerIp = await minersPage.getMinerIpAddressByIndex(1);

    await test.step("Move one miner into the rack", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickMinerActionMenuItem("Add to rack");
      await parentPickerModal.validateTitleMatches(/Add .* to a rack/);
      await parentPickerModal.selectOption(rackName);
      await parentPickerModal.clickSave();
    });

    await test.step("Validate the rack miner count increases for the selected rack", async () => {
      await racksPage.navigateToRacksPage();
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.validateRackRow(rackName, RACK_ZONE, 1);
    });
  });
});
