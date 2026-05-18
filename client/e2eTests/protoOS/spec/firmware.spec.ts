import { expect, test } from "../fixtures/pageFixtures";
import { FirmwareHelper } from "../helpers/firmwareHelper";
import { HeaderComponent } from "../pages/components/header";
import { GeneralPage } from "../pages/general";

type UploadState = "downloaded" | "installing";

async function handleUploadedFirmwareState(
  uploadState: UploadState,
  headerComponent: HeaderComponent,
  generalPage: GeneralPage,
  startingVersion: string,
  installedVersion: string,
) {
  await generalPage.reloadPage();
  await generalPage.validateTitle("General");

  if (uploadState === "downloaded") {
    await headerComponent.validateFirmwareStatusWidgetText(/Ready to install/);
    await headerComponent.openFirmwareStatusModal();
    await headerComponent.validateFirmwareStatusModalTitle("Ready to install");
    await headerComponent.validateFirmwareStatusModalVersionLabel("Current Version:", startingVersion);
    await headerComponent.validateFirmwareStatusModalVersionLabel("New Version:", installedVersion);
    await headerComponent.clickFirmwareStatusModalInstallButton();
    return;
  }

  await headerComponent.validateFirmwareStatusWidgetText(/Installing/);
}

async function getInstallingState(uploadState: UploadState, firmwareHelper: FirmwareHelper) {
  if (uploadState === "installing") {
    return firmwareHelper.getState();
  }

  return firmwareHelper.waitForStatus("installing");
}

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
    let uploadState: UploadState = "installing";

    await test.step("Upload a firmware bundle and validate the update becomes actionable", async () => {
      await firmwareHelper.uploadBundle();

      const stateAfterUpload = await firmwareHelper.waitForAnyStatus(["downloaded", "installing"]);
      uploadState = stateAfterUpload.status as UploadState;
      installedVersion = stateAfterUpload.newVersion ?? "";

      expect(installedVersion).not.toBe("");
      expect(installedVersion).not.toBe(startingVersion);

      await handleUploadedFirmwareState(uploadState, headerComponent, generalPage, startingVersion, installedVersion);
    });

    await test.step("Wait for the install to enter the installing state", async () => {
      const installingState = await getInstallingState(uploadState, firmwareHelper);
      installedVersion = installingState.newVersion ?? installedVersion;

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
