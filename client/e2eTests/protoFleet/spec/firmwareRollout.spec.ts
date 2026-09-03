import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";

test.describe("Firmware release channels", () => {
  const runId = Date.now();
  const channelA = `Channel A ${runId}`;
  const channelB = `Channel B ${runId}`;
  const channelC = `Channel C ${runId}`;
  const channelD = `Channel D ${runId}`;
  const channelE = `Channel E ${runId}`;
  const channelF = `Channel F ${runId}`;
  // Unique per run: the fake rigs keep their firmware across runs, and a
  // version the miners already report would make the assignment a no-op
  // (no update is created when every member is already compliant).
  const firmwareVersion = `3.1.${runId}`;
  const firmwareFileName = `rollout-${firmwareVersion}.swu`;
  const rollbackVersionV1 = `3.2.${runId}`;
  const rollbackFileNameV1 = `rollback-${rollbackVersionV1}.swu`;
  const rollbackVersionV2 = `3.3.${runId}`;
  const rollbackFileNameV2 = `rollback-${rollbackVersionV2}.swu`;
  const pilotVersion = `3.4.${runId}`;
  const pilotFileName = `pilot-${pilotVersion}.swu`;
  const batchesVersion = `3.5.${runId}`;
  const batchesFileName = `batches-${batchesVersion}.swu`;
  const cancelBaseVersion = `3.6.${runId}`;
  const cancelBaseFileName = `cancel-base-${cancelBaseVersion}.swu`;
  const cancelTargetVersion = `3.7.${runId}`;
  const cancelTargetFileName = `cancel-target-${cancelTargetVersion}.swu`;

  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(testConfig.target === "real", "Release channel E2E is only supported against the fake proto rig setup.");

  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test.afterEach("CLEANUP: delete channels and uploaded firmware files", async ({ browser }, testInfo) => {
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
      await settingsFirmwarePage.openReleaseChannelsTab();
      for (const name of [channelA, channelB, channelC, channelD, channelE, channelF]) {
        await settingsFirmwarePage.deleteChannelIfPresent(name);
      }
      await settingsFirmwarePage.openFilesTab();
      await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
    } finally {
      await context.close();
    }
  });

  async function uploadRigFirmware(
    settingsFirmwarePage: SettingsFirmwarePage,
    files: { fileName: string; version: string }[],
  ) {
    await settingsFirmwarePage.navigateToFirmwareSettings();
    await settingsFirmwarePage.validateFirmwarePageOpened();
    await settingsFirmwarePage.deleteAllFirmwareFilesIfAny();
    for (const { fileName, version } of files) {
      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(fileName, `payload ${version} ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion: version,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(fileName);
    }
  }

  // Creates a channel over `minerCount` Rig miners with the behavior the
  // callback configures, and lands in its manage view.
  async function createRigChannel(
    settingsFirmwarePage: SettingsFirmwarePage,
    name: string,
    minerCount: number,
    configureBehavior?: () => Promise<void>,
  ) {
    await settingsFirmwarePage.openReleaseChannelsTab();
    await settingsFirmwarePage.startCreateChannel(name);
    await settingsFirmwarePage.openScopeMiners();
    await settingsFirmwarePage.selectScopeMinersByModel("Rig", minerCount);
    await settingsFirmwarePage.confirmScopeMinerSelection();
    await settingsFirmwarePage.validateScopeCovers(minerCount);
    await configureBehavior?.();
    await settingsFirmwarePage.saveNewChannel(name);
    await settingsFirmwarePage.validateChannelViewMinerCount(name, minerCount);
  }

  test("Create a channel, enforce firmware per model, and keep miners exclusive to one channel", async ({
    settingsFirmwarePage,
  }) => {
    test.setTimeout(testConfig.testTimeout * 6);

    await test.step("Upload a Proto Rig firmware file", async () => {
      await uploadRigFirmware(settingsFirmwarePage, [{ fileName: firmwareFileName, version: firmwareVersion }]);
    });

    await test.step("Create a channel over two Rig miners", async () => {
      await createRigChannel(settingsFirmwarePage, channelA, 2);
    });

    await test.step("Assign firmware to the Rig model group and apply", async () => {
      await settingsFirmwarePage.selectChannelFirmware(channelA, "Rig", new RegExp(firmwareVersion));
      await settingsFirmwarePage.applyFirmwareChanges(channelA);
    });

    await test.step("Manage view and app header signal the ongoing update", async () => {
      await settingsFirmwarePage.validateChannelUpdatePill(channelA);
      await settingsFirmwarePage.validateAppRolloutPill();
    });

    await test.step("Header pill deep-links to the release channels view", async () => {
      await settingsFirmwarePage.openFilesTab();
      await settingsFirmwarePage.followAppRolloutPillToChannels();
      await settingsFirmwarePage.validateActiveUpdateRow(channelA, "Rig");
      await settingsFirmwarePage.validateChannelActiveUpdates(channelA, 1);
    });

    await test.step("Update detail modal and per-model row report the update", async () => {
      await settingsFirmwarePage.openActiveUpdate(channelA, "Rig");
      await settingsFirmwarePage.closeUpdateDetail();
      await settingsFirmwarePage.expandChannel(channelA);
      await settingsFirmwarePage.validateModelRowUpdating(channelA, "Rig");
      await settingsFirmwarePage.manageChannel(channelA);
      await settingsFirmwarePage.validateChannelUpdatePill(channelA);
    });

    await test.step("View miners modal lists the model's miners; progress stays in the view", async () => {
      await settingsFirmwarePage.validateModelMinersCount(channelA, "Rig", 2);
      await settingsFirmwarePage.validateChannelUpdateInProgress(channelA, firmwareVersion);
    });

    await test.step("Update completes and both miners report the assigned version", async () => {
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelA,
        "Rig",
        firmwareVersion,
        testConfig.testTimeout * 4,
      );
    });

    let sharedMiner = "";

    await test.step("A second channel cannot claim a miner already in the first", async () => {
      [sharedMiner] = await settingsFirmwarePage.getChannelMinerNames(channelA, "Rig");
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.startCreateChannel(channelB);
      await settingsFirmwarePage.openScopeMiners();
      await settingsFirmwarePage.toggleScopeMinerByName(sharedMiner);
      await settingsFirmwarePage.confirmScopeMinerSelection();
      await settingsFirmwarePage.validateScopeConflict(channelA);
      await settingsFirmwarePage.backToChannels();
    });

    await test.step("Removing the miner from the first channel frees it for the second", async () => {
      await settingsFirmwarePage.manageChannel(channelA);
      await settingsFirmwarePage.openScopeMiners();
      await settingsFirmwarePage.toggleScopeMinerByName(sharedMiner);
      await settingsFirmwarePage.confirmScopeMinerSelection();
      await settingsFirmwarePage.validateScopeCovers(1);
      await settingsFirmwarePage.saveChannelChanges();
      await settingsFirmwarePage.validateChannelViewMinerCount(channelA, 1);
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.startCreateChannel(channelB);
      await settingsFirmwarePage.openScopeMiners();
      await settingsFirmwarePage.toggleScopeMinerByName(sharedMiner);
      await settingsFirmwarePage.confirmScopeMinerSelection();
      await settingsFirmwarePage.validateScopeCovers(1);
      await settingsFirmwarePage.saveNewChannel(channelB);
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.validateChannelMinerCount(channelA, 1);
      await settingsFirmwarePage.validateChannelMinerCount(channelB, 1);
    });

    await test.step("Delete both channels", async () => {
      await settingsFirmwarePage.manageChannel(channelB);
      await settingsFirmwarePage.deleteChannel(channelB);
      await settingsFirmwarePage.manageChannel(channelA);
      await settingsFirmwarePage.deleteChannel(channelA);
    });
  });

  test("Roll back a model's firmware to the previously assigned version", async ({ settingsFirmwarePage }) => {
    // Three sequential updates have to complete: v1, v2, and the rollback
    // to v1 again.
    test.setTimeout(testConfig.testTimeout * 12);

    await test.step("Upload two Proto Rig firmware versions", async () => {
      await uploadRigFirmware(settingsFirmwarePage, [
        { fileName: rollbackFileNameV1, version: rollbackVersionV1 },
        { fileName: rollbackFileNameV2, version: rollbackVersionV2 },
      ]);
    });

    await test.step("Create a channel with one Rig miner", async () => {
      await createRigChannel(settingsFirmwarePage, channelC, 1);
    });

    await test.step("Roll out the first version", async () => {
      await settingsFirmwarePage.selectChannelFirmware(channelC, "Rig", new RegExp(rollbackVersionV1));
      await settingsFirmwarePage.applyFirmwareChanges(channelC);
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelC,
        "Rig",
        rollbackVersionV1,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Roll out the second version; the first's history entry offers rollback", async () => {
      await settingsFirmwarePage.selectChannelFirmware(channelC, "Rig", new RegExp(rollbackVersionV2));
      await settingsFirmwarePage.applyFirmwareChanges(channelC);
      await settingsFirmwarePage.validateHistoryRollbackAvailable(channelC, rollbackVersionV1);
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelC,
        "Rig",
        rollbackVersionV2,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Roll back from history and wait for the rollback update to complete", async () => {
      await settingsFirmwarePage.rollbackChannelFirmware(channelC, rollbackVersionV1);
      await settingsFirmwarePage.validateChannelUpdateInProgress(channelC, rollbackVersionV1);
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelC,
        "Rig",
        rollbackVersionV1,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("The replaced version's history entry becomes the rollback target (roll forward)", async () => {
      // v2 had finished before the rollback, so its entry stays Completed;
      // it now offers a roll forward instead.
      await settingsFirmwarePage.validateHistoryOutcome(channelC, rollbackVersionV2, "Completed");
      await settingsFirmwarePage.validateHistoryRollbackAvailable(channelC, rollbackVersionV2);
    });

    await test.step("Delete the channel", async () => {
      await settingsFirmwarePage.deleteChannel(channelC);
    });
  });

  test("Pilot update updates one miner, gates the rest, and continues after review", async ({
    settingsFirmwarePage,
  }) => {
    // Two update waves have to complete: the pilot miner and, after the
    // gate is released, the remaining miner.
    test.setTimeout(testConfig.testTimeout * 8);

    await test.step("Upload a Proto Rig firmware file", async () => {
      await uploadRigFirmware(settingsFirmwarePage, [{ fileName: pilotFileName, version: pilotVersion }]);
    });

    await test.step("Create a pilot-then-remaining channel with two Rig miners", async () => {
      await createRigChannel(settingsFirmwarePage, channelD, 2, async () => {
        await settingsFirmwarePage.setMethod("Pilot batch, then remaining");
        await settingsFirmwarePage.setPilotSize(1);
      });
    });

    await test.step("Apply the firmware", async () => {
      await settingsFirmwarePage.selectChannelFirmware(channelD, "Rig", new RegExp(pilotVersion));
      await settingsFirmwarePage.applyFirmwareChanges(channelD);
    });

    await test.step("The pilot completes and parks at the review gate", async () => {
      await settingsFirmwarePage.waitForModelReviewNeeded(channelD, testConfig.testTimeout * 4);
    });

    await test.step("Exactly one miner is on the new version while gated", async () => {
      const updated = await settingsFirmwarePage.countModelMinersOnVersion(channelD, "Rig", pilotVersion);
      test.expect(updated).toBe(1);
    });

    await test.step("The channels table, active updates and header pill flag the review", async () => {
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.validateChannelNeedsAttention(channelD);
      await settingsFirmwarePage.validateActiveUpdateRow(channelD, "Rig");
      await settingsFirmwarePage.validateAppRolloutPillNeedsAttention();
    });

    await test.step("The review gate shows evidence and can be paused and resumed", async () => {
      await settingsFirmwarePage.openActiveUpdate(channelD, "Rig");
      await settingsFirmwarePage.validateDetailHeadline("Pilot batch review");
      await settingsFirmwarePage.validateEvidenceVisible();
      await settingsFirmwarePage.validateDetailMinersCount(2);
      await settingsFirmwarePage.pauseAndResumeFromDetail();
    });

    await test.step("Continue the update from the detail modal", async () => {
      await settingsFirmwarePage.continueFromDetail();
    });

    await test.step("The remaining miner converges on the new version", async () => {
      await settingsFirmwarePage.manageChannel(channelD);
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelD,
        "Rig",
        pilotVersion,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Delete the channel", async () => {
      await settingsFirmwarePage.deleteChannel(channelD);
    });
  });

  test("Batches with auto-continue complete without a manual review", async ({ settingsFirmwarePage }) => {
    // Two batches of one miner each, each gate released automatically.
    test.setTimeout(testConfig.testTimeout * 8);

    await test.step("Upload a Proto Rig firmware file", async () => {
      await uploadRigFirmware(settingsFirmwarePage, [{ fileName: batchesFileName, version: batchesVersion }]);
    });

    await test.step("Create a batched channel with automatic gates over two Rig miners", async () => {
      await createRigChannel(settingsFirmwarePage, channelE, 2, async () => {
        await settingsFirmwarePage.setMethod("Multiple batches");
        await settingsFirmwarePage.setBatchSize(1);
        await settingsFirmwarePage.enableReviewAfterEachBatch();
        await settingsFirmwarePage.enableAutoContinue({ maxHashrateDropPercent: 50 });
      });
    });

    await test.step("Apply the firmware", async () => {
      await settingsFirmwarePage.selectChannelFirmware(channelE, "Rig", new RegExp(batchesVersion));
      await settingsFirmwarePage.applyFirmwareChanges(channelE);
      await settingsFirmwarePage.validateChannelUpdateInProgress(channelE, batchesVersion);
    });

    await test.step("Both batches complete with nobody clicking Continue", async () => {
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelE,
        "Rig",
        batchesVersion,
        testConfig.testTimeout * 6,
      );
    });

    await test.step("Delete the channel", async () => {
      await settingsFirmwarePage.deleteChannel(channelE);
    });
  });

  test("Canceling the remaining update keeps updated miners, and rolling back restores the base version", async ({
    settingsFirmwarePage,
  }) => {
    // Three update waves: the base version, the pilot of the new version,
    // and the rollback to the base version after the cancel.
    test.setTimeout(testConfig.testTimeout * 12);

    await test.step("Upload two Proto Rig firmware versions", async () => {
      await uploadRigFirmware(settingsFirmwarePage, [
        { fileName: cancelBaseFileName, version: cancelBaseVersion },
        { fileName: cancelTargetFileName, version: cancelTargetVersion },
      ]);
    });

    await test.step("Create a pilot channel with two Rig miners on the base version", async () => {
      await createRigChannel(settingsFirmwarePage, channelF, 2, async () => {
        await settingsFirmwarePage.setMethod("Pilot batch, then remaining");
        await settingsFirmwarePage.setPilotSize(1);
      });
      await settingsFirmwarePage.selectChannelFirmware(channelF, "Rig", new RegExp(cancelBaseVersion));
      await settingsFirmwarePage.applyFirmwareChanges(channelF);
      // The pilot on the base version also gates; continue it.
      await settingsFirmwarePage.waitForModelReviewNeeded(channelF, testConfig.testTimeout * 4);
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.openActiveUpdate(channelF, "Rig");
      await settingsFirmwarePage.continueFromDetail();
      await settingsFirmwarePage.manageChannel(channelF);
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelF,
        "Rig",
        cancelBaseVersion,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Pilot the new version until it parks at the review gate", async () => {
      await settingsFirmwarePage.selectChannelFirmware(channelF, "Rig", new RegExp(cancelTargetVersion));
      await settingsFirmwarePage.applyFirmwareChanges(channelF);
      await settingsFirmwarePage.waitForModelReviewNeeded(channelF, testConfig.testTimeout * 4);
    });

    await test.step("Cancel the remaining update from the review gate", async () => {
      await settingsFirmwarePage.backToChannels();
      await settingsFirmwarePage.openActiveUpdate(channelF, "Rig");
      await settingsFirmwarePage.cancelRemainingFromDetail();
    });

    await test.step("The pilot miner keeps the new version; the assignment stays; nothing restarts", async () => {
      await settingsFirmwarePage.manageChannel(channelF);
      await settingsFirmwarePage.validateModelAssignedFirmware(channelF, "Rig", new RegExp(cancelTargetVersion));
      await settingsFirmwarePage.validateHistoryOutcome(channelF, cancelTargetVersion, "Canceled");
      const updated = await settingsFirmwarePage.countModelMinersOnVersion(channelF, "Rig", cancelTargetVersion);
      test.expect(updated).toBe(1);
    });

    await test.step("Rolling back restores the base version on the pilot miner", async () => {
      await settingsFirmwarePage.rollbackChannelFirmware(channelF, cancelBaseVersion);
      await settingsFirmwarePage.validateModelAssignedFirmware(channelF, "Rig", new RegExp(cancelBaseVersion));
      await settingsFirmwarePage.waitForChannelUpdateCompleted(
        channelF,
        "Rig",
        cancelBaseVersion,
        testConfig.testTimeout * 4,
      );
    });

    await test.step("Delete the channel", async () => {
      await settingsFirmwarePage.deleteChannel(channelF);
    });
  });
});
