import { test } from "../fixtures/pageFixtures";

test.describe("Miners", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
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
});
