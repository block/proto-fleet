import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";

test.describe("Firmware rollout lanes", () => {
  const runId = Date.now();
  const laneA = `Lane A ${runId}`;
  const laneB = `Lane B ${runId}`;
  // Unique per run: the fake rigs keep their firmware across runs, and a
  // version the miners already report would make the assignment a no-op
  // (no rollout is created when every member is already compliant).
  const firmwareVersion = `3.1.${runId}`;
  const firmwareFileName = `rollout-${firmwareVersion}.swu`;

  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(testConfig.target === "real", "Rollout lane E2E is only supported against the fake proto rig setup.");

  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test.afterEach("CLEANUP: delete lanes and uploaded firmware files", async ({ browser }, testInfo) => {
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

      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.validateFirmwarePageOpened();
      await settingsFirmwarePage.openRolloutLanesTab();
      await settingsFirmwarePage.deleteLaneIfPresent(laneA);
      await settingsFirmwarePage.deleteLaneIfPresent(laneB);
      await settingsFirmwarePage.openFilesTab();
      await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
    } finally {
      await context.close();
    }
  });

  test("Create a lane, enforce firmware per model, and move a miner between lanes", async ({
    settingsFirmwarePage,
  }) => {
    test.setTimeout(testConfig.testTimeout * 6);

    await test.step("Upload a Proto Rig firmware file", async () => {
      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.validateFirmwarePageOpened();
      await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(firmwareFileName, `rollout payload ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(firmwareFileName);
    });

    await test.step("Create an empty lane", async () => {
      await settingsFirmwarePage.openRolloutLanesTab();
      await settingsFirmwarePage.createLane(laneA);
      await settingsFirmwarePage.validateLaneMinerCount(laneA, 0);
    });

    await test.step("Add two Rig miners to the lane", async () => {
      await settingsFirmwarePage.openManageLaneMiners(laneA);
      await settingsFirmwarePage.selectLaneMinersByModel("Rig", 2);
      await settingsFirmwarePage.confirmLaneMinerSelection();
      await settingsFirmwarePage.validateLaneMinerCount(laneA, 2);
    });

    await test.step("Assign firmware to the Rig model group and apply", async () => {
      await settingsFirmwarePage.selectLaneFirmware(laneA, "Rig", new RegExp(firmwareVersion));
      await settingsFirmwarePage.applyLaneFirmwareChanges(laneA);
    });

    await test.step("Lane and app header signal the ongoing rollout", async () => {
      await settingsFirmwarePage.validateLaneRolloutPill(laneA);
      await settingsFirmwarePage.validateAppRolloutPill();
    });

    await test.step("Header pill deep-links to the rollout lanes view", async () => {
      await settingsFirmwarePage.openFilesTab();
      await settingsFirmwarePage.followAppRolloutPillToLanes();
      await settingsFirmwarePage.validateLaneRolloutPill(laneA);
    });

    await test.step("Collapsed model group still shows overall rollout progress", async () => {
      await settingsFirmwarePage.toggleModelGroup(laneA, "Rig");
      await settingsFirmwarePage.validateModelGroupCollapsed(laneA, "Rig");
      await settingsFirmwarePage.validateLaneRolloutInProgress(laneA, firmwareVersion);
      await settingsFirmwarePage.toggleModelGroup(laneA, "Rig");
    });

    await test.step("Rollout completes and both miners report the assigned version", async () => {
      await settingsFirmwarePage.waitForLaneRolloutCompleted(laneA, firmwareVersion, testConfig.testTimeout * 4);
    });

    await test.step("Collapsing the lane hides its model groups", async () => {
      await settingsFirmwarePage.toggleLane(laneA);
      await settingsFirmwarePage.validateLaneCollapsed(laneA, "Rig");
      await settingsFirmwarePage.toggleLane(laneA);
    });

    await test.step("Moving a miner to a second lane removes it from the first", async () => {
      const [minerToMove] = await settingsFirmwarePage.getLaneMinerNames(laneA);
      await settingsFirmwarePage.createLane(laneB);
      await settingsFirmwarePage.openManageLaneMiners(laneB);
      await settingsFirmwarePage.selectLaneMinerByName(minerToMove);
      await settingsFirmwarePage.confirmLaneMinerSelection();
      await settingsFirmwarePage.validateLaneMinerCount(laneB, 1);
      await settingsFirmwarePage.validateLaneMinerCount(laneA, 1);
    });

    await test.step("Delete both lanes", async () => {
      await settingsFirmwarePage.deleteLane(laneB);
      await settingsFirmwarePage.deleteLane(laneA);
    });
  });
});
