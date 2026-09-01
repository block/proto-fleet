import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";

test.describe("Firmware release channels", () => {
  const runId = Date.now();
  const laneA = `Lane A ${runId}`;
  const laneB = `Lane B ${runId}`;
  const laneC = `Lane C ${runId}`;
  const laneD = `Lane D ${runId}`;
  // Unique per run: the fake rigs keep their firmware across runs, and a
  // version the miners already report would make the assignment a no-op
  // (no rollout is created when every member is already compliant).
  const firmwareVersion = `3.1.${runId}`;
  const firmwareFileName = `rollout-${firmwareVersion}.swu`;
  const rollbackVersionV1 = `3.2.${runId}`;
  const rollbackFileNameV1 = `rollback-${rollbackVersionV1}.swu`;
  const rollbackVersionV2 = `3.3.${runId}`;
  const rollbackFileNameV2 = `rollback-${rollbackVersionV2}.swu`;
  const pilotVersion = `3.4.${runId}`;
  const pilotFileName = `pilot-${pilotVersion}.swu`;

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
      await settingsFirmwarePage.deleteLaneIfPresent(laneC);
      await settingsFirmwarePage.deleteLaneIfPresent(laneD);
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

    await test.step("Create an empty channel", async () => {
      await settingsFirmwarePage.openRolloutLanesTab();
      await settingsFirmwarePage.createLane(laneA);
      await settingsFirmwarePage.validateChannelMinerCount(laneA, 0);
    });

    await test.step("Add two Rig miners to the channel", async () => {
      await settingsFirmwarePage.manageChannel(laneA);
      await settingsFirmwarePage.openManageLaneMiners(laneA);
      await settingsFirmwarePage.selectLaneMinersByModel("Rig", 2);
      await settingsFirmwarePage.confirmLaneMinerSelection();
      await settingsFirmwarePage.validateLaneMinerCount(laneA, 2);
    });

    await test.step("Assign firmware to the Rig model group and apply", async () => {
      await settingsFirmwarePage.selectLaneFirmware(laneA, "Rig", new RegExp(firmwareVersion));
      await settingsFirmwarePage.applyLaneFirmwareChanges(laneA);
    });

    await test.step("Channel card and app header signal the ongoing rollout", async () => {
      await settingsFirmwarePage.validateLaneRolloutPill(laneA);
      await settingsFirmwarePage.validateAppRolloutPill();
    });

    await test.step("Header pill deep-links to the release channels view", async () => {
      await settingsFirmwarePage.openFilesTab();
      await settingsFirmwarePage.followAppRolloutPillToLanes();
      await settingsFirmwarePage.validateActiveUpdateRow(laneA, "Rig");
      await settingsFirmwarePage.validateChannelActiveUpdates(laneA, 1);
    });

    await test.step("Update detail modal and per-model row report the update", async () => {
      await settingsFirmwarePage.openUpdateDetail(laneA, "Rig");
      await settingsFirmwarePage.closeUpdateDetail();
      await settingsFirmwarePage.expandChannel(laneA);
      await settingsFirmwarePage.validateModelRowUpdating(laneA, "Rig");
      await settingsFirmwarePage.manageChannel(laneA);
      await settingsFirmwarePage.validateLaneRolloutPill(laneA);
    });

    await test.step("View miners modal lists the model's miners; progress stays in the card", async () => {
      await settingsFirmwarePage.validateModelMinersCount(laneA, "Rig", 2);
      await settingsFirmwarePage.validateLaneRolloutInProgress(laneA, firmwareVersion);
    });

    await test.step("Rollout completes and both miners report the assigned version", async () => {
      await settingsFirmwarePage.waitForLaneRolloutCompleted(laneA, "Rig", firmwareVersion, testConfig.testTimeout * 4);
    });

    await test.step("Moving a miner to a second channel removes it from the first", async () => {
      const [minerToMove] = await settingsFirmwarePage.getLaneMinerNames(laneA, "Rig");
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.createLane(laneB);
      await settingsFirmwarePage.manageChannel(laneB);
      await settingsFirmwarePage.openManageLaneMiners(laneB);
      await settingsFirmwarePage.selectLaneMinerByName(minerToMove);
      await settingsFirmwarePage.confirmLaneMinerSelection();
      await settingsFirmwarePage.validateLaneMinerCount(laneB, 1);
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.validateChannelMinerCount(laneA, 1);
      await settingsFirmwarePage.validateChannelMinerCount(laneB, 1);
    });

    await test.step("Delete both channels", async () => {
      await settingsFirmwarePage.manageChannel(laneB);
      await settingsFirmwarePage.deleteLane(laneB);
      await settingsFirmwarePage.manageChannel(laneA);
      await settingsFirmwarePage.deleteLane(laneA);
    });
  });

  test("Roll back a model's firmware to the previously assigned version", async ({ settingsFirmwarePage }) => {
    // Three sequential rollouts have to complete: v1, v2, and the rollback
    // to v1 again.
    test.setTimeout(testConfig.testTimeout * 12);

    await test.step("Upload two Proto Rig firmware versions", async () => {
      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.validateFirmwarePageOpened();
      await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(rollbackFileNameV1, `rollback payload v1 ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion: rollbackVersionV1,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(rollbackFileNameV1);
      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(rollbackFileNameV2, `rollback payload v2 ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion: rollbackVersionV2,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(rollbackFileNameV2);
    });

    await test.step("Create a channel with one Rig miner", async () => {
      await settingsFirmwarePage.openRolloutLanesTab();
      await settingsFirmwarePage.createLane(laneC);
      await settingsFirmwarePage.manageChannel(laneC);
      await settingsFirmwarePage.openManageLaneMiners(laneC);
      await settingsFirmwarePage.selectLaneMinersByModel("Rig", 1);
      await settingsFirmwarePage.confirmLaneMinerSelection();
      await settingsFirmwarePage.validateLaneMinerCount(laneC, 1);
    });

    await test.step("Roll out the first version", async () => {
      await settingsFirmwarePage.selectLaneFirmware(laneC, "Rig", new RegExp(rollbackVersionV1));
      await settingsFirmwarePage.applyLaneFirmwareChanges(laneC);
      await settingsFirmwarePage.waitForLaneRolloutCompleted(
        laneC,
        "Rig",
        rollbackVersionV1,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Roll out the second version; the first's history entry offers rollback", async () => {
      await settingsFirmwarePage.selectLaneFirmware(laneC, "Rig", new RegExp(rollbackVersionV2));
      await settingsFirmwarePage.applyLaneFirmwareChanges(laneC);
      await settingsFirmwarePage.validateHistoryRollbackAvailable(laneC, rollbackVersionV1);
      await settingsFirmwarePage.waitForLaneRolloutCompleted(
        laneC,
        "Rig",
        rollbackVersionV2,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Roll back from history and wait for the rollback rollout to complete", async () => {
      await settingsFirmwarePage.rollbackLaneFirmware(laneC, rollbackVersionV1);
      await settingsFirmwarePage.validateLaneRolloutInProgress(laneC, rollbackVersionV1);
      await settingsFirmwarePage.waitForLaneRolloutCompleted(
        laneC,
        "Rig",
        rollbackVersionV1,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("The replaced version's history entry becomes the rollback target (roll forward)", async () => {
      await settingsFirmwarePage.validateHistoryRollbackAvailable(laneC, rollbackVersionV2);
    });

    await test.step("Delete the channel", async () => {
      await settingsFirmwarePage.deleteLane(laneC);
    });
  });

  test("Pilot rollout updates one miner, gates the rest, and continues after review", async ({
    settingsFirmwarePage,
  }) => {
    // Two update waves have to complete: the pilot miner and, after the
    // gate is released, the remaining miner.
    test.setTimeout(testConfig.testTimeout * 8);

    await test.step("Upload a Proto Rig firmware file", async () => {
      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.validateFirmwarePageOpened();
      await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(pilotFileName, `pilot payload ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion: pilotVersion,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(pilotFileName);
    });

    await test.step("Create a channel with two Rig miners", async () => {
      await settingsFirmwarePage.openRolloutLanesTab();
      await settingsFirmwarePage.createLane(laneD);
      await settingsFirmwarePage.manageChannel(laneD);
      await settingsFirmwarePage.openManageLaneMiners(laneD);
      await settingsFirmwarePage.selectLaneMinersByModel("Rig", 2);
      await settingsFirmwarePage.confirmLaneMinerSelection();
      await settingsFirmwarePage.validateLaneMinerCount(laneD, 2);
    });

    await test.step("Apply the firmware as a pilot of one miner", async () => {
      await settingsFirmwarePage.selectLaneFirmware(laneD, "Rig", new RegExp(pilotVersion));
      await settingsFirmwarePage.applyLaneFirmwareChangesWithPilot(laneD, 1);
    });

    await test.step("The pilot completes and parks at the review gate", async () => {
      await settingsFirmwarePage.waitForModelReviewNeeded(laneD, testConfig.testTimeout * 4);
    });

    await test.step("Exactly one miner is on the new version while gated", async () => {
      const updated = await settingsFirmwarePage.countModelMinersOnVersion(laneD, "Rig", pilotVersion);
      test.expect(updated).toBe(1);
    });

    await test.step("The channels table and active updates flag the review", async () => {
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.validateChannelReviewNeeded(laneD);
      await settingsFirmwarePage.validateActiveUpdateRow(laneD, "Rig");
    });

    await test.step("Continue the rollout from the update detail modal", async () => {
      await settingsFirmwarePage.continueRolloutFromActiveUpdates(laneD, "Rig");
    });

    await test.step("The remaining miner converges on the new version", async () => {
      await settingsFirmwarePage.manageChannel(laneD);
      await settingsFirmwarePage.waitForLaneRolloutCompleted(laneD, "Rig", pilotVersion, testConfig.testTimeout * 4);
    });

    await test.step("Delete the channel", async () => {
      await settingsFirmwarePage.deleteLane(laneD);
    });
  });
});
