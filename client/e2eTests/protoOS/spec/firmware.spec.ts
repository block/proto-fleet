import type { Page, Route } from "@playwright/test";
import { expect, test } from "../fixtures/pageFixtures";
import { FirmwareHelper } from "../helpers/firmwareHelper";
import { HeaderComponent } from "../pages/components/header";
import { GeneralPage } from "../pages/general";

type UploadState = "downloaded" | "installing" | "installed";

const SYSTEM_INFO_ROUTE = "**/api/v1/system";

async function validateInstallingWidget(
  page: Page,
  firmwareHelper: FirmwareHelper,
  generalPage: GeneralPage,
  headerComponent: HeaderComponent,
) {
  const systemInfo = await firmwareHelper.getSystemInfo();
  systemInfo["system-info"].sw_update_status.status = "installing";
  const fulfillInstallingSystemInfo = (route: Route) => route.fulfill({ json: systemInfo });

  await page.route(SYSTEM_INFO_ROUTE, fulfillInstallingSystemInfo);
  try {
    await generalPage.reloadPage();
    await generalPage.validateTitle("General");
    await headerComponent.validateFirmwareStatusWidgetText(/Installing/);
  } finally {
    await page.unroute(SYSTEM_INFO_ROUTE, fulfillInstallingSystemInfo);
  }
}

// The fake rig only holds "downloaded" for firmwareStepDelay (~1s) before it
// auto-advances to "installing" (progress 75%). A live page reload + widget
// assertion doesn't fit in that window, so pin /api/v1/system to a "downloaded"
// payload for the duration of the assertion — mirroring validateInstallingWidget
// — instead of racing the rig's timer.
async function validateDownloadedWidget(
  page: Page,
  firmwareHelper: FirmwareHelper,
  generalPage: GeneralPage,
  headerComponent: HeaderComponent,
  startingVersion: string,
  installedVersion: string,
) {
  const systemInfo = await firmwareHelper.getSystemInfo();
  systemInfo["system-info"].sw_update_status.status = "downloaded";
  systemInfo["system-info"].sw_update_status.new_version = installedVersion;
  const fulfillDownloadedSystemInfo = (route: Route) => route.fulfill({ json: systemInfo });

  await page.route(SYSTEM_INFO_ROUTE, fulfillDownloadedSystemInfo);
  try {
    await generalPage.reloadPage();
    await generalPage.validateTitle("General");
    await headerComponent.validateFirmwareStatusWidgetText(/Ready to install/);
    await headerComponent.openFirmwareStatusModal();
    await headerComponent.validateFirmwareStatusModalTitle("Ready to install");
    await headerComponent.validateFirmwareStatusModalVersionLabel("Current Version:", startingVersion);
    await headerComponent.validateFirmwareStatusModalVersionLabel("New Version:", installedVersion);
    await headerComponent.clickFirmwareStatusModalDismissButton();
  } finally {
    await page.unroute(SYSTEM_INFO_ROUTE, fulfillDownloadedSystemInfo);
  }
}

async function handleUploadedFirmwareState(
  uploadState: UploadState,
  page: Page,
  firmwareHelper: FirmwareHelper,
  headerComponent: HeaderComponent,
  generalPage: GeneralPage,
  startingVersion: string,
  installedVersion: string,
) {
  if (uploadState === "installing") {
    await validateInstallingWidget(page, firmwareHelper, generalPage, headerComponent);
    return;
  }

  if (uploadState === "downloaded") {
    await validateDownloadedWidget(
      page,
      firmwareHelper,
      generalPage,
      headerComponent,
      startingVersion,
      installedVersion,
    );
    return;
  }

  await generalPage.reloadPage();
  await generalPage.validateTitle("General");

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

  // Coming from "downloaded": the rig auto-advances on its own timer, so by the
  // time we get here it may already be "installed". Accept either so we don't
  // race a window that has closed; validateInstallingState handles "installed".
  return firmwareHelper.waitForAnyStatus(["installing", "installed"]);
}

async function validateInstallingState(
  installingState: Awaited<ReturnType<FirmwareHelper["getState"]>>,
  page: Page,
  firmwareHelper: FirmwareHelper,
  generalPage: GeneralPage,
  headerComponent: HeaderComponent,
) {
  if (installingState.status === "installed") {
    return;
  }

  await validateInstallingWidget(page, firmwareHelper, generalPage, headerComponent);
}

test.describe("Firmware updates", () => {
  test.beforeEach(async ({ page, commonSteps, firmwareHelper }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
    await firmwareHelper.initializeAuthAccessToken();
    await firmwareHelper.assertSafeDummyUploadTarget();
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
    const writes: string[] = [];
    page.on("request", (request) => {
      if (request.url().endsWith("/api/v1/system/update")) writes.push(request.method());
    });

    await test.step("Upload a firmware bundle and validate the update becomes actionable", async () => {
      await generalPage.chooseFirmwareFile();
      expect(writes).toEqual([]);
      const upload = page.waitForRequest(
        (request) => request.method() === "PUT" && request.url().endsWith("/api/v1/system/update"),
      );
      await generalPage.confirmFirmwareUpload();
      const request = await upload;
      expect(request.headers()["content-type"]).toContain("multipart/form-data; boundary=");
      expect(request.headers().authorization).toMatch(/^Bearer /);

      const stateAfterUpload = await firmwareHelper.waitForAnyStatus(["downloaded", "installing", "installed"]);
      uploadState = stateAfterUpload.status as UploadState;
      installedVersion = stateAfterUpload.newVersion ?? "";

      expect(installedVersion).not.toBe("");
      expect(installedVersion).not.toBe(startingVersion);
      // Upload itself starts installation; the browser must not request a catalog install too.
      expect(writes).toEqual(["PUT"]);

      await handleUploadedFirmwareState(
        uploadState,
        page,
        firmwareHelper,
        headerComponent,
        generalPage,
        startingVersion,
        installedVersion,
      );
    });

    await test.step("Wait for the install to enter the installing state", async () => {
      const installingState = await getInstallingState(uploadState, firmwareHelper);
      installedVersion = installingState.newVersion ?? installedVersion;
      await validateInstallingState(installingState, page, firmwareHelper, generalPage, headerComponent);
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
      expect(writes).toEqual(["PUT"]);
      expect(currentState.currentVersion).toBe(installedVersion);
      expect(currentState.previousVersion).toBe(startingVersion);

      await page.goto("/settings/general");
      await generalPage.validateTitle("General");
      await generalPage.validateFirmwareVersion(installedVersion);
      await generalPage.validateCheckForUpdatesButtonVisible();
      await headerComponent.validateFirmwareStatusWidgetHidden();
    });
  });

  test("Rejected local uploads retain the file and allow another attempt", async ({ page, generalPage }) => {
    await page.route("**/api/v1/system/update", (route) =>
      route.fulfill({ status: 400, json: { message: "Invalid firmware" } }),
    );
    await generalPage.chooseFirmwareFile();
    await generalPage.confirmFirmwareUpload();
    await generalPage.validateFirmwareUploadError(/The rig rejected this firmware file/);
    await generalPage.confirmFirmwareUpload();
    await generalPage.validateFirmwareUploadError(/The rig rejected this firmware file/);
  });
});
