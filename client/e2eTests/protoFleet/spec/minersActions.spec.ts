/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AddMinersPage } from "../pages/addMiners";
import { AuthPage } from "../pages/auth";
import { HomePage } from "../pages/home";
import { MinersPage } from "../pages/miners";

test.describe("Miners", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll("CLEANUP: Add miners, re-authenticate, and wake up", async ({ browser }, testInfo) => {
    if (testConfig.target === "real") {
      return;
    }

    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    try {
      const page = await context.newPage();
      await page.goto("/");

      const homePage = new HomePage(page, isMobile);
      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const addMinersPage = new AddMinersPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();

      // Step 1: Add miners from network if any are available
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.waitForFoundMinersList();
      const foundMinerCount = await addMinersPage.getFoundMinersCount();

      if (foundMinerCount === 0) {
        await addMinersPage.clickHeaderIconButton();
      } else {
        await addMinersPage.clickContinueWithSelectedMiners();
      }
      await minersPage.waitForMinersListToLoad();

      // Step 2: Re-authenticate miners if needed (existing logic)
      if (await page.getByRole("button", { name: "Authenticate", exact: true }).isVisible({ timeout: 3000 })) {
        await homePage.clickAuthenticateMinersButton();
        await homePage.validateAuthenticateMinersModalTitle();
        await homePage.clickShowMinersButton();
        const miners = await homePage.getListOfMinersToAuthenticate();

        if (miners.some((miner) => miner.includes("S17 XP"))) {
          await homePage.inputMinerAuthUsername("root17");
          await homePage.inputMinerAuthPassword("root17");
          await homePage.clickAuthenticateMinersConfirmButton();
        }
        if (miners.some((miner) => miner.includes("S19 XP"))) {
          await homePage.inputMinerAuthUsername("root19");
          await homePage.inputMinerAuthPassword("root19");
          await homePage.clickAuthenticateMinersConfirmButton();
        }
        if (miners.some((miner) => miner.includes("S21 XP"))) {
          await homePage.inputMinerAuthUsername("root21");
          await homePage.inputMinerAuthPassword("root21");
          await homePage.clickAuthenticateMinersConfirmButton();
        }
        await homePage.validateModalClosed();
      }

      // Step 3: Wake up all miners
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      const minerCount = await minersPage.getMinersCount();

      if (minerCount > 0) {
        await minersPage.clickSelectAllCheckbox();
        await minersPage.clickActionsMenuButton();
        await minersPage.clickWakeUpButton();
        await minersPage.clickWakeUpConfirm();
        await minersPage.validateTextInToastGroup("Waking up");
      }
    } finally {
      await context.close();
    }
  });

  test("SLEEP - WAKE a miner @smoke", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support SLEEP action
      await minersPage.filterRigMiners();
    });

    let minerIp: string;

    await test.step("Select first miner and shut it down", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Putting to sleep");
      await minersPage.validateTextInToastGroup("Put to sleep");
    });

    await test.step("Validate miner is sleeping", async () => {
      await minersPage.validateMinerStatus(minerIp, "Sleeping");
    });

    await test.step("Select all miners and wake them up", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickWakeUpButton();
      await minersPage.clickWakeUpConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Waking up");
      await minersPage.validateTextInToastGroup("Woke up");
    });

    await test.step("Validate none of the miners are sleeping", async () => {
      await minersPage.validateMinerStatus(minerIp, "Hashing");
      await minersPage.validateNoMinerWithStatus("Sleeping");
      await minersPage.validateNoMinerWithStatus("Waking");
    });
  });

  test("REBOOT a single miner", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const requestPromise = page.waitForRequest(/Reboot/);
    const responsePromise = page.waitForResponse(/Reboot/);

    await test.step("Select first miner and reboot it", async () => {
      let minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickRebootButton();
      await minersPage.clickRebootConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Rebooting");
      await minersPage.validateTextInToastGroup("Rebooted");
    });

    await test.step("Validate reboot API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });
  });

  test("REBOOT multiple miners", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const requestPromise = page.waitForRequest(/Reboot/);
    const responsePromise = page.waitForResponse(/Reboot/);

    await test.step("Select multiple miners and reboot them", async () => {
      let minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      let minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      let minerIp3 = await minersPage.getMinerIpAddressByIndex(2);

      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(minerIp3);
      await minersPage.validateActionBarMinerCount(3);

      await minersPage.clickActionsMenuButton();
      await minersPage.clickRebootButton();
      await minersPage.clickRebootConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Rebooting");
      await minersPage.validateTextInToastGroup("Rebooted");
    });

    await test.step("Validate reboot API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      test.expect(request.method()).toBe("POST");
      const requestBody = request.postDataJSON();
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(3);
      test.expect(response.status()).toBe(200);
    });
  });

  test("MANAGE POWER for a single miner", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support MANAGE POWER action
      await minersPage.filterRigMiners();
    });

    const requestPromise1 = page.waitForRequest(/SetPowerTarget/);
    const responsePromise1 = page.waitForResponse(/SetPowerTarget/);

    await test.step("Select first miner and set MAX power", async () => {
      let minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickManagePowerButton();
      await minersPage.clickMaxPowerOption();
      await minersPage.clickManagePowerConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Updating power settings");
      await minersPage.validateTextInToastGroup("Updated power settings");
      await minersPage.dismissToast();
    });

    await test.step("Validate 'SetPowerTarget' API request", async () => {
      const request = await requestPromise1;
      const response = await responsePromise1;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_MAXIMUM_HASHRATE");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });

    const requestPromise2 = page.waitForRequest(/SetPowerTarget/);
    const responsePromise2 = page.waitForResponse(/SetPowerTarget/);

    await test.step("Select first miner and set REDUCE power", async () => {
      let minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickManagePowerButton();
      await minersPage.clickReducePowerOption();
      await minersPage.clickManagePowerConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Updating power settings");
      await minersPage.validateTextInToastGroup("Updated power settings");
    });

    await test.step("Validate 'SetPowerTarget' API request", async () => {
      const request = await requestPromise2;
      const response = await responsePromise2;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_EFFICIENCY");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });
  });

  test("MANAGE POWER for multiple miners", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support MANAGE POWER action
      await minersPage.filterRigMiners();
    });

    const requestPromise1 = page.waitForRequest(/SetPowerTarget/);
    const responsePromise1 = page.waitForResponse(/SetPowerTarget/);

    await test.step("Select multiple miners and set MAX power", async () => {
      let minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      let minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      let minerIp3 = await minersPage.getMinerIpAddressByIndex(2);

      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(minerIp3);
      await minersPage.validateActionBarMinerCount(3);

      await minersPage.clickActionsMenuButton();
      await minersPage.clickManagePowerButton();
      await minersPage.clickMaxPowerOption();
      await minersPage.clickManagePowerConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Updating power settings");
      await minersPage.validateTextInToastGroup("Updated power settings");
      await minersPage.dismissToast();
    });

    await test.step("Validate 'SetPowerTarget' API request", async () => {
      const request = await requestPromise1;
      const response = await responsePromise1;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_MAXIMUM_HASHRATE");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(3);
      test.expect(response.status()).toBe(200);
    });

    const requestPromise2 = page.waitForRequest(/SetPowerTarget/);
    const responsePromise2 = page.waitForResponse(/SetPowerTarget/);

    await test.step("Select multiple miners and set REDUCE power", async () => {
      await minersPage.clickActionsMenuButton();
      await minersPage.clickManagePowerButton();
      await minersPage.clickReducePowerOption();
      await minersPage.clickManagePowerConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Updating power settings");
      await minersPage.validateTextInToastGroup("Updated power settings");
    });

    await test.step("Validate 'SetPowerTarget' API request", async () => {
      const request = await requestPromise2;
      const response = await responsePromise2;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("performanceMode");
      test.expect(requestBody.performanceMode).toBe("PERFORMANCE_MODE_EFFICIENCY");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(3);
      test.expect(response.status()).toBe(200);
    });
  });

  test("Set COOLING MODE to Air Cooled for a single miner", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support COOLING_MODE action
      await minersPage.filterRigMiners();
    });

    const requestPromise = page.waitForRequest(/SetCoolingMode/);
    const responsePromise = page.waitForResponse(/SetCoolingMode/);

    await test.step("Select first miner and set Air Cooled mode", async () => {
      const minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickCoolingModeButton();
      await minersPage.validateAirCooledOptionSelected();
      await minersPage.clickUpdateCoolingModeConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Setting cooling mode");
      await minersPage.validateTextInToastGroup("Updated cooling mode");
    });

    await test.step("Validate 'SetCoolingMode' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("mode");
      test.expect(requestBody.mode).toBe("COOLING_MODE_AIR_COOLED");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });
  });

  test("Set COOLING MODE to Immersion Cooled for a single miner", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support COOLING_MODE action
      await minersPage.filterRigMiners();
    });

    const requestPromise = page.waitForRequest(/SetCoolingMode/);
    const responsePromise = page.waitForResponse(/SetCoolingMode/);

    await test.step("Select first miner and set Immersion Cooled mode", async () => {
      const minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickCoolingModeButton();
      await minersPage.clickImmersionCooledOption();
      await minersPage.clickUpdateCoolingModeConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Setting cooling mode");
      await minersPage.validateTextInToastGroup("Updated cooling mode");
    });

    await test.step("Validate 'SetCoolingMode' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("mode");
      test.expect(requestBody.mode).toBe("COOLING_MODE_IMMERSION_COOLED");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });
  });

  test("Set COOLING MODE for multiple miners", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Filter Proto miners as a workaround", async () => {
      // Workaround: Antminer miners don't support COOLING_MODE action
      await minersPage.filterRigMiners();
    });

    const requestPromise = page.waitForRequest(/SetCoolingMode/);
    const responsePromise = page.waitForResponse(/SetCoolingMode/);

    await test.step("Select multiple miners and set Air Cooled mode", async () => {
      const minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      const minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      const minerIp3 = await minersPage.getMinerIpAddressByIndex(2);

      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(minerIp3);
      await minersPage.validateActionBarMinerCount(3);

      await minersPage.clickActionsMenuButton();
      await minersPage.clickCoolingModeButton();
      await minersPage.clickAirCooledOption();
      await minersPage.clickUpdateCoolingModeConfirm();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Setting cooling mode");
      await minersPage.validateTextInToastGroup("Updated cooling mode");
    });

    await test.step("Validate 'SetCoolingMode' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("mode");
      test.expect(requestBody.mode).toBe("COOLING_MODE_AIR_COOLED");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(3);
      test.expect(response.status()).toBe(200);
    });
  });

  test("RENAME a single miner", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const requestPromise = page.waitForRequest(/RenameMiners/);
    const responsePromise = page.waitForResponse(/RenameMiners/);

    const newName = generateRandomText("Renamed Miner E2E");
    let minerIp: string;

    await test.step("Select first miner and rename it", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickRenameButton();
      await minersPage.fillRenameInput(newName);
      await minersPage.clickRenameSave();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Miner renamed");
    });

    await test.step("Validate 'RenameMiners' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });

    await test.step("Validate name updated in miner list", async () => {
      await minersPage.validateMinerName(minerIp, newName);
    });
  });

  test("BULK RENAME multiple miners", async ({ minersPage, page, commonSteps }, testInfo) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(testInfo.project.use?.isMobile === true, "Desktop-only bulk rename flow");
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const requestPromise = page.waitForRequest(/RenameMiners/);
    const responsePromise = page.waitForResponse(/RenameMiners/);

    let minerIp1: string;
    let minerIp2: string;

    await test.step("Select two rig miners and open bulk rename", async () => {
      await minersPage.filterRigMiners();
      minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();
    });

    await test.step("Enable MAC address and validate preview updates", async () => {
      await minersPage.clickBulkRenamePropertyToggle("fixed-mac-address");
      await test.expect(page.getByTestId("bulk-rename-desktop-preview")).toContainText(/([0-9a-f]{2}:){2}/i);
    });

    await test.step("Save the bulk rename", async () => {
      await minersPage.clickBulkRenameSave();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Renamed 2 miners");
    });

    await test.step("Validate 'RenameMiners' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(2);
      test.expect(requestBody.nameConfig.properties).toHaveLength(1);
      test.expect(requestBody.nameConfig.separator).toBe("-");
      test.expect(response.status()).toBe(200);
    });
  });

  test("BULK RENAME mobile layout", async ({ minersPage, page, commonSteps }, testInfo) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(testInfo.project.use?.isMobile !== true, "Mobile-only bulk rename layout");
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Open bulk rename from selected miners", async () => {
      await minersPage.filterRigMiners();
      const minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      const minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();
    });

    await test.step("Validate mobile preview and fixed-value options sheet", async () => {
      await test.expect(page.getByTestId("bulk-rename-mobile-preview")).toBeVisible();
      await minersPage.clickBulkRenamePropertyToggle("fixed-mac-address");
      await minersPage.clickBulkRenamePropertyOptions("fixed-mac-address");
      await minersPage.validateTextIsVisible("Number of characters");
      await test.expect(page.getByTestId("fixed-value-options-save-button-mobile")).toBeVisible();
      await page.getByTestId("fixed-value-options-save-button-mobile").click();
      await minersPage.validateBulkRenamePageOpened();
    });
  });

  test("DELETE - ADD a single miner", async ({ minersPage, commonSteps, addMinersPage }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    let minerCount: number;
    let minerIp: string;

    await test.step("Select a miner and delete it", async () => {
      minerCount = await minersPage.getMinersCount();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickDeleteButton();
      await minersPage.clickDeleteConfirm();
    });

    await test.step("Validate miner was deleted", async () => {
      await minersPage.validateMinerNotPresent(minerIp);
      await minersPage.validateAmountOfMiners(minerCount - 1);
    });

    await test.step("Add a single miner", async () => {
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.clickContinueWithXMiners(1);
    });

    await test.step("Validate miner was added", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(minerIp);
      await minersPage.validateAmountOfMiners(minerCount);
    });
  });

  test("DELETE - ADD multiple miners @smoke", async ({ minersPage, commonSteps, addMinersPage }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    let minerCount: number;
    let minerIp1: string;
    let minerIp2: string;
    let minerIp3: string;

    await test.step("Select multiple miners and delete them", async () => {
      minerCount = await minersPage.getMinersCount();
      minerIp1 = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      minerIp2 = await minersPage.getAuthenticatedMinerIpAddressByIndex(1);
      minerIp3 = await minersPage.getAuthenticatedMinerIpAddressByIndex(2);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickMinerCheckbox(minerIp3);
      await minersPage.validateActionBarMinerCount(3);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickDeleteButton();
      await minersPage.clickDeleteConfirm();
    });

    await test.step("Validate miners were deleted", async () => {
      await minersPage.validateMinerNotPresent(minerIp1);
      await minersPage.validateMinerNotPresent(minerIp2);
      await minersPage.validateMinerNotPresent(minerIp3);
      await minersPage.validateAmountOfMiners(minerCount - 3);
    });

    await test.step("Add multiple miners", async () => {
      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.clickChooseMiners();
      await addMinersPage.clickSelectNone();
      await addMinersPage.clickMinerCheckbox(minerIp1);
      await addMinersPage.clickMinerCheckbox(minerIp2);
      await addMinersPage.clickMinerCheckbox(minerIp3);
      await addMinersPage.clickDone();
      await addMinersPage.clickContinueWithXMiners(3);
    });

    await test.step("Validate miners were added", async () => {
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(minerIp1);
      await minersPage.validateMinerInList(minerIp2);
      await minersPage.validateMinerInList(minerIp3);
      await minersPage.validateAmountOfMiners(minerCount);
    });
  });
});
