import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";

async function cleanupUpdatedRigMiner(minersPage: MinersPage, rigMinerIp: string) {
  const currentStatus = (await minersPage.getMinerStatus(rigMinerIp)).trim();

  if (currentStatus === "Updating firmware") {
    await minersPage.validateMinerStatusSettled(rigMinerIp, "Reboot required", testConfig.testTimeout);
  }

  const rebootRequiredStatus = (await minersPage.getMinerStatus(rigMinerIp)).trim();
  if (rebootRequiredStatus === "Reboot required") {
    await minersPage.clickMinerThreeDotsButton(rigMinerIp);
    await minersPage.clickRebootButton();
    await minersPage.clickRebootConfirm();
    await minersPage.validateMinerStatusSettled(rigMinerIp, "Hashing");
  }
}

test.describe("Firmware", () => {
  let updatedRigMinerIp = "";

  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(testConfig.target === "real", "Firmware update E2E is only supported against the fake proto rig setup.");

  test.beforeEach(async ({ page, commonSteps }) => {
    updatedRigMinerIp = "";
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test.afterEach(
    "CLEANUP: Reboot updated rig miners and delete uploaded firmware files",
    async ({ browser }, testInfo) => {
      const isMobile = testInfo.project.use?.isMobile ?? false;
      const context = await browser.newContext({
        baseURL: testConfig.baseUrl,
        viewport: testInfo.project.use?.viewport,
      });

      try {
        const page = await context.newPage();
        await page.goto("/");

        const authPage = new AuthPage(page, isMobile);
        const minersPage = new MinersPage(page, isMobile);
        const settingsFirmwarePage = new SettingsFirmwarePage(page, isMobile);
        const commonSteps = new CommonSteps(authPage, minersPage);

        await commonSteps.loginAsAdmin();

        await minersPage.navigateToMinersPage();
        await minersPage.waitForMinersListToLoad();
        await minersPage.filterRigMiners();
        if (updatedRigMinerIp) {
          await cleanupUpdatedRigMiner(minersPage, updatedRigMinerIp);
        }

        await settingsFirmwarePage.navigateToFirmwareSettings();
        await settingsFirmwarePage.validateFirmwarePageOpened();
        await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
      } finally {
        updatedRigMinerIp = "";
        await context.close();
      }
    },
  );

  test("Upload firmware and update a rig miner", async ({ minersPage, settingsFirmwarePage }) => {
    test.setTimeout(testConfig.testTimeout * 4);

    const firmwareFileName = `firmware-${Date.now()}.swu`;
    const firmwareFileContents = `fake firmware payload ${Date.now()}`;
    const firmwareStatusTimeout = testConfig.testTimeout;

    await test.step("Upload a firmware payload in Settings", async () => {
      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.validateFirmwarePageOpened();
      await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();

      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(firmwareFileName, firmwareFileContents);
      await settingsFirmwarePage.clickDoneInUploadDialog();
      await settingsFirmwarePage.validateTextInToast("Firmware file uploaded successfully");
      await settingsFirmwarePage.validateFirmwareFileVisible(firmwareFileName);
    });

    let rigMinerIp = "";

    await test.step("Pick a hashing rig miner", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.waitForMinersListToLoad();
      await minersPage.filterRigMiners();
      await test.expect
        .poll(async () => await minersPage.hasAnyMinerWithStatus("Hashing"), {
          timeout: testConfig.testTimeout,
        })
        .toBe(true);

      rigMinerIp = await minersPage.getMinerIpAddressByStatus("Hashing");
      updatedRigMinerIp = rigMinerIp;
    });

    await test.step("Start the firmware update from the miner actions menu", async () => {
      await minersPage.clickMinerThreeDotsButton(rigMinerIp);
      await minersPage.clickUpdateFirmwareButton();
      await minersPage.validateFirmwareUpdateModalOpened();
      await minersPage.selectExistingFirmwareFile(firmwareFileName);
      await minersPage.clickContinueInFirmwareUpdateModal();
    });

    await test.step("Validate the miner transitions through firmware update states", async () => {
      await minersPage.validateMinerStatusSettled(rigMinerIp, "Updating firmware", firmwareStatusTimeout);
      await minersPage.validateMinerStatusSettled(rigMinerIp, "Reboot required", firmwareStatusTimeout);
    });

    await test.step("Reboot the miner and validate it returns to hashing", async () => {
      await minersPage.clickMinerThreeDotsButton(rigMinerIp);
      await minersPage.clickRebootButton();
      await minersPage.clickRebootConfirm();
      await minersPage.validateMinerStatusSettled(rigMinerIp, "Hashing");
    });
  });
});
