import type { Page, Route } from "@playwright/test";
import { expect, test } from "../fixtures/pageFixtures";
import { FirmwareHelper } from "../helpers/firmwareHelper";
import { HeaderComponent } from "../pages/components/header";
import { GeneralPage } from "../pages/general";

type UploadState = "downloaded" | "installing" | "installed";

const SYSTEM_INFO_ROUTE = "**/api/v1/system";

async function validateInstallingWidget(page: Page, generalPage: GeneralPage, headerComponent: HeaderComponent) {
  const fulfillInstallingSystemInfo = async (route: Route) => {
    const response = await route.fetch();
    const data = (await response.json()) as {
      "system-info": {
        sw_update_status: {
          status: string;
        };
      };
    };
    data["system-info"].sw_update_status.status = "installing";
    await route.fulfill({ response, json: data });
  };

  await page.route(SYSTEM_INFO_ROUTE, fulfillInstallingSystemInfo);
  try {
    await generalPage.reloadPage();
    await generalPage.validateTitle("General");
    await headerComponent.validateFirmwareStatusWidgetText(/Installing/);
  } finally {
    await page.unroute(SYSTEM_INFO_ROUTE, fulfillInstallingSystemInfo);
  }
}

async function handleUploadedFirmwareState(
  uploadState: UploadState,
  page: Page,
  headerComponent: HeaderComponent,
  generalPage: GeneralPage,
  startingVersion: string,
  installedVersion: string,
) {
  if (uploadState === "installing") {
    await validateInstallingWidget(page, generalPage, headerComponent);
    return;
  }

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

  await generalPage.validateInlineFirmwareStatus(/Reboot required/);
  await headerComponent.validateFirmwareStatusWidgetText(/Reboot required/);
}

async function getInstallingState(uploadState: UploadState, firmwareHelper: FirmwareHelper) {
  if (uploadState === "installed") {
    return firmwareHelper.getState();
  }

  if (uploadState === "installing") {
    return firmwareHelper.getState();
  }

  return firmwareHelper.waitForStatus("installing");
}

async function validateInstallingState(
  installingState: Awaited<ReturnType<FirmwareHelper["getState"]>>,
  page: Page,
  generalPage: GeneralPage,
  headerComponent: HeaderComponent,
) {
  if (installingState.status === "installed") {
    return;
  }

  await validateInstallingWidget(page, generalPage, headerComponent);
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

      const stateAfterUpload = await firmwareHelper.waitForAnyStatus(["downloaded", "installing", "installed"]);
      uploadState = stateAfterUpload.status as UploadState;
      installedVersion = stateAfterUpload.newVersion ?? "";

      expect(installedVersion).not.toBe("");
      expect(installedVersion).not.toBe(startingVersion);

      await handleUploadedFirmwareState(
        uploadState,
        page,
        headerComponent,
        generalPage,
        startingVersion,
        installedVersion,
      );
    });

    await test.step("Wait for the install to enter the installing state", async () => {
      const installingState = await getInstallingState(uploadState, firmwareHelper);
      installedVersion = installingState.newVersion ?? installedVersion;
      await validateInstallingState(installingState, page, generalPage, headerComponent);
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
