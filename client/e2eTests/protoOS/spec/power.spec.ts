import { expect, test } from "../fixtures/pageFixtures";
import { getAuthAccessToken, waitForAuthenticatedApiRecovery } from "../helpers/apiAuthHelper";

test.describe("Power management", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Miner can be rebooted from the header power menu", async ({ headerComponent, page, request }) => {
    const accessToken = await getAuthAccessToken(page);
    const rebootRequestPromise = page.waitForRequest(
      (request) => request.method() === "POST" && request.url().includes("/api/v1/system/reboot"),
    );
    const rebootResponsePromise = page.waitForResponse(
      (response) => response.request().method() === "POST" && response.url().includes("/api/v1/system/reboot"),
    );

    await test.step("Open the header power menu and choose reboot", async () => {
      await headerComponent.clickPowerButton();
      await headerComponent.clickPowerPopoverButton("Reboot");
      await headerComponent.validateWarnRebootDialog();
    });

    await test.step("Confirm the reboot request starts", async () => {
      await headerComponent.clickRebootMinerInDialog();

      const rebootRequest = await rebootRequestPromise;
      const rebootResponse = await rebootResponsePromise;

      expect(rebootRequest.method()).toBe("POST");
      expect(rebootResponse.status()).toBe(202);
      await headerComponent.validateRebootingDialogVisible();
    });

    await test.step("Wait for the miner to come back and validate the UI recovers", async () => {
      await waitForAuthenticatedApiRecovery({
        accessToken,
        path: "/api/v1/mining",
        request,
      });

      await page.goto("/");
      await headerComponent.validateMinerStatus("Hashing");
    });
  });

  test("Miner sleep status in different pages", async ({
    homePage,
    commonSteps,
    headerComponent,
    sleepWakeDialogsComponent,
  }) => {
    await test.step("Put miner to SLEEP", async () => {
      await headerComponent.clickPowerButton();
      await headerComponent.clickPowerPopoverButton("Sleep");
    });

    await test.step("Confirm enter SLEEP mode", async () => {
      await homePage.validateWarnSleepDialog();
      await sleepWakeDialogsComponent.clickEnterSleepMode();
      await sleepWakeDialogsComponent.validateEnteringSleepDialog();
    });

    await test.step("Validate miner status is Sleeping", async () => {
      await headerComponent.validateMinerStatus("Sleeping");
    });

    await commonSteps.navigateToDiagnostics();
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToLogs();
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToAuthenticationSettings();
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToGeneralSettings(false);
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToPoolsSettings(false);
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToHardwareSettings(false);
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToCoolingSettings(false);
    await commonSteps.validateWakeCallout();

    await commonSteps.navigateToHome();

    await test.step("Wake miner up", async () => {
      await headerComponent.clickPowerButton();
      await headerComponent.clickPowerPopoverButton("Wake up");
    });

    await test.step("Confirm wake up miner", async () => {
      await homePage.validateWarnWakeUpDialog();
      await sleepWakeDialogsComponent.clickWakeMinerInDialog();
      await sleepWakeDialogsComponent.validateWakingDialog();
    });

    await test.step("Validate miner status is Hashing", async () => {
      await headerComponent.validateMinerStatus("Hashing");
    });
  });

  test("Different ways of setting miner to sleep and waking it up", async ({
    commonSteps,
    headerComponent,
    sleepWakeDialogsComponent,
  }) => {
    await test.step("Put miner to sleep from home page", async () => {
      await headerComponent.clickPowerButton();
      await headerComponent.clickPowerPopoverButton("Sleep");
      await sleepWakeDialogsComponent.clickEnterSleepMode();
      await sleepWakeDialogsComponent.validateEnteringSleepDialog();
    });

    await test.step("Wake miner up from header status", async () => {
      await headerComponent.clickMinerStatusButton();
      await sleepWakeDialogsComponent.validateMinerAsleepModal();
      await sleepWakeDialogsComponent.clickWakeMinerInModal();
      await sleepWakeDialogsComponent.clickWakeMinerInDialog();
      await sleepWakeDialogsComponent.validateWakingDialog();
      await headerComponent.validateMinerStatus("Hashing");
    });

    await commonSteps.navigateToDiagnostics();
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();

    await commonSteps.navigateToLogs();
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();

    await commonSteps.navigateToAuthenticationSettings();
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();

    await commonSteps.navigateToGeneralSettings(false);
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();

    await commonSteps.navigateToPoolsSettings(false);
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();

    await commonSteps.navigateToHardwareSettings(false);
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();

    await commonSteps.navigateToCoolingSettings(false);
    await commonSteps.putMinerToSleep();
    await commonSteps.wakeMinerFromCallout();
  });
});
