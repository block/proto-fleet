import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { PROTO_RIG_MODEL } from "../helpers/minerModels";
import { addSelectableRigMinersToSlots } from "../helpers/racksHelpers";
import {
  AUTOMATION_ZONE,
  cleanupPoolIfPageOpen,
  OVERVIEW_RACK_COLUMNS,
  OVERVIEW_RACK_ROWS,
  RACK_COLUMNS,
  RACK_LABEL,
  RACK_ROWS,
  useRacksHooks,
  VALID_POOL_URL,
} from "../helpers/racksTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";
import { type RackSelectorMiner } from "../pages/racks";

test.describe("Racks - overview actions", () => {
  useRacksHooks();

  test("Rack overview actions menu manages power for assigned rig miners", async ({ racksPage, minersPage, page }) => {
    let rackLabel = "";
    let selectedMiners: RackSelectorMiner[] = [];
    let rackDeviceIdentifiers: string[] = [];

    await test.step("Create and save a new rack with two rig miners", async () => {
      const saveRackRequestPromise = page.waitForRequest(/SaveRack/);

      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);

      rackLabel = RACK_LABEL;
      await racksPage.inputRackLabel(rackLabel);

      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(RACK_COLUMNS);
      await racksPage.inputRows(RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();

      selectedMiners = await addSelectableRigMinersToSlots(racksPage, 2, [1, 2]);
      test.expect(selectedMiners).toHaveLength(2);
      test.expect(selectedMiners.every((miner) => miner.model === PROTO_RIG_MODEL)).toBe(true);

      await racksPage.clickSaveRack();

      const saveRackRequest = await saveRackRequestPromise;
      const saveRackRequestBody = saveRackRequest.postDataJSON();
      rackDeviceIdentifiers = saveRackRequestBody.deviceSelector.deviceList.deviceIdentifiers;

      await racksPage.validateRackToast(rackLabel);
      test.expect(rackDeviceIdentifiers).toHaveLength(2);
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
      const targetedDeviceIdentifiers = requestBody.deviceSelector.includeDevices.deviceIdentifiers;
      const sortedTargetedDeviceIdentifiers = [...targetedDeviceIdentifiers].sort();
      const sortedRackDeviceIdentifiers = [...rackDeviceIdentifiers].sort();

      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_EFFICIENCY");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(sortedTargetedDeviceIdentifiers).toEqual(sortedRackDeviceIdentifiers);
      test.expect(response.status()).toBe(200);
    });
  });

  if (testConfig.target !== "real") {
    test("Rack overview actions menu assigns pools to assigned rig miners", async ({
      racksPage,
      editPoolPage,
      newPoolModal,
      loginModal,
      settingsPage,
      settingsPoolsPage,
      page,
    }) => {
      const poolName = generateRandomText("PoolName");
      const poolUsername = generateRandomText("PoolUsername");
      let rackLabel = "";
      let rackDeviceIdentifiers: string[] = [];

      try {
        await test.step("Create a rack with two assigned Proto rigs", async () => {
          const saveRackRequestPromise = page.waitForRequest(/SaveRack/);

          await racksPage.clickAddRackButton();
          await racksPage.inputZone(AUTOMATION_ZONE);
          rackLabel = RACK_LABEL;
          await racksPage.inputRackLabel(rackLabel);
          await racksPage.enableCustomRackLayout();
          await racksPage.inputColumns(OVERVIEW_RACK_COLUMNS);
          await racksPage.inputRows(OVERVIEW_RACK_ROWS);
          await racksPage.clickContinueFromRackSettings();
          await addSelectableRigMinersToSlots(racksPage, 2, [1, 2]);
          await racksPage.clickSaveRack();

          const saveRackRequest = await saveRackRequestPromise;
          const saveRackRequestBody = saveRackRequest.postDataJSON();
          rackDeviceIdentifiers = saveRackRequestBody.deviceSelector.deviceList.deviceIdentifiers;

          await racksPage.validateRackToast(rackLabel);
          test.expect(rackDeviceIdentifiers).toHaveLength(2);
        });

        await test.step("Open the rack overview and start the assign pools flow", async () => {
          await racksPage.clickViewGrid();
          await racksPage.openRackCard(rackLabel, AUTOMATION_ZONE);
          await racksPage.openRackOverviewActionsMenu();
          await racksPage.clickRackOverviewAssignPools();
          await loginModal.loginAsAdmin();
        });

        await test.step("Create a pool from the overview flow", async () => {
          await editPoolPage.clickPoolAddButton();
          await editPoolPage.clickAddNewPool();
          await newPoolModal.inputPoolName(poolName);
          await newPoolModal.inputPoolUrl(VALID_POOL_URL);
          await newPoolModal.inputPoolUsername(poolUsername);
          await newPoolModal.clickSaveNewPool();
          await editPoolPage.validatePoolVisible(poolName, VALID_POOL_URL);
        });

        await test.step("Assign the created pool to the rack miners", async () => {
          const requestPromise = page.waitForRequest(/UpdateMiningPools/);
          const responsePromise = page.waitForResponse(/UpdateMiningPools/);

          await editPoolPage.clickAssignToXMiners(2);

          const request = await requestPromise;
          const response = await responsePromise;
          const requestBody = request.postDataJSON();
          const targetedDeviceIdentifiers = requestBody.deviceSelector.includeDevices.deviceIdentifiers;
          const sortedTargetedDeviceIdentifiers = [...targetedDeviceIdentifiers].sort();
          const sortedRackDeviceIdentifiers = [...rackDeviceIdentifiers].sort();

          test.expect(request.method()).toBe("POST");
          test.expect(requestBody).toHaveProperty("defaultPool");
          test.expect(requestBody).toHaveProperty("deviceSelector");
          test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
          test.expect(sortedTargetedDeviceIdentifiers).toEqual(sortedRackDeviceIdentifiers);
          test.expect(response.status()).toBe(200);
          await racksPage.validateTextInToastGroup("Assigned pools");
        });
      } finally {
        await cleanupPoolIfPageOpen(page, settingsPage, settingsPoolsPage, poolName);
      }
    });
  }

  test("Rack overview actions menu opens manage security and validates password mismatch", async ({
    racksPage,
    loginModal,
    minersPage,
    page,
  }) => {
    let rackLabel = "";

    await test.step("Create a rack with two assigned Proto rigs and open the overview security flow", async () => {
      await racksPage.clickAddRackButton();
      await racksPage.inputZone(AUTOMATION_ZONE);
      rackLabel = RACK_LABEL;
      await racksPage.inputRackLabel(rackLabel);
      await racksPage.enableCustomRackLayout();
      await racksPage.inputColumns(OVERVIEW_RACK_COLUMNS);
      await racksPage.inputRows(OVERVIEW_RACK_ROWS);
      await racksPage.clickContinueFromRackSettings();
      await addSelectableRigMinersToSlots(racksPage, 2, [1, 2]);
      await racksPage.clickSaveRack();

      await racksPage.validateRackToast(rackLabel);
      await racksPage.clickViewGrid();
      await racksPage.openRackCard(rackLabel, AUTOMATION_ZONE);
      await racksPage.openRackOverviewActionsMenu();
      await racksPage.clickRackOverviewManageSecurity();
      await loginModal.loginAsAdminForSecurity();
      await minersPage.validateManageSecurityModalOpened();
    });

    await test.step("Open the password form and validate the mismatch state", async () => {
      await minersPage.clickManageSecurityUpdateButton();
      await minersPage.validateTitleInModal("Update the admin login for your miners");
      await minersPage.inputCurrentMinerPassword("root");
      await minersPage.inputNewMinerPassword("ProtoRigPass123!");
      await minersPage.inputConfirmMinerPassword("ProtoRigPass1234!");
      await minersPage.clickIn("Continue", "modal");
      await minersPage.validateTextInModal("Passwords don't match");

      await page.getByTestId("modal").getByTestId("header-icon-button").click();
      await minersPage.closeManageSecurityModal();
      await racksPage.validateTitle(rackLabel);
    });
  });
});
