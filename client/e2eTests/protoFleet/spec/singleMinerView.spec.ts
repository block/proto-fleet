import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";

test.describe("Proto Fleet - Single Miner View", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("opens an embedded miner from fleet, navigates within the hosted view, and returns to the fleet list", async ({
    commonSteps,
    minersPage,
    singleMinerPage,
  }) => {
    let miner: { name: string; ipAddress: string };

    await test.step("Open the fleet miners list and pick a Proto rig", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      [miner] = await minersPage.getVisibleMinerSummaries(1);
    });

    await test.step("Open the embedded miner view from the fleet list", async () => {
      await minersPage.openMinerRow(miner.ipAddress);
      await singleMinerPage.validateSingleMinerSurfaceOpened();
      await singleMinerPage.validateCurrentSubRoute("hashrate");
      await singleMinerPage.validateCloseButtonLabel(miner.name);
      await singleMinerPage.validateHostedMetadata({
        minerName: miner.name,
        ipAddress: miner.ipAddress,
      });
    });

    await test.step("Navigate to a second embedded page and keep the miner route scoped", async () => {
      await singleMinerPage.navigateToLogs();
      await singleMinerPage.validateCurrentSubRoute("logs");
    });

    await test.step("Close the embedded view and return to the fleet list", async () => {
      await singleMinerPage.clickCloseButton();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateMinerInList(miner.ipAddress);
    });
  });

  test("switching directly between embedded miner routes resets page-local state for the new miner", async ({
    commonSteps,
    minersPage,
    singleMinerPage,
  }) => {
    let firstMiner: { name: string; ipAddress: string };
    let secondMiner: { name: string; ipAddress: string };
    let secondMinerIdentifier = "";

    await test.step("Prepare two visible Proto rig miners", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      [firstMiner, secondMiner] = await minersPage.getVisibleMinerSummaries(2);
    });

    await test.step("Capture the embedded route identifier for the second miner", async () => {
      await minersPage.openMinerRow(secondMiner.ipAddress);
      await singleMinerPage.validateCurrentSubRoute("hashrate");
      secondMinerIdentifier = await singleMinerPage.getCurrentMinerIdentifier();
      await singleMinerPage.clickCloseButton();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Open the first miner logs and set a search query", async () => {
      await minersPage.openMinerRow(firstMiner.ipAddress);
      await singleMinerPage.navigateToLogs();
      const query = await singleMinerPage.getSearchableSubstringFromFirstLogRow();
      await singleMinerPage.searchLogs(query);
      await singleMinerPage.validateLogsSearchQuery(query);
    });

    await test.step("Switch directly to the second miner route and validate the view resets for that miner", async () => {
      await singleMinerPage.navigateClientSide(`/miners/${encodeURIComponent(secondMinerIdentifier)}/logs`);
      await singleMinerPage.validateCurrentSubRoute("logs");
      await singleMinerPage.waitForLogsListToBeReady();
      await singleMinerPage.validateLogsSearchQuery("");
      await singleMinerPage.validateCloseButtonLabel(secondMiner.name);
      await singleMinerPage.validateHostedMetadata({
        minerName: secondMiner.name,
        ipAddress: secondMiner.ipAddress,
      });
    });
  });

  test("row click falls back to the miner web UI when embedded view is unavailable", async ({
    commonSteps,
    minersPage,
    page,
  }) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(
      testConfig.target === "real",
      "The fake fleet provides a deterministic mixed miner set for fallback coverage.",
    );

    let fallbackMinerIp = "";

    await test.step("Open a non-Proto miner row that cannot use the embedded view", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await minersPage.filterAllMinersExceptRig();
      [{ ipAddress: fallbackMinerIp }] = await minersPage.getVisibleMinerSummaries(1);
    });

    await test.step("Click the row and open the standalone miner URL in a new tab", async () => {
      const popupPromise = page.context().waitForEvent("page");
      const popupNavigationPromise = page.context().waitForEvent("request", (request) => {
        return request.isNavigationRequest() && new URL(request.url()).hostname === fallbackMinerIp;
      });
      await minersPage.openMinerRow(fallbackMinerIp);
      const [popup] = await Promise.all([popupPromise, popupNavigationPromise]);

      await minersPage.waitForMinersTitle();
      await expect(page.getByTestId("single-miner-surface")).toHaveCount(0);

      await popup.close();
    });
  });

  test("fleet-hosted miner routes can open authentication settings without surfacing the direct ProtoOS login modal", async ({
    commonSteps,
    minersPage,
    singleMinerPage,
  }) => {
    let miner: { name: string; ipAddress: string };

    await test.step("Open an embedded Proto rig from the fleet miners list", async () => {
      await commonSteps.loginAsAdmin({ forceReauth: true });
      await commonSteps.goToMinersPage();
      await minersPage.filterRigMiners();
      [miner] = await minersPage.getVisibleMinerSummaries(1);
      await minersPage.openMinerRow(miner.ipAddress);
      await singleMinerPage.validateCurrentSubRoute("hashrate");
    });

    await test.step("Open the authentication settings route inside the hosted miner view", async () => {
      await singleMinerPage.navigateToAuthenticationSettings();
      await singleMinerPage.validateAuthenticationSettingsPageOpened();
      await singleMinerPage.validateDirectLoginModalHidden();
      await singleMinerPage.validateCloseButtonLabel(miner.name);
      await singleMinerPage.validateHostedMetadata({
        minerName: miner.name,
        ipAddress: miner.ipAddress,
      });
    });
  });
});
