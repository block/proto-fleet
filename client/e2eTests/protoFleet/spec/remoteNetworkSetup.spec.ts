import { expect, test } from "../fixtures/pageFixtures";
import { mockRemoteNetworkSetup } from "../helpers/remoteNetworkSetupMocks";

test.describe("Remote-network prerequisites", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("explains setup before discovery and leads to installation and enrollment", async ({
    context,
    addMinersPage,
    settingsNodesPage,
  }) => {
    const mock = await mockRemoteNetworkSetup(context);

    await test.step("Keep the landing unchanged", async () => {
      await addMinersPage.openOnboarding();
      await expect(addMinersPage.getGetStartedButton()).toBeVisible();
      await expect(addMinersPage.getRemoteNetworkPrerequisite()).toHaveCount(0);
    });

    await test.step("Show one concise notice above discovery options", async () => {
      await addMinersPage.startOnboarding();
      await addMinersPage.validateTextIsVisible(
        "Scan your network or provide miner IP addresses and hostnames to find miners to add to your fleet.",
      );
      await addMinersPage.validateTextIsVisible(
        "Note that you can add more miners and adjust security settings after setup.",
      );
      const prerequisite = addMinersPage.getRemoteNetworkPrerequisite();
      await expect(prerequisite).toHaveCount(1);
      await expect(prerequisite).toContainText("No Fleet Nodes set up");
      await expect(prerequisite).toContainText(
        "To find miners on a network Fleet can't reach directly, install and enroll Fleet Node",
      );
      await expect(prerequisite).toBeVisible();
      await expect(addMinersPage.getScanNetworkSection().getByRole("button", { name: "Find miners" })).toBeEnabled();
      await expect(addMinersPage.getSearchByIpSection()).toBeVisible();
    });

    await test.step("Open Settings and show installation before the enrollment command", async () => {
      await addMinersPage.openFleetNodeSettings();
      await settingsNodesPage.validateNodesPageOpened();
      await settingsNodesPage.clickEnrollNode();
      await settingsNodesPage.validateEnrollNodeModalOpened();
      await settingsNodesPage.continueEnrollment();
      await settingsNodesPage.validateInstallationPrerequisite();
    });

    expect(mock.unexpected).toEqual([]);
  });
});
