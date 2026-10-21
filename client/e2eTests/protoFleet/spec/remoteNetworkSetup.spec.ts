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
      await expect(addMinersPage.getRemoteNetworkPrerequisite()).toHaveCount(0);
    });

    await test.step("Show one concise notice above discovery options", async () => {
      await addMinersPage.startOnboarding();
      const prerequisite = addMinersPage.getRemoteNetworkPrerequisite();
      await expect(prerequisite).toHaveCount(1);
      await expect(prerequisite).toContainText("No Fleet Nodes enrolled");
      await expect(prerequisite).toContainText("For networks Fleet can't reach, install and enroll a Fleet Node");
      await expect(prerequisite).toBeVisible();
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
