import { Page } from "@playwright/test";
import { test } from "../fixtures/pageFixtures";

type CoolingCleanupFixtures = {
  page: Page;
  coolingPage: InstanceType<typeof import("../pages/cooling").CoolingPage>;
  wakeCalloutComponent: InstanceType<typeof import("../pages/components/wakeCallout").WakeCalloutComponent>;
  sleepWakeDialogsComponent: InstanceType<
    typeof import("../pages/components/sleepWakeDialog").SleepWakeDialogsComponent
  >;
  headerComponent: InstanceType<typeof import("../pages/components/header").HeaderComponent>;
};

async function restoreDefaultCoolingState({
  page,
  coolingPage,
  wakeCalloutComponent,
  sleepWakeDialogsComponent,
  headerComponent,
}: CoolingCleanupFixtures) {
  if (await coolingPage.isCoolingInfoModalVisible()) {
    await coolingPage.dismissInfoModal();
  }

  await page.goto("/settings/cooling");
  await coolingPage.validateTitle("Cooling");

  if (await coolingPage.isImmersionCooledSelected()) {
    await coolingPage.clickAirCooledOption();
    await coolingPage.validateCoolingModeUpdatedTo("air cooled");
    await coolingPage.validateAirCooledSelected();
  }

  if (await coolingPage.isWakeCalloutVisible()) {
    await wakeCalloutComponent.clickWakeMinerInCallout();
    await sleepWakeDialogsComponent.clickWakeMinerInDialog();
    await sleepWakeDialogsComponent.validateWakingDialog();
    await headerComponent.validateMinerStatus("Hashing");
    await wakeCalloutComponent.validateWakeCalloutNotVisible();
  }
}

test.describe("Cooling settings", () => {
  test.beforeEach(
    async ({ page, commonSteps, coolingPage, wakeCalloutComponent, sleepWakeDialogsComponent, headerComponent }) => {
      await page.goto("/");
      await commonSteps.authenticateAsAdmin();
      await page.goto("/settings/cooling");
      await coolingPage.validateTitle("Cooling");
      await restoreDefaultCoolingState({
        page,
        coolingPage,
        wakeCalloutComponent,
        sleepWakeDialogsComponent,
        headerComponent,
      });
    },
  );

  test.afterEach(async ({ page, coolingPage, wakeCalloutComponent, sleepWakeDialogsComponent, headerComponent }) => {
    await restoreDefaultCoolingState({
      page,
      coolingPage,
      wakeCalloutComponent,
      sleepWakeDialogsComponent,
      headerComponent,
    });
  });

  test("Switching to immersion requires confirmation and enters sleep flow", async ({
    coolingPage,
    sleepWakeDialogsComponent,
  }) => {
    await test.step("Select immersion cooling", async () => {
      await coolingPage.validateAirCooledSelected();
      await coolingPage.clickImmersionCooledOption();
    });

    await test.step("Confirm immersion cooling", async () => {
      await coolingPage.validateImmersionCoolingModalOpen();
      await coolingPage.clickEnterSleepModeInModal();
      await coolingPage.validateCoolingModeUpdatedTo("immersion cooled");
      await sleepWakeDialogsComponent.validateEnteringSleepDialogVisible();
    });

    await test.step("Validate immersion transition feedback", async () => {
      await sleepWakeDialogsComponent.validateEnteringSleepDialogVisible();
    });
  });

  test("Switching back to air cooled succeeds after immersion mode", async ({
    page,
    coolingPage,
    sleepWakeDialogsComponent,
  }) => {
    await test.step("Enter immersion mode and sleep", async () => {
      await coolingPage.clickImmersionCooledOption();
      await coolingPage.validateImmersionCoolingModalOpen();
      await coolingPage.clickEnterSleepModeInModal();
      await coolingPage.validateCoolingModeUpdatedTo("immersion cooled");
      await sleepWakeDialogsComponent.validateEnteringSleepDialogVisible();
    });

    await test.step("Switch back to air cooling", async () => {
      await page.goto("/settings/cooling");
      await coolingPage.validateTitle("Cooling");
      await coolingPage.validateImmersionCooledSelected();
      await coolingPage.clickAirCooledOption();
      await coolingPage.validateCoolingModeUpdatedTo("air cooled");
      await coolingPage.validateAirCooledSelected();
    });
  });

  test("Learn more modal opens and closes", async ({ coolingPage }) => {
    await test.step("Open learn more modal", async () => {
      await coolingPage.clickLearnMoreButton();
      await coolingPage.validateLearnMoreModalOpen();
    });

    await test.step("Close learn more modal", async () => {
      await coolingPage.dismissInfoModal();
    });
  });
});
