import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Between-channel firmware rollout", () => {
  let rolloutId: string | undefined;
  let laneDeviceIdentifiers: string[] = [];

  test.beforeEach(async ({ page, commonSteps }) => {
    rolloutId = undefined;
    laneDeviceIdentifiers = [];
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  // Cleanup restores mutable rollout authority and miner membership only. Lane/release
  // audit history and referenced artifacts remain immutable; unique run IDs and the
  // isolated E2E database lifecycle bound that retained data.
  test.afterEach(
    "CLEANUP: Restore rollout firmware and clear channel membership",
    async ({ page, settingsFirmwarePage }) => {
      if (!rolloutId && laneDeviceIdentifiers.length === 0) {
        return;
      }

      await page.goto("/");
      await settingsFirmwarePage.cleanupRollout(rolloutId, laneDeviceIdentifiers);
      rolloutId = undefined;
      laneDeviceIdentifiers = [];
    },
  );

  test("opens the stable rollout lane surface", async ({ settingsFirmwarePage }) => {
    await settingsFirmwarePage.navigateToFirmwareSettings();
    await settingsFirmwarePage.validateFirmwarePageOpened();
    await settingsFirmwarePage.openRolloutLanesTab();
    await settingsFirmwarePage.validateRolloutLanesSurface();
  });

  test("creates, completes, reopens, aborts, and reverts a two-batch rollout", async ({
    minersPage,
    settingsFirmwarePage,
  }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(testConfig.target === "real", "Firmware rollout E2E is supported only by the fake Proto rig setup.");
    test.setTimeout(testConfig.testTimeout * 12);

    const runId = `${Date.now()}`;
    const sourceVersion = "1.8.0";
    const targetVersion = "1.8.1";
    const sourceFileName = `rollout-source-${sourceVersion}-${runId}.swu`;
    const targetFileName = `rollout-target-${targetVersion}-${runId}.swu`;
    const laneLabel = `firmware-rollout-e2e-${runId}`;
    const firstRolloutName = `firmware-rollout-complete-${runId}`;
    const abortRolloutName = `firmware-rollout-abort-${runId}`;

    await test.step("Upload source and target firmware releases", async () => {
      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.validateFirmwarePageOpened();

      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(sourceFileName, `source firmware ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion: sourceVersion,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(sourceFileName);

      await settingsFirmwarePage.clickUploadFirmware();
      await settingsFirmwarePage.uploadFirmwareFile(targetFileName, `target firmware ${runId}`, {
        manufacturer: "Proto",
        model: "Rig",
        firmwareVersion: targetVersion,
      });
      await settingsFirmwarePage.validateFirmwareFileVisible(targetFileName);
    });

    let minerIpAddresses: string[] = [];

    await test.step("Choose two hashing Proto rigs", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.waitForMinersListToLoad();
      await minersPage.filterRigMiners();
      minerIpAddresses = await minersPage.getSelectableProtoRigIpAddresses(2);
    });

    await test.step("Create a stable lane with an initial release and membership", async () => {
      await settingsFirmwarePage.navigateToFirmwareSettings();
      await settingsFirmwarePage.openRolloutLanesTab();
      const createRequest = await settingsFirmwarePage.createRolloutLane({
        label: laneLabel,
        description: "Repeatable operator evaluation lane",
        sourceFirmwareFileName: sourceFileName,
        minerIpAddresses,
        onLaneCreated: (deviceIdentifiers) => {
          laneDeviceIdentifiers = deviceIdentifiers;
        },
      });
      laneDeviceIdentifiers = createRequest.deviceIdentifiers ?? [];
      test.expect(laneDeviceIdentifiers).toHaveLength(2);
      await settingsFirmwarePage.validateRolloutLane(laneLabel, `Rig ${sourceVersion}`, 2);
    });

    await test.step("Select the target release and start two manual batches", async () => {
      const rollout = await settingsFirmwarePage.startTwoBatchRollout({
        laneLabel,
        rolloutName: firstRolloutName,
        reason: "Evaluate confirmed membership semantics",
        targetRelease: {
          manufacturer: "Proto",
          model: "Rig",
          firmwareVersion: targetVersion,
          fileName: targetFileName,
        },
        onRolloutCreated: (createdRolloutId) => {
          rolloutId = createdRolloutId;
        },
      });
      rolloutId = rollout.rolloutId;
      await settingsFirmwarePage.validateMembershipSplit(2, 0);
    });

    await test.step("Confirm the first batch, reopen durable state, and continue", async () => {
      await settingsFirmwarePage.waitForMembershipSplit(1, 1);
      await settingsFirmwarePage.waitForRolloutStage("Review");
      await settingsFirmwarePage.reloadAndReopenRollout(laneLabel, firstRolloutName);
      await settingsFirmwarePage.pauseAndResumeReview();
      await settingsFirmwarePage.continueRollout();
    });

    await test.step("Complete the final batch, reopen the target lane, and revert", async () => {
      await settingsFirmwarePage.waitForMembershipSplit(0, 2);
      await settingsFirmwarePage.waitForRolloutStage("Completed");
      await settingsFirmwarePage.reloadAndReopenRollout(laneLabel, firstRolloutName);
      await settingsFirmwarePage.validateRolloutLane(laneLabel, `Rig ${targetVersion}`, 2);
      await settingsFirmwarePage.revertRollout();
      await settingsFirmwarePage.waitForMembershipSplit(2, 0);
      await settingsFirmwarePage.reloadAndReopenRollout(laneLabel, firstRolloutName);
      await settingsFirmwarePage.validateRolloutLane(laneLabel, `Rig ${sourceVersion}`, 2);
    });

    await test.step("Start another rollout and abort after the first batch", async () => {
      const rollout = await settingsFirmwarePage.startTwoBatchRollout({
        laneLabel,
        rolloutName: abortRolloutName,
        reason: "Evaluate the no-new-work abort boundary",
        targetRelease: {
          manufacturer: "Proto",
          model: "Rig",
          firmwareVersion: targetVersion,
          fileName: targetFileName,
        },
        onRolloutCreated: (createdRolloutId) => {
          rolloutId = createdRolloutId;
        },
      });
      rolloutId = rollout.rolloutId;
      await settingsFirmwarePage.validateMembershipSplit(2, 0);
      await settingsFirmwarePage.waitForMembershipSplit(1, 1);
      await settingsFirmwarePage.abortRollout();
      await settingsFirmwarePage.waitForMembershipSplit(1, 1);
    });

    await test.step("Reload the split outcome and revert only transitioned miners", async () => {
      await settingsFirmwarePage.reloadAndReopenRollout(laneLabel, abortRolloutName);
      await settingsFirmwarePage.validateNoRetryAction();
      await settingsFirmwarePage.revertRollout();
      await settingsFirmwarePage.waitForMembershipSplit(2, 0);
      await settingsFirmwarePage.reloadAndReopenRollout(laneLabel, abortRolloutName);
      await settingsFirmwarePage.validateRolloutLane(laneLabel, `Rig ${sourceVersion}`, 2);

      await minersPage.navigateToMinersPage();
      await minersPage.waitForMinersListToLoad();
      await minersPage.filterRigMiners();
      for (const ipAddress of minerIpAddresses) {
        await minersPage.validateMinerValue(ipAddress, "firmware", sourceVersion);
      }
    });
  });

  // Fake Proto rigs resolve every accepted upload and expose no public post-upload
  // ambiguity control. Service and component tests cover attention-required no-retry behavior.
  // eslint-disable-next-line playwright/no-skipped-test -- fake rigs cannot produce post-upload ambiguity
  test.fixme("shows attention-required firmware with no retry action", async () => {});
});
