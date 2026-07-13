import { type Browser } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import {
  provisionRoleAndLoginViaStoredAdminContext,
  useRbacHooks,
  wakeRigMinerIfSleeping,
} from "../helpers/rbacTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";

const MINER_READ_PERMISSIONS = ["fleet:read", "miner:read"] as const;

async function provisionMinerRole(
  browser: Browser,
  commonSteps: Parameters<typeof provisionRoleAndLoginViaStoredAdminContext>[2],
  {
    permissionKeys,
    roleDescription,
  }: {
    permissionKeys: string[];
    roleDescription: string;
  },
) {
  return await provisionRoleAndLoginViaStoredAdminContext(browser, test.info(), commonSteps, {
    permissionKeys,
    roleDescription,
  });
}

test.describe("Proto Fleet - Miner RBAC", () => {
  useRbacHooks();

  test("Miners read-only role can view the miner list and status without mutating action controls", async ({
    browser,
    commonSteps,
    minersPage,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Read-only miner access for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    const minerStatus = (await minersPage.getMinerStatus(minerIp)).trim();

    expect(minerIp).not.toBe("");
    expect(minerStatus).not.toBe("");

    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.validateSingleMinerActionsHidden([
      "blink-leds-popover-button",
      "reboot-popover-button",
      "manage-power-popover-button",
      "mining-pool-popover-button",
      "firmware-update-popover-button",
      "cooling-mode-popover-button",
      "download-logs-popover-button",
      "rename-popover-button",
      "update-worker-names-popover-button",
      "security-popover-button",
      "unpair-popover-button",
    ]);
  });

  test("Miners blink-led role can blink a miner locator LED", async ({ browser, commonSteps, minersPage }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Blink miner LEDs for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:blink_led"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickBlinkLEDsButton();
    await minersPage.validateTextInToastGroup("Blinking LEDs");
    await minersPage.validateTextInToastGroup("Blinked LEDs");
  });

  test("Miners reboot role can open the reboot confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Reboot miners for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:reboot"],
    });

    await commonSteps.goToMinersPage();
    const minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickRebootButton();
    await expect(page.getByTestId("reboot-confirm-button")).toBeVisible();
    await minersPage.cancelSingleMinerConfirmationDialog();
  });

  test("Miners start-mining role can open the wake-up confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      testConfig.target === "real",
      "Stateful miner RBAC action coverage is only supported against fake targets.",
    );

    await commonSteps.loginAsAdmin({ forceReauth: true });
    await commonSteps.goToMinersPage();
    const minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickShutdownButton();
    await minersPage.clickShutdownConfirm();
    await minersPage.validateMinerStatusSettled(minerIp, "Sleeping");

    try {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Wake miners for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:start_mining"],
      });

      await commonSteps.goToMinersPage();
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickWakeUpButton();
      await expect(page.getByTestId("wake-up-confirm-button")).toBeVisible();
      await minersPage.cancelSingleMinerConfirmationDialog();
    } finally {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await wakeRigMinerIfSleeping(minersPage, minerIp);
    }
  });

  test("Miners stop-mining role can open the sleep confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Stop miners for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:stop_mining"],
    });

    await commonSteps.goToMinersPage();
    const minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickShutdownButton();
    await expect(page.getByTestId("shutdown-confirm-button")).toBeVisible();
    await minersPage.cancelSingleMinerConfirmationDialog();
  });

  test("Miners update-pools role can open the pool editor from a miner action menu", async ({
    browser,
    commonSteps,
    loginModal,
    minersPage,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Update miner pools for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "pool:read", "miner:update_pools"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickEditMiningPoolButton();
    await loginModal.validateTitleInModal("Log in to update your pool settings");
  });

  test("Miners update-worker-names role can open the worker-name flow", async ({
    browser,
    commonSteps,
    loginModal,
    minersPage,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Update worker names for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:update_worker_names"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickUpdateWorkerNameButton();
    await loginModal.validateTitleInModal("Log in to update worker names");
  });

  test("Miners rename role can open the rename flow", async ({ browser, commonSteps, minersPage }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Rename miners for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:rename"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickRenameButton();
    await minersPage.validateTitleInModal("Rename miner");
    await minersPage.fillRenameInput(generateRandomText("rbac_rename_preview"));
    await minersPage.dismissModalIfVisible();
  });

  test("Miners delete role can open the unpair confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Delete miners from fleet for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:delete"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickUnpairButton();
    await expect(page.getByTestId("unpair-confirm-button")).toBeVisible();
    await minersPage.dismissModalIfVisible();
  });

  test("Miners cooling-mode role can open the cooling-mode flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Change cooling mode for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:set_cooling_mode"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickCoolingModeButton();
    await expect(page.getByTestId("cooling-option-air")).toBeVisible();
    await expect(page.getByTestId("cooling-option-immersion")).toBeVisible();
    await minersPage.dismissModalIfVisible();
  });

  test("Miners power-target role can open the power-target flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Change miner power targets for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:set_power_target"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickManagePowerButton();
    await expect(page.getByTestId("power-option-maximize")).toBeVisible();
    await expect(page.getByTestId("power-option-reduce")).toBeVisible();
    await minersPage.dismissModalIfVisible();
  });

  test("Miners firmware-update role can open the firmware-update flow", async ({
    browser,
    commonSteps,
    minersPage,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Update miner firmware for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:firmware_update", "miner:reboot"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickUpdateFirmwareButton();
    await minersPage.validateFirmwareUpdateModalOpened();
    await minersPage.dismissModalIfVisible();
  });

  test("Miners download-logs role can start a diagnostic log download", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Download miner logs for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:download_logs"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    const downloadPromise = page.waitForEvent("download");

    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickDownloadLogsButton();

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toMatch(/\.(zip|csv)$/i);
    await minersPage.validateTextInToastGroup("Downloaded logs");
  });

  test("Miners update-password role can open the manage-security password flow", async ({
    browser,
    commonSteps,
    minersPage,
    loginModal,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Update miner passwords for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:update_password"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    await minersPage.clickMinerThreeDotsButton(minerIp);
    await minersPage.clickManageSecurityButton();
    await loginModal.validateTitleInModal("Log in to update your security settings");
  });

  test("Miners pair role can discover miners in the add-miners flow", async ({
    addMinersPage,
    browser,
    commonSteps,
    minersPage,
  }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Pair miners for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:pair", "fleetnode:manage"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.clickAddMinersButton();
    await addMinersPage.validateAddMinersFlowOpened();
    await addMinersPage.clickHeaderIconButton();
  });

  test("Miners export-csv role can export the miner list", async ({ browser, commonSteps, minersPage, page }) => {
    await provisionMinerRole(browser, commonSteps, {
      roleDescription: "Export miner list CSV for RBAC coverage.",
      permissionKeys: [...MINER_READ_PERMISSIONS, "miner:export_csv"],
    });

    await commonSteps.goToMinersPage();
    await minersPage.filterRigMiners();

    const downloadPromise = page.waitForEvent("download");
    await minersPage.clickButton("Export CSV");
    const download = await downloadPromise;

    expect(download.suggestedFilename()).toMatch(/miner|proto-fleet-miner-snapshot/i);
  });
});
