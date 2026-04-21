/* eslint-disable playwright/expect-expect */
import { test } from "../fixtures/pageFixtures";

test.describe("Power management", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
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
