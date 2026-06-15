import { test } from "../fixtures/pageFixtures";

test.describe("Hardware settings", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Hardware settings show the current control board, hashboards, fans, and PSUs", async ({
    commonSteps,
    hardwarePage,
  }) => {
    await commonSteps.navigateToHardwareSettings();

    await test.step("Validate the main hardware sections are visible", async () => {
      await hardwarePage.validateSectionHeadings();
      await hardwarePage.validateControlBoardSerialLooksLikeSimulatorData();
    });

    await test.step("Validate the default fake rig inventory is shown", async () => {
      await hardwarePage.validateHashboardInventory();
      await hardwarePage.validateFanInventory();
      await hardwarePage.validatePsuInventory();
    });
  });

  test("Hardware settings keep the fan table visible in the default cooling mode", async ({
    commonSteps,
    hardwarePage,
  }) => {
    await commonSteps.navigateToHardwareSettings();

    await test.step("Validate the default Hardware page shows the fan table instead of the immersion callout", async () => {
      await hardwarePage.validateNoFansConnectedCalloutHidden();
      await hardwarePage.validateFanInventory();
    });
  });
});
