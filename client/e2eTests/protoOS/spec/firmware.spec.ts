import { expect, test } from "../fixtures/pageFixtures";

test.describe("Firmware updates", () => {
  test.beforeEach(async ({ page, commonSteps, firmwareHelper }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
    await firmwareHelper.initializeAuthAccessToken();
    await firmwareHelper.ensureCurrentState();
    await page.goto("/");
    await commonSteps.navigateToGeneralSettings();
  });

  test.afterEach(async ({ firmwareHelper }) => {
    if (!firmwareHelper.hasAuthAccessToken()) {
      return;
    }

    await firmwareHelper.ensureCurrentState();
    firmwareHelper.clearAuthAccessToken();
  });

  test("Firmware version and check-for-updates state stay stable when already current", async ({
    generalPage,
    headerComponent,
  }) => {
    const currentVersion = await generalPage.getFirmwareVersion();

    await test.step("Validate the current firmware section state", async () => {
      await generalPage.validateFirmwareVersion(currentVersion);
      await generalPage.validateCheckForUpdatesButtonVisible();
      await headerComponent.validateFirmwareStatusWidgetHidden();
    });

    await test.step("Check for updates and confirm the current state stays stable", async () => {
      await generalPage.clickCheckForUpdatesButton();
      await generalPage.validateCheckForUpdatesButtonVisible();
      await headerComponent.validateFirmwareStatusWidgetHidden();
      await generalPage.reloadPage();
      await generalPage.validateTitle("General");
      await generalPage.validateFirmwareVersion(currentVersion);
      await generalPage.validateCheckForUpdatesButtonVisible();
    });
  });

  test("Uploaded firmware can be installed and rebooted into the new current version", async ({
    page,
    generalPage,
    headerComponent,
    firmwareHelper,
  }) => {
    const startingVersion = await generalPage.getFirmwareVersion();
    let installedVersion = "";

    await test.step("Upload a firmware bundle and validate the install starts", async () => {
      await firmwareHelper.uploadBundle();

      const installingState = await firmwareHelper.waitForStatus("installing");
      installedVersion = installingState.newVersion ?? "";

      expect(installedVersion).not.toBe("");
      expect(installedVersion).not.toBe(startingVersion);

      await generalPage.reloadPage();
      await generalPage.validateTitle("General");
      await headerComponent.validateFirmwareStatusWidgetText(/Installing/);
    });

    await test.step("Wait for reboot-required state after the upload-driven install", async () => {
      const installedState = await firmwareHelper.waitForStatus("installed");
      installedVersion = installedState.newVersion ?? installedVersion;

      await generalPage.reloadPage();
      await generalPage.validateTitle("General");
      await generalPage.validateInlineFirmwareStatus(/Reboot required/);
      await headerComponent.validateFirmwareStatusWidgetText(/Reboot required/);
      await headerComponent.openFirmwareStatusModal();
      await headerComponent.validateFirmwareStatusModalTitle("Update installed");
      await headerComponent.validateFirmwareStatusModalVersionLabel("Current Version:", startingVersion);
      await headerComponent.validateFirmwareStatusModalVersionLabel("New Version:", installedVersion);
    });

    await test.step("Reboot and validate the new firmware becomes current", async () => {
      await headerComponent.clickFirmwareStatusModalRebootButton();

      const currentState = await firmwareHelper.waitForStatus("current");
      expect(currentState.currentVersion).toBe(installedVersion);
      expect(currentState.previousVersion).toBe(startingVersion);

      await page.goto("/settings/general");
      await generalPage.validateTitle("General");
      await generalPage.validateFirmwareVersion(installedVersion);
      await generalPage.validateCheckForUpdatesButtonVisible();
      await headerComponent.validateFirmwareStatusWidgetHidden();
    });
  });
});
