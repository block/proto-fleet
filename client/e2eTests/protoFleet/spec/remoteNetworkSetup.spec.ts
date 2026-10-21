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

    await test.step("Show prerequisites on the empty-fleet landing", async () => {
      await addMinersPage.openOnboarding();
      const prerequisite = addMinersPage.getRemoteNetworkPrerequisite();
      await expect(prerequisite).toContainText("For miners on a remote network, install and enroll a Fleet Node");
      await expect(prerequisite).toContainText("Fleet can discover directly on networks reachable from the server.");
      await expect(prerequisite).toContainText("No Fleet Nodes are set up for remote discovery.");
      await expect(prerequisite).toBeVisible();
    });

    await test.step("Keep one prerequisite explanation above discovery options", async () => {
      await addMinersPage.startOnboarding();
      await expect(addMinersPage.getRemoteNetworkPrerequisite()).toHaveCount(1);
      await expect(addMinersPage.getRemoteNetworkPrerequisite()).toBeVisible();
      await expect(addMinersPage.getScanNetworkSection().getByRole("button", { name: "Find miners" })).toBeEnabled();
      await expect(addMinersPage.getSearchByIpSection()).toBeVisible();
    });

    await test.step("Open Settings and show installation before the enrollment command", async () => {
      await addMinersPage.openFleetNodeSettings();
      await settingsNodesPage.validateNodesPageOpened();
      await settingsNodesPage.clickEnrollNode();
      await settingsNodesPage.validateEnrollNodeModalOpened();
      await settingsNodesPage.validateInstallationPrerequisite();
    });

    expect(mock.unexpected).toEqual([]);
  });
});
