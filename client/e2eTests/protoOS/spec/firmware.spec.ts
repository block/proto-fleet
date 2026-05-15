import type { APIRequestContext, Page } from "@playwright/test";
import { expect, test } from "../fixtures/pageFixtures";

type FirmwareState = {
  status: string;
  currentVersion: string;
  newVersion: string | null;
  previousVersion: string | null;
};

const FIRMWARE_STATUS_TIMEOUT_MS = 20_000;
const FIRMWARE_STATUS_POLL_INTERVAL_MS = 250;

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function getAuthAccessToken(page: Page) {
  const authStorage = await page.evaluate(() => localStorage.getItem("proto-os-auth"));
  if (!authStorage) {
    throw new Error("proto-os-auth is missing from localStorage");
  }

  const parsedStorage = JSON.parse(authStorage) as {
    state?: {
      auth?: {
        authTokens?: {
          accessToken?: {
            value?: string;
          };
        };
      };
    };
  };

  const accessToken = parsedStorage.state?.auth?.authTokens?.accessToken?.value;
  if (!accessToken) {
    throw new Error("Access token is missing from proto-os-auth");
  }

  return accessToken;
}

async function getFirmwareState(request: APIRequestContext): Promise<FirmwareState> {
  const response = await request.get("/api/v1/system");
  expect(response.ok()).toBeTruthy();

  const data = (await response.json()) as {
    "system-info": {
      sw_update_status: {
        status: string;
        current_version?: string;
        new_version?: string;
        previous_version?: string;
      };
    };
  };

  const updateStatus = data["system-info"].sw_update_status;

  return {
    status: updateStatus.status,
    currentVersion: updateStatus.current_version ?? "",
    newVersion: updateStatus.new_version ?? null,
    previousVersion: updateStatus.previous_version ?? null,
  };
}

async function waitForFirmwareStatus(
  request: APIRequestContext,
  expectedStatus: string,
  timeoutMs: number = FIRMWARE_STATUS_TIMEOUT_MS,
) {
  const deadline = Date.now() + timeoutMs;
  let lastState: FirmwareState | null = null;
  let lastError: unknown = null;

  while (Date.now() < deadline) {
    try {
      lastState = await getFirmwareState(request);
      if (lastState.status === expectedStatus) {
        return lastState;
      }
      lastError = null;
    } catch (error: unknown) {
      lastError = error;
    }

    await sleep(FIRMWARE_STATUS_POLL_INTERVAL_MS);
  }

  throw new Error(
    `Timed out waiting for firmware status "${expectedStatus}". Last state: ${JSON.stringify(lastState)}. Last error: ${
      lastError instanceof Error ? lastError.message : String(lastError)
    }`,
  );
}

async function uploadFirmwareBundle(request: APIRequestContext, accessToken: string) {
  const response = await request.put("/api/v1/system/update", {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
    multipart: {
      file: {
        name: "proto-os-test-update.swu",
        mimeType: "application/octet-stream",
        buffer: Buffer.from("fake firmware bundle for e2e"),
      },
    },
  });

  expect(response.status()).toBe(200);
}

async function startFirmwareInstall(request: APIRequestContext, accessToken: string) {
  const response = await request.post("/api/v1/system/update", {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
  });

  expect(response.status()).toBe(202);
}

async function rebootAfterFirmwareUpdate(request: APIRequestContext, accessToken: string) {
  const response = await request.post("/api/v1/system/reboot", {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
  });

  expect(response.status()).toBe(202);
}

async function ensureCurrentFirmwareState(request: APIRequestContext, accessToken: string) {
  const state = await getFirmwareState(request);

  switch (state.status) {
    case "current":
      return;
    case "downloading":
      await waitForFirmwareStatus(request, "downloaded");
      return ensureCurrentFirmwareState(request, accessToken);
    case "downloaded":
      await startFirmwareInstall(request, accessToken);
      await waitForFirmwareStatus(request, "installed");
      return ensureCurrentFirmwareState(request, accessToken);
    case "installing":
      await waitForFirmwareStatus(request, "installed");
      return ensureCurrentFirmwareState(request, accessToken);
    case "installed":
      await rebootAfterFirmwareUpdate(request, accessToken);
      await waitForFirmwareStatus(request, "current");
      return;
    default:
      throw new Error(`Unexpected firmware status during cleanup: ${state.status}`);
  }
}

test.describe("Firmware updates", () => {
  let authAccessToken = "";

  test.beforeEach(async ({ page, request, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
    authAccessToken = await getAuthAccessToken(page);
    await ensureCurrentFirmwareState(request, authAccessToken);
    await page.goto("/");
    await commonSteps.navigateToGeneralSettings();
  });

  test.afterEach(async ({ request }) => {
    if (!authAccessToken) {
      return;
    }

    await ensureCurrentFirmwareState(request, authAccessToken);
    authAccessToken = "";
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
    request,
    generalPage,
    headerComponent,
  }) => {
    const startingVersion = await generalPage.getFirmwareVersion();
    let installedVersion = "";

    await test.step("Upload a firmware bundle and validate the staged install state", async () => {
      await uploadFirmwareBundle(request, authAccessToken);

      const downloadedState = await waitForFirmwareStatus(request, "downloaded");
      installedVersion = downloadedState.newVersion ?? "";

      expect(installedVersion).not.toBe("");
      expect(installedVersion).not.toBe(startingVersion);

      await generalPage.reloadPage();
      await generalPage.validateTitle("General");
      await headerComponent.validateFirmwareStatusWidgetText(/Ready to install/);
      await headerComponent.openFirmwareStatusModal();
      await headerComponent.validateFirmwareStatusModalTitle("Ready to install");
      await headerComponent.validateFirmwareStatusModalVersionLabel("Current Version:", startingVersion);
      await headerComponent.validateFirmwareStatusModalVersionLabel("New Version:", installedVersion);
    });

    await test.step("Install the uploaded firmware and wait for reboot-required state", async () => {
      await headerComponent.clickFirmwareStatusModalInstallButton();

      await waitForFirmwareStatus(request, "installing");

      await generalPage.reloadPage();
      await generalPage.validateTitle("General");
      await headerComponent.validateFirmwareStatusWidgetText(/Installing/);

      await waitForFirmwareStatus(request, "installed");

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

      const currentState = await waitForFirmwareStatus(request, "current");
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
