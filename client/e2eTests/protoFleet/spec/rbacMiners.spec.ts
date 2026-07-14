import { type Browser } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { installAllSitesInitScript } from "../helpers/fleetLocationsSetup";
import {
  cleanupRbacTeamArtifacts,
  ensureVisibleRigMinersAwake,
  provisionRoleAndLoginViaStoredAdminContext,
  selectHashingRigMinerForStopFlow,
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
  test.beforeEach(async ({ page }) => {
    await installAllSitesInitScript(page);
    await page.goto("/");
  });

  test.afterAll("CLEANUP: delete RBAC team fixtures", async ({ browser }, testInfo) => {
    await cleanupRbacTeamArtifacts(browser, testInfo);
  });

  test("Miners read-only role can view the miner list and status without mutating action controls", async ({
    browser,
    commonSteps,
    minersPage,
  }) => {
    let minerIp = "";
    let minerStatus = "";

    await test.step("Provision a read-only miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Read-only miner access for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS],
      });
    });

    await test.step("Open Proto rig miners and capture a visible authenticated miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();

      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      minerStatus = (await minersPage.getMinerStatus(minerIp)).trim();

      expect(minerIp).not.toBe("");
      expect(minerStatus).not.toBe("");
    });

    await test.step("Verify single-miner mutating controls stay hidden", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.validateSingleMinerActionsHidden([
        "add-to-site-popover-button",
        "add-to-building-popover-button",
        "add-to-rack-popover-button",
        "add-to-group-popover-button",
        "blink-leds-popover-button",
        "reboot-popover-button",
        "shutdown-popover-button",
        "wake-up-popover-button",
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
  });

  test("Miners blink-led role can blink a miner locator LED", async ({ browser, commonSteps, minersPage }) => {
    let minerIp = "";

    await test.step("Provision a blink-led miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Blink miner LEDs for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:blink_led"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Blink the miner locator LED and validate toasts", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickBlinkLEDsButton();
      await minersPage.validateTextInToastGroup("Blinking LEDs");
      await minersPage.validateTextInToastGroup("Blinked LEDs");
    });
  });

  test("Miners reboot role can open the reboot confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    let minerIp = "";

    await test.step("Provision a reboot-capable miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Reboot miners for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:reboot"],
      });
    });

    await test.step("Open the miners page and select a hashing miner", async () => {
      await commonSteps.goToMinersPage();
      minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
    });

    await test.step("Open the reboot confirmation flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickRebootButton();
      await expect(page.getByTestId("reboot-confirm-button")).toBeVisible();
      await minersPage.cancelSingleMinerConfirmationDialog();
    });
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

    let minerIp = "";

    await test.step("Prepare a sleeping Proto rig as admin", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await ensureVisibleRigMinersAwake(minersPage);
      minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickShutdownButton();
      await minersPage.clickShutdownConfirm();
      await minersPage.validateMinerStatusSettled(minerIp, "Sleeping");
    });

    try {
      await test.step("Provision a start-mining miner role", async () => {
        await provisionMinerRole(browser, commonSteps, {
          roleDescription: "Wake miners for RBAC coverage.",
          permissionKeys: [...MINER_READ_PERMISSIONS, "miner:start_mining"],
        });
      });

      await test.step("Open the wake-up confirmation flow", async () => {
        await commonSteps.goToMinersPage();
        await minersPage.clickMinerThreeDotsButton(minerIp);
        await minersPage.clickWakeUpButton();
        await expect(page.getByTestId("wake-up-confirm-button")).toBeVisible();
        await minersPage.cancelSingleMinerConfirmationDialog();
      });
    } finally {
      await test.step("Restore the sleeping rig miner", async () => {
        await commonSteps.loginAsAdmin({ forceReauth: true });
        await commonSteps.goToMinersPage();
        await wakeRigMinerIfSleeping(minersPage, minerIp);
      });
    }
  });

  test("Miners stop-mining role can open the sleep confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    let minerIp = "";

    await test.step("Prepare a hashing Proto rig", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      minerIp = await selectHashingRigMinerForStopFlow(minersPage);
    });

    await test.step("Provision a stop-mining miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Stop miners for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:stop_mining"],
      });
    });

    await test.step("Open Proto rig miners", async () => {
      await commonSteps.goToMinersPage();
    });

    await test.step("Open the sleep confirmation flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickShutdownButton();
      await expect(page.getByTestId("shutdown-confirm-button")).toBeVisible();
      await minersPage.cancelSingleMinerConfirmationDialog();
    });
  });

  test("Miners update-pools role can open the pool editor from a miner action menu", async ({
    browser,
    commonSteps,
    loginModal,
    minersPage,
  }) => {
    let minerIp = "";

    await test.step("Provision an update-pools miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Update miner pools for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "pool:read", "miner:update_pools"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the pool editor login prompt", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickEditMiningPoolButton();
      await loginModal.validateTitleInModal("Log in to update your pool settings");
    });
  });

  test("Miners update-worker-names role can open the worker-name flow", async ({
    browser,
    commonSteps,
    loginModal,
    minersPage,
  }) => {
    let minerIp = "";

    await test.step("Provision an update-worker-names miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Update worker names for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:update_worker_names"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the worker-name login prompt", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUpdateWorkerNameButton();
      await loginModal.validateTitleInModal("Log in to update worker names");
    });
  });

  test("Miners rename role can open the rename flow", async ({ browser, commonSteps, minersPage }) => {
    let minerIp = "";

    await test.step("Provision a rename miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Rename miners for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:rename"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the rename flow and validate its controls", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickRenameButton();
      await minersPage.validateTitleInModal("Rename miner");
      await minersPage.fillRenameInput(generateRandomText("rbac_rename_preview"));
      await minersPage.dismissModalIfVisible();
    });
  });

  test("Miners delete role can open the unpair confirmation flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    let minerIp = "";

    await test.step("Provision a delete miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Delete miners from fleet for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:delete"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the unpair confirmation flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUnpairButton();
      await expect(page.getByTestId("unpair-confirm-button")).toBeVisible();
      await minersPage.dismissModalIfVisible();
    });
  });

  test("Miners cooling-mode role can open the cooling-mode flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    let minerIp = "";

    await test.step("Provision a cooling-mode miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Change cooling mode for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:set_cooling_mode"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the cooling-mode flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickCoolingModeButton();
      await expect(page.getByTestId("cooling-option-air")).toBeVisible();
      await expect(page.getByTestId("cooling-option-immersion")).toBeVisible();
      await minersPage.dismissModalIfVisible();
    });
  });

  test("Miners power-target role can open the power-target flow", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    let minerIp = "";

    await test.step("Provision a power-target miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Change miner power targets for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:set_power_target"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the power-target flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickManagePowerButton();
      await expect(page.getByTestId("power-option-maximize")).toBeVisible();
      await expect(page.getByTestId("power-option-reduce")).toBeVisible();
      await minersPage.dismissModalIfVisible();
    });
  });

  test("Miners firmware-update role can open the firmware-update flow", async ({
    browser,
    commonSteps,
    minersPage,
  }) => {
    let minerIp = "";

    await test.step("Provision a firmware-update miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Update miner firmware for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:firmware_update", "miner:reboot"],
      });
    });

    await test.step("Open Proto rig miners and select a hashing miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
    });

    await test.step("Open the firmware-update flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUpdateFirmwareButton();
      await minersPage.validateFirmwareUpdateModalOpened();
      await minersPage.dismissModalIfVisible();
    });
  });

  test("Miners download-logs role can start a diagnostic log download", async ({
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    let minerIp = "";

    await test.step("Provision a download-logs miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Download miner logs for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:download_logs"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Start a diagnostic log download", async () => {
      const downloadPromise = page.waitForEvent("download");

      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickDownloadLogsButton();

      const download = await downloadPromise;
      expect(download.suggestedFilename()).toMatch(/\.(zip|csv)$/i);
      await minersPage.validateTextInToastGroup("Downloaded logs");
    });
  });

  test("Miners update-password role can open the manage-security password flow", async ({
    browser,
    commonSteps,
    minersPage,
    loginModal,
  }) => {
    let minerIp = "";

    await test.step("Provision an update-password miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Update miner passwords for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:update_password"],
      });
    });

    await test.step("Open Proto rig miners and select a miner", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
    });

    await test.step("Open the manage-security password flow", async () => {
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickManageSecurityButton();
      await loginModal.validateTitleInModal("Log in to update your security settings");
    });
  });

  test("Miners pair role can discover miners in the add-miners flow", async ({
    addMinersPage,
    browser,
    commonSteps,
    minersPage,
    page,
  }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      testConfig.target === "real",
      "Pair discovery RBAC coverage temporarily unpairs a miner and is only supported against fake targets.",
    );

    let minerIp = "";

    await test.step("Prepare an unpaired Proto rig", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      minerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
      await minersPage.validateMinerNotPresent(minerIp);
    });

    try {
      await test.step("Provision a pair-capable miner role", async () => {
        await provisionMinerRole(browser, commonSteps, {
          roleDescription: "Pair miners for RBAC coverage.",
          permissionKeys: [...MINER_READ_PERMISSIONS, "miner:pair", "fleetnode:manage"],
        });
      });

      await test.step("Run a pair-gated discovery request by IP and wait for a found miner", async () => {
        const discoverRequestPromise = page.waitForRequest((request) =>
          request.url().includes("/pairing.v1.PairingService/Discover"),
        );
        const discoverResponsePromise = page.waitForResponse((response) =>
          response.url().includes("/pairing.v1.PairingService/Discover"),
        );

        await commonSteps.goToMinersPage();
        await minersPage.clickAddMinersButton();
        await addMinersPage.validateAddMinersFlowOpened();
        await addMinersPage.inputMinerIp(minerIp);
        await addMinersPage.clickFindMinersByIp();

        const discoverRequest = await discoverRequestPromise;
        const discoverResponse = await discoverResponsePromise;

        expect(discoverRequest.method()).toBe("POST");
        expect(discoverRequest.postData() ?? "").toContain(minerIp);
        expect(discoverResponse.status()).toBe(200);

        await addMinersPage.validateOneMinerWasFoundByIp();
        await addMinersPage.clickHeaderIconButton();
      });
    } finally {
      await test.step("Re-add the unpaired Proto rig", async () => {
        await commonSteps.loginAsAdmin({ forceReauth: true });
        await commonSteps.goToMinersPage();
        await minersPage.clickAddMinersButton();
        await addMinersPage.validateAddMinersFlowOpened();
        await addMinersPage.inputMinerIp(minerIp);
        await addMinersPage.clickFindMinersByIp();
        await addMinersPage.validateOneMinerWasFoundByIp();
        await addMinersPage.clickContinueWithSelectedMiners();
        await minersPage.waitForMinersListToLoad();
        await minersPage.validateMinerInList(minerIp);
      });
    }
  });

  test("Miners export-csv role can export the miner list", async ({ browser, commonSteps, minersPage, page }) => {
    await test.step("Provision an export-csv miner role", async () => {
      await provisionMinerRole(browser, commonSteps, {
        roleDescription: "Export miner list CSV for RBAC coverage.",
        permissionKeys: [...MINER_READ_PERMISSIONS, "miner:export_csv"],
      });
    });

    await test.step("Open Proto rig miners", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
    });

    await test.step("Export the miner list as CSV", async () => {
      const downloadPromise = page.waitForEvent("download");
      await minersPage.clickButton("Export CSV");
      const download = await downloadPromise;

      expect(download.suggestedFilename()).toMatch(/miner|proto-fleet-miner-snapshot/i);
    });
  });
});
