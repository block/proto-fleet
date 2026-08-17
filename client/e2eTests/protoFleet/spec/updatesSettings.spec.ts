import { test } from "../fixtures/pageFixtures";
import {
  buildReleaseInfo,
  buildUpdateStatus,
  buildUpgradeOperation,
  mockUpdatesSettings,
} from "../helpers/updatesSettingsMocks";
import { ReleaseChannel, UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";

test.describe("Proto Fleet - Updates Settings", () => {
  test(
    "View update details and refresh the offer after enabling release candidates",
    { tag: "@smoke" },
    async ({ page, settingsUpdatesPage }) => {
      const updatesMock = await mockUpdatesSettings(page, {
        initialStatus: buildUpdateStatus(),
        onSetReleaseChannel: (channel) =>
          buildUpdateStatus({
            channel,
            installCommand: "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.4.0-rc.1",
            latestEligible: buildReleaseInfo({
              version: "v1.4.0-rc.1",
              releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
              prerelease: true,
            }),
          }),
      });

      await test.step("Open Updates settings and validate the initial stable release offer", async () => {
        await settingsUpdatesPage.navigateToUpdatesSettings();
        await settingsUpdatesPage.validateUpdatesPageOpened();
        await settingsUpdatesPage.validateCurrentVersion("v1.2.0");
        await settingsUpdatesPage.validateLatestAvailableVersion("v1.3.0");
        await settingsUpdatesPage.validateReleaseNotesLink("https://github.com/block/proto-fleet/releases/tag/v1.3.0");
        await settingsUpdatesPage.validateInstallCommand(
          "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.3.0",
        );
        await settingsUpdatesPage.validateCopyInstallCommandEnabled();
        await settingsUpdatesPage.validateIncludeReleaseCandidatesUnchecked();
      });

      await test.step("Enable release candidates and validate the refreshed offer", async () => {
        await settingsUpdatesPage.clickIncludeReleaseCandidates();
        await settingsUpdatesPage.validateTextInToast("Release channel updated");
        test.expect(updatesMock.releaseChannelRequests).toEqual([ReleaseChannel.STABLE_AND_RC]);
        await settingsUpdatesPage.validateIncludeReleaseCandidatesChecked();
        await settingsUpdatesPage.validateLatestAvailableVersion("v1.4.0-rc.1");
        await settingsUpdatesPage.validateReleaseNotesLink(
          "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
        );
        await settingsUpdatesPage.validateInstallCommand(
          "curl -fsSL https://fleet.example.com/install.sh | sh -s -- v1.4.0-rc.1",
        );
      });
    },
  );

  test("Confirm the exact offered version before triggering a one-click upgrade", async ({
    page,
    settingsUpdatesPage,
  }) => {
    const updatesMock = await mockUpdatesSettings(page, {
      initialStatus: buildUpdateStatus({ oneClickAvailable: true }),
      onTriggerUpgrade: (targetVersion) =>
        buildUpgradeOperation(UpgradePhase.QUEUED, {
          targetVersion,
          message: `Preparing ${targetVersion}`,
        }),
    });

    await settingsUpdatesPage.navigateToUpdatesSettings();
    await settingsUpdatesPage.validateUpdatesPageOpened();

    await test.step("Open the upgrade confirmation modal without triggering the request yet", async () => {
      await settingsUpdatesPage.clickUpgradeToVersion("v1.3.0");
      await settingsUpdatesPage.validateUpgradeConfirmationOpened("v1.3.0");
      test.expect(updatesMock.triggerUpgradeRequests).toEqual([]);
    });

    await test.step("Confirm the upgrade and validate the exact requested version", async () => {
      await settingsUpdatesPage.confirmUpgradeToVersion("v1.3.0");
      await settingsUpdatesPage.validateUpgradeModalText("Preparing v1.3.0");
      await settingsUpdatesPage.validateCopyInstallCommandDisabled();
      test.expect(updatesMock.triggerUpgradeRequests).toEqual(["v1.3.0"]);
    });
  });
});
